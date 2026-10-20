package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/telegram"
	"okuu/internal/token"
)

// Данные inline-кнопок.
const (
	cbConsent = "consent:" // + id приглашения
	cbLang    = "lang:"    // + ru | ky
	cbBalance = "balance"
)

// maxMessageLen — запас до лимита Telegram в 4096 символов.
const maxMessageLen = 3500

func (b *Bot) handle(ctx context.Context, u telegram.Update) error {
	chatID := u.ChatID()
	if chatID == 0 {
		return nil
	}
	if (u.Message != nil && u.Message.Chat.Type != "private") ||
		(u.CallbackQuery != nil && u.CallbackQuery.Message != nil && u.CallbackQuery.Message.Chat.Type != "private") {
		return nil // в группах бот молчит: там чужие люди
	}
	// Родитель пишет боту — значит, разблокировал его (R19: blocked → active).
	if _, err := b.q.SetChatStatus(ctx, sqlc.SetChatStatusParams{Status: "active", ChatID: &chatID, FromStatuses: []string{"blocked"}}); err != nil {
		return err
	}

	switch {
	case u.CallbackQuery != nil:
		return b.onCallback(ctx, chatID, u.CallbackQuery)
	case u.Message != nil && u.Message.Contact != nil:
		return b.onContact(ctx, chatID, u.Message)
	case u.Message != nil:
		return b.onText(ctx, chatID, u.Message)
	}
	return nil
}

func (b *Bot) onText(ctx context.Context, chatID int64, m *telegram.Message) error {
	cmd, arg := parseCommand(m.Text)
	switch cmd {
	case "/start":
		if arg != "" {
			return b.startWithInvite(ctx, chatID, m.From, arg)
		}
		return b.start(ctx, chatID, m.From)
	case "/balance":
		return b.balance(ctx, chatID, m.From)
	case "/lang":
		return b.send(ctx, chatID, notify.LangPrompt, langKeyboard())
	case "/stop":
		return b.stop(ctx, chatID, m.From)
	case "/help":
		tx, err := b.texts(ctx, chatID, m.From)
		if err != nil {
			return err
		}
		return b.send(ctx, chatID, tx.Help, balanceKeyboard(tx))
	}
	tx, err := b.texts(ctx, chatID, m.From)
	if err != nil {
		return err
	}
	// Ждём номер, а пришёл текст — напомним про кнопку.
	if st, err := b.q.GetLinkState(ctx, chatID); err == nil && st.ConsentAt != nil {
		return b.send(ctx, chatID, tx.ContactPrompt, contactKeyboard(tx))
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return b.send(ctx, chatID, tx.Unknown, nil)
}

// parseCommand: «/start@okuu_bot abc» → «/start», «abc».
func parseCommand(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	cmd, arg, _ = strings.Cut(text, " ")
	cmd, _, _ = strings.Cut(cmd, "@")
	return strings.ToLower(cmd), strings.TrimSpace(arg)
}

// validStartToken — токен приглашения: base64url, не длиннее лимита deep link Telegram (64).
func validStartToken(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// startWithInvite — /start <токен>: показать центр и детей, попросить согласие.
func (b *Bot) startWithInvite(ctx context.Context, chatID int64, from *telegram.User, raw string) error {
	tx, err := b.texts(ctx, chatID, from)
	if err != nil {
		return err
	}
	if from == nil || !validStartToken(raw) {
		return b.send(ctx, chatID, tx.InviteInvalid, nil)
	}
	inv, err := b.q.GetParentInviteByToken(ctx, token.HashHex(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return b.send(ctx, chatID, tx.InviteInvalid, nil)
	}
	if err != nil {
		return err
	}
	if err := b.q.UpsertLinkState(ctx, sqlc.UpsertLinkStateParams{ChatID: chatID, UserID: from.ID, InviteID: inv.InviteID}); err != nil {
		return err
	}
	children, err := b.q.ListParentChildNames(ctx, sqlc.ListParentChildNamesParams{CenterID: inv.CenterID, ParentID: &inv.ParentID})
	if err != nil {
		return err
	}
	return b.send(ctx, chatID, tx.ConsentPrompt(inv.CenterName, children), &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: tx.ConsentButton, CallbackData: cbConsent + inv.InviteID.String()}}},
	})
}

func (b *Bot) onCallback(ctx context.Context, chatID int64, cq *telegram.CallbackQuery) error {
	// Ответ на нажатие — сразу, чтобы у родителя не крутились «часики».
	if err := b.tg.Answer(ctx, telegram.AnswerCallbackQueryParams{CallbackQueryID: cq.ID}); err != nil {
		b.logger.Warn("telegram: answerCallbackQuery", "err", err)
	}
	switch {
	case strings.HasPrefix(cq.Data, cbConsent):
		id, err := uuid.Parse(strings.TrimPrefix(cq.Data, cbConsent))
		if err != nil {
			return nil
		}
		return b.consent(ctx, chatID, cq, id)
	case strings.HasPrefix(cq.Data, cbLang):
		return b.setLang(ctx, chatID, cq, strings.TrimPrefix(cq.Data, cbLang))
	case cq.Data == cbBalance:
		return b.balance(ctx, chatID, &cq.From)
	}
	return nil
}

// consent — «Согласен»: запомнить согласие и попросить номер телефона.
func (b *Bot) consent(ctx context.Context, chatID int64, cq *telegram.CallbackQuery, inviteID uuid.UUID) error {
	tx, err := b.texts(ctx, chatID, &cq.From)
	if err != nil {
		return err
	}
	// Согласие — только от того, кто открыл ссылку, и по текущему приглашению чата.
	if _, err := b.q.SetLinkConsent(ctx, sqlc.SetLinkConsentParams{ChatID: chatID, UserID: cq.From.ID, InviteID: inviteID}); errors.Is(err, pgx.ErrNoRows) {
		return b.send(ctx, chatID, tx.InviteInvalid, nil)
	} else if err != nil {
		return err
	}
	if _, err := b.q.GetParentInviteByID(ctx, inviteID); errors.Is(err, pgx.ErrNoRows) {
		if err := b.q.DeleteLinkState(ctx, chatID); err != nil {
			return err
		}
		return b.send(ctx, chatID, tx.InviteInvalid, nil)
	} else if err != nil {
		return err
	}
	// Кнопку «Согласен» убираем, текст согласия остаётся в чате.
	if cq.Message != nil && cq.Message.Text != "" {
		if err := b.tg.Edit(ctx, telegram.EditMessageTextParams{ChatID: chatID, MessageID: cq.Message.MessageID,
			Text: cq.Message.Text + "\n\n✅ " + tx.ConsentAccepted}); err != nil {
			return err
		}
	}
	return b.send(ctx, chatID, tx.ContactPrompt, contactKeyboard(tx))
}

// onContact — родитель поделился номером: свой контакт, номер совпадает → привязка.
func (b *Bot) onContact(ctx context.Context, chatID int64, m *telegram.Message) error {
	tx, err := b.texts(ctx, chatID, m.From)
	if err != nil {
		return err
	}
	st, err := b.q.GetLinkState(ctx, chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.send(ctx, chatID, tx.ContactNoInvite, telegram.RemoveKeyboard)
	}
	if err != nil {
		return err
	}
	if st.ConsentAt == nil {
		return b.send(ctx, chatID, tx.ContactBeforeConsent, nil)
	}
	// Чужой контакт (переслан или выбран из записной книжки) — не подтверждение.
	if m.From == nil || m.Contact.UserID == 0 || m.Contact.UserID != m.From.ID || st.UserID != m.From.ID {
		return b.send(ctx, chatID, tx.ContactNotOwn, contactKeyboard(tx))
	}
	lang, err := b.chatLang(ctx, chatID, m.From)
	if err != nil {
		return err
	}
	phone, phoneErr := auth.NormalizePhone(m.Contact.PhoneNumber)

	var (
		inv      sqlc.GetParentInviteByIDRow
		children []string
		outcome  string // invalid | mismatch | linked
	)
	err = pgx.BeginFunc(ctx, b.pool, func(dbtx pgx.Tx) error {
		q := sqlc.New(dbtx)
		var err error
		inv, err = q.GetParentInviteByID(ctx, st.InviteID)
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = "invalid"
			return q.DeleteLinkState(ctx, chatID)
		}
		if err != nil {
			return err
		}
		if phoneErr != nil || phone != inv.ParentPhone {
			outcome = "mismatch" // приглашение остаётся: администратор исправит номер — нажмут ещё раз
			return nil
		}
		if _, err := q.ClaimParentInvite(ctx, inv.InviteID); errors.Is(err, pgx.ErrNoRows) {
			outcome = "invalid"
			return q.DeleteLinkState(ctx, chatID)
		} else if err != nil {
			return err
		}
		p, err := q.LinkParentTelegram(ctx, sqlc.LinkParentTelegramParams{
			ChatID: &chatID, ConsentAt: st.ConsentAt, Lang: lang, CenterID: inv.CenterID, ID: inv.ParentID,
		})
		if err != nil {
			return err
		}
		if err := q.DeleteLinkState(ctx, chatID); err != nil {
			return err
		}
		if children, err = q.ListParentChildNames(ctx, sqlc.ListParentChildNamesParams{CenterID: inv.CenterID, ParentID: &inv.ParentID}); err != nil {
			return err
		}
		outcome = "linked"
		return audit.Record(ctx, dbtx, audit.Entry{
			CenterID: inv.CenterID, Action: audit.ParentTelegramLink, Entity: "parent", EntityID: &p.ID,
			After: map[string]any{"tg_status": p.TgStatus, "consent_at": p.ConsentAt, "invite_id": inv.InviteID, "lang": p.Lang},
		})
	})
	if err != nil {
		return err
	}
	switch outcome {
	case "invalid":
		return b.send(ctx, chatID, tx.InviteInvalid, telegram.RemoveKeyboard)
	case "mismatch":
		return b.send(ctx, chatID, tx.PhoneMismatch(inv.CenterName), contactKeyboard(tx))
	}
	return b.send(ctx, chatID, notify.For(lang).Linked(inv.CenterName, children), telegram.RemoveKeyboard)
}

// start — /start без токена: включить уведомления после /stop или подсказать, как подключиться.
func (b *Bot) start(ctx context.Context, chatID int64, from *telegram.User) error {
	tx, err := b.texts(ctx, chatID, from)
	if err != nil {
		return err
	}
	var resumed []sqlc.SetChatStatusRow
	var parents []sqlc.ListChatParentsRow
	err = pgx.BeginFunc(ctx, b.pool, func(dbtx pgx.Tx) error {
		q := sqlc.New(dbtx)
		var err error
		if resumed, err = q.SetChatStatus(ctx, sqlc.SetChatStatusParams{Status: "active", ChatID: &chatID, FromStatuses: []string{"stopped"}}); err != nil {
			return err
		}
		for _, r := range resumed {
			if err := audit.Record(ctx, dbtx, audit.Entry{CenterID: r.CenterID, Action: audit.ParentTelegramResume,
				Entity: "parent", EntityID: &r.ID, After: map[string]any{"tg_status": "active"}}); err != nil {
				return err
			}
		}
		parents, err = q.ListChatParents(ctx, &chatID)
		return err
	})
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		return b.send(ctx, chatID, tx.StartHint, nil)
	}
	if len(resumed) > 0 {
		return b.send(ctx, chatID, tx.Resumed(centerNames(parents, resumed)), balanceKeyboard(tx))
	}
	return b.send(ctx, chatID, tx.Welcome(centerNames(parents, nil)), balanceKeyboard(tx))
}

// stop — /stop (R18): напоминания больше не шлём, администратор видит «отключил уведомления».
func (b *Bot) stop(ctx context.Context, chatID int64, from *telegram.User) error {
	tx, err := b.texts(ctx, chatID, from)
	if err != nil {
		return err
	}
	var parents []sqlc.ListChatParentsRow
	err = pgx.BeginFunc(ctx, b.pool, func(dbtx pgx.Tx) error {
		q := sqlc.New(dbtx)
		stopped, err := q.SetChatStatus(ctx, sqlc.SetChatStatusParams{Status: "stopped", ChatID: &chatID, FromStatuses: []string{"active", "blocked"}})
		if err != nil {
			return err
		}
		for _, r := range stopped {
			if err := audit.Record(ctx, dbtx, audit.Entry{CenterID: r.CenterID, Action: audit.ParentTelegramStop,
				Entity: "parent", EntityID: &r.ID, After: map[string]any{"tg_status": "stopped"}}); err != nil {
				return err
			}
		}
		parents, err = q.ListChatParents(ctx, &chatID)
		return err
	})
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		return b.send(ctx, chatID, tx.NotLinked, nil)
	}
	return b.send(ctx, chatID, tx.Stopped(centerNames(parents, nil)), nil)
}

func (b *Bot) setLang(ctx context.Context, chatID int64, cq *telegram.CallbackQuery, lang string) error {
	if !notify.ValidLang(lang) {
		return nil
	}
	if err := b.q.SetChatLang(ctx, sqlc.SetChatLangParams{Lang: lang, ChatID: &chatID}); err != nil {
		return err
	}
	tx := notify.For(lang)
	if cq.Message != nil {
		return b.tg.Edit(ctx, telegram.EditMessageTextParams{ChatID: chatID, MessageID: cq.Message.MessageID, Text: tx.LangChanged})
	}
	return b.send(ctx, chatID, tx.LangChanged, nil)
}

// packageInfo — активный абонемент ребёнка (из ListParentChildrenSummary.packages).
type packageInfo struct {
	Group       string `json:"group"`
	LessonsLeft *int32 `json:"lessons_left"`
}

// balance — /balance: по каждому центру и ребёнку баланс, абонементы и посещения за месяц.
func (b *Bot) balance(ctx context.Context, chatID int64, from *telegram.User) error {
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		tx, err := b.texts(ctx, chatID, from)
		if err != nil {
			return err
		}
		return b.send(ctx, chatID, tx.NotLinked, nil)
	}
	tx := notify.For(parents[0].Lang)

	var blocks []string
	for _, p := range parents {
		today, err := civil.Today(b.now(), p.Timezone)
		if err != nil {
			return err
		}
		rows, err := b.q.ListParentChildrenSummary(ctx, sqlc.ListParentChildrenSummaryParams{
			FromDate: today.FirstOfMonth(), ToDate: today, CenterID: p.CenterID, ParentID: &p.ID,
		})
		if err != nil {
			return err
		}
		lines := []string{tx.BalanceCenter(p.CenterName)}
		shown := 0
		for _, r := range rows {
			if r.Status == "left" && r.Balance == 0 {
				continue // ушёл и ничего не должен — не показываем
			}
			shown++
			var pkgs []packageInfo
			if err := json.Unmarshal(r.Packages, &pkgs); err != nil {
				return fmt.Errorf("абонементы ученика %s: %w", r.ID, err)
			}
			lines = append(lines, "", "👤 "+r.FullName, tx.Balance(r.Balance))
			for _, pk := range pkgs {
				if pk.LessonsLeft != nil {
					lines = append(lines, tx.Package(pk.Group, *pk.LessonsLeft))
				}
			}
			lines = append(lines, tx.Attendance(int(today.Month()), r.Present, r.Absent, r.Excused))
		}
		if shown == 0 {
			lines = append(lines, tx.NoChildren)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}

	// Одно сообщение, а если не влезает — по центрам.
	var msgs []string
	cur := ""
	for _, blk := range blocks {
		if cur != "" && len(cur)+2+len(blk) > maxMessageLen {
			msgs = append(msgs, cur)
			cur = ""
		}
		if cur != "" {
			cur += "\n\n"
		}
		cur += blk
	}
	msgs = append(msgs, cur)
	for i, m := range msgs {
		var kb telegram.ReplyMarkup
		if i == len(msgs)-1 {
			kb = balanceKeyboard(tx)
		}
		if err := b.send(ctx, chatID, m, kb); err != nil {
			return err
		}
	}
	return nil
}

// texts — тексты на языке чата.
func (b *Bot) texts(ctx context.Context, chatID int64, from *telegram.User) (*notify.Texts, error) {
	lang, err := b.chatLang(ctx, chatID, from)
	if err != nil {
		return nil, err
	}
	return notify.For(lang), nil
}

// chatLang — язык чата: выбранный родителем (/lang меняет его во всех центрах), а до привязки —
// язык Telegram (ky → кыргызский, остальное — русский).
func (b *Bot) chatLang(ctx context.Context, chatID int64, from *telegram.User) (string, error) {
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil {
		return "", err
	}
	if len(parents) > 0 {
		return parents[0].Lang, nil
	}
	if from != nil && strings.HasPrefix(from.LanguageCode, notify.LangKY) {
		return notify.LangKY, nil
	}
	return notify.LangRU, nil
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, kb telegram.ReplyMarkup) error {
	_, err := b.tg.Send(ctx, telegram.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: kb})
	return err
}

// centerNames — названия центров родителей чата; only != nil — только этих родителей.
func centerNames(parents []sqlc.ListChatParentsRow, only []sqlc.SetChatStatusRow) []string {
	keep := map[uuid.UUID]bool{}
	for _, r := range only {
		keep[r.ID] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range parents {
		if (only != nil && !keep[p.ID]) || seen[p.CenterName] {
			continue
		}
		seen[p.CenterName] = true
		out = append(out, p.CenterName)
	}
	return out
}

func contactKeyboard(tx *notify.Texts) *telegram.ReplyKeyboardMarkup {
	return &telegram.ReplyKeyboardMarkup{
		Keyboard:       [][]telegram.KeyboardButton{{{Text: tx.ContactButton, RequestContact: true}}},
		ResizeKeyboard: true, OneTimeKeyboard: true,
	}
}

func balanceKeyboard(tx *notify.Texts) *telegram.InlineKeyboardMarkup {
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tx.BalanceButton, CallbackData: cbBalance}},
	}}
}

func langKeyboard() *telegram.InlineKeyboardMarkup {
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{
		{Text: notify.LangButtons[notify.LangRU], CallbackData: cbLang + notify.LangRU},
		{Text: notify.LangButtons[notify.LangKY], CallbackData: cbLang + notify.LangKY},
	}}}
}
