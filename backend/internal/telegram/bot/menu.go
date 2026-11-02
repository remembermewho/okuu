package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"okuu/internal/civil"
	"okuu/internal/notify"
	"okuu/internal/telegram"
)

// Навигация: постоянная клавиатура «Баланс · Как оплатить · Посещения · Помощь», меню команд (setMyCommands),
// выбор ребёнка, если детей несколько, и кнопка «« Меню» в ответах.

// Данные inline-кнопок навигации.
const (
	cbMenu     = "menu"
	cbBal      = "bal:" // + id ученика | all
	cbAtt      = "att:" // + id ученика | all
	cbHowToPay = "howtopay"
	cbPayList  = "paylist" // старая кнопка «Оплатить» в «« Меню» — теперь «Как оплатить»
	cbHelp     = "help"
	allKids    = "all"
)

// legacyPayLabels — подписи прежней кнопки «Оплатить» постоянной клавиатуры: у родителей она остаётся,
// пока бот не пришлёт новую клавиатуру, — понимаем её как «Как оплатить».
var legacyPayLabels = []string{"💳 Оплатить", "💳 Төлөө"}

// Commands — команды в меню бота (кнопка «Меню» у поля ввода), с русскими описаниями.
// /pay нет: оплаты в боте нет, «Как оплатить» — кнопка постоянной клавиатуры.
func Commands(l notify.Langs) []telegram.BotCommand {
	cmds := []telegram.BotCommand{
		{Command: "start", Description: "Подключиться или включить уведомления"},
		{Command: "balance", Description: "Баланс и абонемент"},
		{Command: "attendance", Description: "Посещения за месяц"},
		{Command: "help", Description: "Помощь"},
		{Command: "stop", Description: "Отключить уведомления"},
	}
	if l.Multi() {
		cmds = append(cmds, telegram.BotCommand{Command: "lang", Description: "Язык / Тил"})
	}
	return cmds
}

// SetupCommands — setMyCommands при старте бота.
func SetupCommands(ctx context.Context, api telegram.API, l notify.Langs) error {
	return api.SetMyCommands(ctx, telegram.SetMyCommandsParams{Commands: Commands(l)})
}

// MenuKeyboard — постоянная клавиатура внизу чата (после привязки).
func MenuKeyboard(tx *notify.Texts) *telegram.ReplyKeyboardMarkup {
	return &telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: tx.MenuBalance}, {Text: tx.MenuHowToPay}},
			{{Text: tx.MenuAttendance}, {Text: tx.MenuHelp}},
		},
		IsPersistent: true, ResizeKeyboard: true,
	}
}

// menuAction — что означает текст кнопки постоянной клавиатуры (на любом из языков); "" — не кнопка.
func menuAction(text string) string {
	text = strings.TrimSpace(text)
	for _, lang := range []string{notify.LangRU, notify.LangKY} {
		tx := notify.For(lang)
		switch text {
		case tx.MenuBalance:
			return "/balance"
		case tx.MenuHowToPay:
			return "/pay" // внутреннее имя «Как оплатить»
		case tx.MenuAttendance:
			return "/attendance"
		case tx.MenuHelp:
			return "/help"
		}
	}
	for _, l := range legacyPayLabels {
		if text == l {
			return "/pay"
		}
	}
	return ""
}

func backRow(tx *notify.Texts) []telegram.InlineKeyboardButton {
	return []telegram.InlineKeyboardButton{{Text: tx.MenuBack, CallbackData: cbMenu}}
}

// menuInline — «« Меню»: те же разделы inline-кнопками под сообщением.
func menuInline(tx *notify.Texts) *telegram.InlineKeyboardMarkup {
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tx.MenuBalance, CallbackData: cbBal}, {Text: tx.MenuHowToPay, CallbackData: cbHowToPay}},
		{{Text: tx.MenuAttendance, CallbackData: cbAtt}, {Text: tx.MenuHelp, CallbackData: cbHelp}},
	}}
}

// kid — ребёнок родителя, привязанного к чату, со сводкой за текущий месяц (в поясе его центра).
type kid struct {
	ID       uuid.UUID
	Name     string
	CenterID uuid.UUID
	Center   string
	Month    int
	Balance  int64
	Present  int32
	Absent   int32
	Excused  int32
	Packages []packageInfo
}

// chatKids — дети всех родителей чата (по центрам). Ушедшие без долга не показываются.
// linked=false — чат ни к кому не привязан.
func (b *Bot) chatKids(ctx context.Context, chatID int64) (kids []kid, tx *notify.Texts, linked bool, err error) {
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil || len(parents) == 0 {
		return nil, b.langs.For(""), false, err
	}
	tx = b.langs.For(parents[0].Lang)
	for _, p := range parents {
		today, err := civil.Today(b.now(), p.Timezone)
		if err != nil {
			return nil, nil, true, err
		}
		rows, err := b.q.ListParentChildrenSummary(ctx, summaryParams(p.CenterID, p.ID, today))
		if err != nil {
			return nil, nil, true, err
		}
		for _, r := range rows {
			if r.Status == "left" && r.Balance == 0 {
				continue // ушёл и ничего не должен — не показываем
			}
			var pkgs []packageInfo
			if err := json.Unmarshal(r.Packages, &pkgs); err != nil {
				return nil, nil, true, fmt.Errorf("абонементы ученика %s: %w", r.ID, err)
			}
			kids = append(kids, kid{ID: r.ID, Name: r.FullName, CenterID: p.CenterID, Center: p.CenterName, Month: int(today.Month()),
				Balance: r.Balance, Present: r.Present, Absent: r.Absent, Excused: r.Excused, Packages: pkgs})
		}
	}
	return kids, tx, true, nil
}

// pick — дети по выбору: all — все, иначе один по id; nil — не нашли (кнопка от чужого ребёнка или устарела).
func pick(kids []kid, target string) []kid {
	if target == allKids {
		return kids
	}
	for _, k := range kids {
		if k.ID.String() == target {
			return []kid{k}
		}
	}
	return nil
}

// chooseKeyboard — выбор ребёнка: кнопка на каждого и «Все дети».
func chooseKeyboard(tx *notify.Texts, kids []kid, prefix string) *telegram.InlineKeyboardMarkup {
	centers := map[uuid.UUID]bool{}
	for _, k := range kids {
		centers[k.CenterID] = true
	}
	var rows [][]telegram.InlineKeyboardButton
	for _, k := range kids {
		label := k.Name
		if len(centers) > 1 {
			label += " · " + k.Center
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: label, CallbackData: prefix + k.ID.String()}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tx.AllChildren, CallbackData: prefix + allKids}}, backRow(tx))
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// showBalance — /balance и «Баланс»: несколько детей — сначала выбор; target — id, all или "".
func (b *Bot) showBalance(ctx context.Context, chatID int64, target string) error {
	kids, tx, linked, err := b.chatKids(ctx, chatID)
	if err != nil {
		return err
	}
	if !linked {
		return b.send(ctx, chatID, tx.NotLinked, nil)
	}
	if target == "" {
		if len(kids) > 1 {
			return b.send(ctx, chatID, tx.ChooseChild, chooseKeyboard(tx, kids, cbBal))
		}
		target = allKids
	}
	shown := pick(kids, target)
	if shown == nil && target != allKids {
		return b.send(ctx, chatID, tx.ChooseChild, chooseKeyboard(tx, kids, cbBal))
	}
	return b.sendBalance(ctx, chatID, tx, kids, shown)
}

// sendBalance — баланс, абонементы и посещения по центрам; одно сообщение, а если не влезает — по центрам.
// Под последним — «Как оплатить», если есть долг, и «« Меню».
func (b *Bot) sendBalance(ctx context.Context, chatID int64, tx *notify.Texts, all, shown []kid) error {
	var blocks []string
	debt := false
	byCenter := map[uuid.UUID][]kid{}
	for _, k := range shown {
		byCenter[k.CenterID] = append(byCenter[k.CenterID], k)
	}
	// Центры — в порядке родителей чата; центр без детей — «Учеников в центре нет» (только когда показаны все).
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range parents {
		if seen[p.CenterID] {
			continue
		}
		seen[p.CenterID] = true
		ks := byCenter[p.CenterID]
		if len(ks) == 0 && len(shown) != len(all) {
			continue // выбран один ребёнок — другие центры не показываем
		}
		lines := []string{tx.BalanceCenter(p.CenterName)}
		for _, k := range ks {
			lines = append(lines, "", "👤 "+k.Name, tx.Balance(k.Balance))
			debt = debt || k.Balance < 0
			for _, pk := range k.Packages {
				if pk.LessonsLeft != nil {
					lines = append(lines, tx.Package(pk.Group, *pk.LessonsLeft))
				}
			}
			lines = append(lines, tx.Attendance(k.Month, k.Present, k.Absent, k.Excused))
		}
		if len(ks) == 0 {
			lines = append(lines, tx.NoChildren)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	kb := &telegram.InlineKeyboardMarkup{}
	if debt {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineKeyboardButton{{Text: tx.MenuHowToPay, CallbackData: cbHowToPay}})
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, backRow(tx))
	return b.sendChunks(ctx, chatID, blocks, kb)
}

// showAttendance — /attendance и «Посещения»: за текущий месяц по каждому ребёнку.
func (b *Bot) showAttendance(ctx context.Context, chatID int64, target string) error {
	kids, tx, linked, err := b.chatKids(ctx, chatID)
	if err != nil {
		return err
	}
	if !linked {
		return b.send(ctx, chatID, tx.NotLinked, nil)
	}
	if len(kids) == 0 {
		return b.send(ctx, chatID, tx.NoChildren, &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{backRow(tx)}})
	}
	if target == "" {
		if len(kids) > 1 {
			return b.send(ctx, chatID, tx.ChooseChild, chooseKeyboard(tx, kids, cbAtt))
		}
		target = allKids
	}
	shown := pick(kids, target)
	if shown == nil {
		return b.send(ctx, chatID, tx.ChooseChild, chooseKeyboard(tx, kids, cbAtt))
	}
	lines := []string{tx.AttendanceHeader(shown[0].Month)}
	for _, k := range shown {
		lines = append(lines, "", tx.AttendanceChild(k.Name, k.Center, k.Present, k.Absent, k.Excused))
	}
	return b.send(ctx, chatID, strings.Join(lines, "\n"), &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{backRow(tx)}})
}

// showHowToPay — «Как оплатить»: реквизиты и инструкция центра (pay_details) как есть, блок на каждый центр чата
// с его названием; пустые реквизиты — «Уточните способ оплаты у администратора центра». Сюда же — старые
// кнопки «Оплатить» и набранная вручную /pay.
func (b *Bot) showHowToPay(ctx context.Context, chatID int64) error {
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		return b.send(ctx, chatID, b.langs.For("").NotLinked, nil)
	}
	tx := b.langs.For(parents[0].Lang)
	var blocks []string
	seen := map[uuid.UUID]bool{}
	for _, p := range parents {
		if seen[p.CenterID] {
			continue
		}
		seen[p.CenterID] = true
		details := ""
		if p.PayDetails != nil {
			details = *p.PayDetails
		}
		blocks = append(blocks, tx.BalanceCenter(p.CenterName)+"\n"+notify.HowToPay(tx, details))
	}
	return b.sendChunks(ctx, chatID, blocks, &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{backRow(tx)}})
}

// showMenu — «« Меню»: разделы inline-кнопками (постоянная клавиатура тоже остаётся).
func (b *Bot) showMenu(ctx context.Context, chatID int64, from *telegram.User) error {
	tx, linked, err := b.linkedTexts(ctx, chatID, from)
	if err != nil {
		return err
	}
	if !linked {
		return b.send(ctx, chatID, tx.NotLinked, nil)
	}
	return b.send(ctx, chatID, tx.MenuPrompt, menuInline(tx))
}

// showHelp — /help и «Помощь»: команды; привязанным — заодно вернуть постоянную клавиатуру.
func (b *Bot) showHelp(ctx context.Context, chatID int64, from *telegram.User) error {
	tx, linked, err := b.linkedTexts(ctx, chatID, from)
	if err != nil {
		return err
	}
	if !linked {
		return b.send(ctx, chatID, tx.Help, nil)
	}
	return b.send(ctx, chatID, tx.Help, MenuKeyboard(tx))
}

// linkedTexts — тексты чата и привязан ли он хоть к одному родителю.
func (b *Bot) linkedTexts(ctx context.Context, chatID int64, from *telegram.User) (*notify.Texts, bool, error) {
	parents, err := b.q.ListChatParents(ctx, &chatID)
	if err != nil {
		return nil, false, err
	}
	if len(parents) > 0 {
		return b.langs.For(parents[0].Lang), true, nil
	}
	tx, err := b.texts(ctx, chatID, from)
	return tx, false, err
}

// sendChunks — блоки одним сообщением, а если не влезает — несколькими; клавиатура — под последним.
func (b *Bot) sendChunks(ctx context.Context, chatID int64, blocks []string, kb telegram.ReplyMarkup) error {
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
		var k telegram.ReplyMarkup
		if i == len(msgs)-1 {
			k = kb
		}
		if err := b.send(ctx, chatID, m, k); err != nil {
			return err
		}
	}
	return nil
}
