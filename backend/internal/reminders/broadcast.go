package reminders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"okuu/internal/audit"
	"okuu/internal/billing"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/validate"
)

// Рассылка (объявление центра): родителям с Telegram — бот через общую очередь (telegram.Sender следит за лимитами),
// остальным — список ссылок wa.me. Получатели записываются сразу; отправляет фоновый Dispatch.

const (
	// MaxBroadcastText — длина объявления, символов.
	MaxBroadcastText = 1000
	// MaxBroadcastGroups — групп в одной рассылке.
	MaxBroadcastGroups = 200

	AudienceCenter = "center"
	AudienceGroups = "groups"

	broadcastBatch    = 50
	historyLimit      = 100
	broadcastInterval = 5 * time.Minute // не больше одной рассылки центра за этот срок

	statusQueued     = "queued"
	statusSent       = "sent"
	statusWAPending  = "whatsapp_pending"
	statusWAOpened   = "whatsapp_opened"
	statusNoContact  = "no_contact"
	broadcastLockTag = 0x62726f61 // "broa": блокировка центра отдельная от ручных напоминаний
)

var (
	// ErrBroadcastTooSoon — прошлая рассылка центра была меньше 5 минут назад.
	ErrBroadcastTooSoon = errors.New("рассылка меньше 5 минут назад")
	// ErrGroupNotFound — группы из запроса нет в центре.
	ErrGroupNotFound = errors.New("группа не найдена")
	// ErrBroadcastNotFound — рассылки или получателя нет в центре.
	ErrBroadcastNotFound = errors.New("рассылка не найдена")
)

// BroadcastInput — кому и что; IdempotencyKey — защита от двойного нажатия (только для отправки).
type BroadcastInput struct {
	Audience       string
	GroupIDs       []uuid.UUID
	Text           string
	IdempotencyKey string
}

// BroadcastPreview — сколько родителей по какому каналу и как будет выглядеть сообщение.
type BroadcastPreview struct {
	Telegram, WhatsApp, NoContact int
	TelegramText, WhatsAppText    string
}

// BroadcastCounts — итоги рассылки.
type BroadcastCounts struct {
	TelegramSent, TelegramQueued, WhatsAppTotal, WhatsAppOpened, NoContact int
}

// BroadcastSummary — строка истории.
type BroadcastSummary struct {
	ID         uuid.UUID
	Audience   string
	GroupNames []string
	Text       string
	Author     string
	CreatedAt  time.Time
	Counts     BroadcastCounts
}

// BroadcastLink — родитель, которому пишут вручную. URL пустой — телефона нет (no_contact).
type BroadcastLink struct {
	ParentID   uuid.UUID
	ParentName string
	Students   []string
	Text, URL  string
	Opened     bool
}

// BroadcastDetail — рассылка со списком ссылок.
type BroadcastDetail struct {
	BroadcastSummary
	Links []BroadcastLink
}

func normalizeBroadcast(in BroadcastInput) (BroadcastInput, error) {
	in.Text = strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	if in.Text == "" {
		return in, validate.Errorf("text", "Напишите текст объявления")
	}
	if n := utf8.RuneCountInString(in.Text); n > MaxBroadcastText {
		return in, validate.Errorf("text", "Текст — не длиннее %d символов, сейчас %d", MaxBroadcastText, n)
	}
	switch in.Audience {
	case AudienceCenter:
		if len(in.GroupIDs) > 0 {
			return in, validate.Errorf("group_ids", "Группы указывают только при audience = groups")
		}
	case AudienceGroups:
		if len(in.GroupIDs) == 0 {
			return in, validate.Errorf("group_ids", "Выберите хотя бы одну группу")
		}
		if len(in.GroupIDs) > MaxBroadcastGroups {
			return in, validate.Errorf("group_ids", "Не больше %d групп", MaxBroadcastGroups)
		}
	default:
		return in, validate.Errorf("audience", "Получатели: center или groups")
	}
	return in, nil
}

// resolveGroups — названия групп центра; чужая группа — ошибка.
func resolveGroups(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID, in BroadcastInput) (ids []uuid.UUID, names []string, err error) {
	if in.Audience != AudienceGroups {
		return []uuid.UUID{}, []string{}, nil
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.GroupIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	rows, err := q.ListCenterGroupsByIDs(ctx, sqlc.ListCenterGroupsByIDsParams{CenterID: centerID, Ids: ids})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) != len(ids) {
		return nil, nil, ErrGroupNotFound
	}
	names = make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return ids, names, nil
}

// channelOf — статус получателя при создании рассылки. Telegram выключен — все идут в WhatsApp.
func (s *Service) channelOf(tgStatus string, chatID *int64, phone string) string {
	if s.tg != nil && tgStatus == "active" && chatID != nil {
		return statusQueued
	}
	if _, ok := WhatsAppPhone(phone); ok {
		return statusWAPending
	}
	return statusNoContact
}

// PreviewBroadcast — число получателей по каналам и текст сообщения. Ничего не записывает, работает и в демо-центре.
func (s *Service) PreviewBroadcast(ctx context.Context, centerID uuid.UUID, in BroadcastInput) (BroadcastPreview, error) {
	in, err := normalizeBroadcast(in)
	if err != nil {
		return BroadcastPreview{}, err
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return BroadcastPreview{}, err
	}
	groupIDs, _, err := resolveGroups(ctx, s.q, centerID, in)
	if err != nil {
		return BroadcastPreview{}, err
	}
	byGroups := in.Audience == AudienceGroups
	rows, err := s.q.ListBroadcastAudience(ctx, sqlc.ListBroadcastAudienceParams{CenterID: centerID, ByGroups: byGroups, GroupIds: groupIDs})
	if err != nil {
		return BroadcastPreview{}, err
	}
	noParent, err := s.q.CountAudienceWithoutParent(ctx, sqlc.CountAudienceWithoutParentParams{CenterID: centerID, ByGroups: byGroups, GroupIds: groupIDs})
	if err != nil {
		return BroadcastPreview{}, err
	}
	tx := s.langs.For(notify.LangRU)
	p := BroadcastPreview{NoContact: int(noParent),
		TelegramText: notify.Broadcast(tx, c.Name, in.Text), WhatsAppText: notify.BroadcastWhatsApp(tx, c.Name, in.Text)}
	for _, r := range rows {
		switch s.channelOf(r.TgStatus, r.TelegramChatID, r.ParentPhone) {
		case statusQueued:
			p.Telegram++
		case statusWAPending:
			p.WhatsApp++
		default:
			p.NoContact++
		}
	}
	return p, nil
}

// CreateBroadcast — «Отправить». Повтор с тем же Idempotency-Key — та же рассылка (created=false).
func (s *Service) CreateBroadcast(ctx context.Context, actor, centerID uuid.UUID, in BroadcastInput) (BroadcastDetail, bool, error) {
	in, err := normalizeBroadcast(in)
	if err != nil {
		return BroadcastDetail{}, false, err
	}
	if n := utf8.RuneCountInString(in.IdempotencyKey); n < 1 || n > billing.MaxIdempotencyKey || strings.TrimSpace(in.IdempotencyKey) != in.IdempotencyKey {
		return BroadcastDetail{}, false, validate.Errorf("Idempotency-Key", "Заголовок Idempotency-Key обязателен: 1–%d символов без пробелов по краям", billing.MaxIdempotencyKey)
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return BroadcastDetail{}, false, err
	}
	if c.IsDemo {
		return BroadcastDetail{}, false, ErrDemoCenter
	}

	var id uuid.UUID
	created := false
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		// Двойное нажатие и лимит «раз в 5 минут» — под блокировкой центра.
		if err := q.LockCenterBroadcast(ctx, lockKey(centerID)^broadcastLockTag); err != nil {
			return err
		}
		prev, err := q.GetBroadcastByKey(ctx, sqlc.GetBroadcastByKeyParams{CenterID: centerID, IdempotencyKey: in.IdempotencyKey})
		if err == nil {
			if prev.Body != in.Text || prev.Audience != in.Audience {
				return billing.ErrIdempotencyMismatch
			}
			id = prev.ID
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		now := s.now()
		recent, err := q.BroadcastSentSince(ctx, sqlc.BroadcastSentSinceParams{CenterID: centerID, Since: now.Add(-broadcastInterval)})
		if err != nil {
			return err
		}
		if recent {
			return ErrBroadcastTooSoon
		}
		groupIDs, groupNames, err := resolveGroups(ctx, q, centerID, in)
		if err != nil {
			return err
		}
		byGroups := in.Audience == AudienceGroups
		rows, err := q.ListBroadcastAudience(ctx, sqlc.ListBroadcastAudienceParams{CenterID: centerID, ByGroups: byGroups, GroupIds: groupIDs})
		if err != nil {
			return err
		}
		noParent, err := q.CountAudienceWithoutParent(ctx, sqlc.CountAudienceWithoutParentParams{CenterID: centerID, ByGroups: byGroups, GroupIds: groupIDs})
		if err != nil {
			return err
		}
		if len(rows) == 0 && noParent == 0 {
			return validate.Unprocessablef("audience", "Среди выбранных получателей нет учеников")
		}
		b, err := q.InsertBroadcast(ctx, sqlc.InsertBroadcastParams{CenterID: centerID, CreatedBy: actor, IdempotencyKey: in.IdempotencyKey,
			Audience: in.Audience, GroupIds: groupIDs, GroupNames: groupNames, Body: in.Text, NoParentStudents: noParent, CreatedAt: now})
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, r := range rows {
			status := s.channelOf(r.TgStatus, r.TelegramChatID, r.ParentPhone)
			counts[status]++
			if err := q.InsertBroadcastRecipient(ctx, sqlc.InsertBroadcastRecipientParams{BroadcastID: b.ID, CenterID: centerID, ParentID: r.ParentID,
				Status: status, StudentIds: r.StudentIds, StudentNames: r.StudentNames}); err != nil {
				return err
			}
		}
		if err := audit.Record(ctx, tx, audit.Entry{CenterID: centerID, UserID: &actor, Action: audit.BroadcastSend, Entity: "broadcast", EntityID: &b.ID,
			After: map[string]any{"audience": in.Audience, "groups": groupNames, "text": in.Text, "telegram": counts[statusQueued],
				"whatsapp": counts[statusWAPending], "no_contact": counts[statusNoContact] + int(noParent)}}); err != nil {
			return err
		}
		id, created = b.ID, true
		return q.NotifyOutbox(ctx) // будит диспетчер после COMMIT
	})
	if err != nil {
		return BroadcastDetail{}, false, err
	}
	d, err := s.GetBroadcast(ctx, centerID, id)
	return d, created, err
}

func broadcastSummary(r sqlc.ListBroadcastsRow) BroadcastSummary {
	return BroadcastSummary{ID: r.ID, Audience: r.Audience, GroupNames: r.GroupNames, Text: r.Body, Author: r.AuthorName, CreatedAt: r.CreatedAt,
		Counts: BroadcastCounts{TelegramSent: int(r.TelegramSent), TelegramQueued: int(r.TelegramQueued), WhatsAppTotal: int(r.WhatsappTotal),
			WhatsAppOpened: int(r.WhatsappOpened), NoContact: int(r.NoContact) + int(r.NoParentStudents)}}
}

// ListBroadcasts — история центра, новые сверху.
func (s *Service) ListBroadcasts(ctx context.Context, centerID uuid.UUID) ([]BroadcastSummary, error) {
	rows, err := s.q.ListBroadcasts(ctx, sqlc.ListBroadcastsParams{CenterID: centerID, Lim: historyLimit})
	if err != nil {
		return nil, err
	}
	out := make([]BroadcastSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, broadcastSummary(r))
	}
	return out, nil
}

// GetBroadcast — итоги и список ссылок: хранится на сервере, поэтому не теряется, что бы ни случилось со страницей.
func (s *Service) GetBroadcast(ctx context.Context, centerID, id uuid.UUID) (BroadcastDetail, error) {
	rows, err := s.q.ListBroadcasts(ctx, sqlc.ListBroadcastsParams{CenterID: centerID, ID: &id, Lim: 1})
	if err != nil {
		return BroadcastDetail{}, err
	}
	if len(rows) == 0 {
		return BroadcastDetail{}, ErrBroadcastNotFound
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return BroadcastDetail{}, err
	}
	links, err := s.q.ListBroadcastLinks(ctx, sqlc.ListBroadcastLinksParams{CenterID: centerID, BroadcastID: id})
	if err != nil {
		return BroadcastDetail{}, err
	}
	d := BroadcastDetail{BroadcastSummary: broadcastSummary(rows[0]), Links: make([]BroadcastLink, 0, len(links))}
	for _, l := range links {
		text := notify.BroadcastWhatsApp(s.langs.For(l.Lang), c.Name, d.Text)
		link := BroadcastLink{ParentID: l.ParentID, ParentName: l.ParentName, Students: l.StudentNames, Text: text, Opened: l.Status == statusWAOpened}
		if phone, ok := WhatsAppPhone(l.ParentPhone); ok && l.Status != statusNoContact {
			link.URL = notify.WhatsAppURL(phone, text)
		}
		d.Links = append(d.Links, link)
	}
	return d, nil
}

// OpenBroadcastLink — сотрудник открыл ссылку WhatsApp получателя. Повторное открытие запись не дублирует.
func (s *Service) OpenBroadcastLink(ctx context.Context, actor, centerID, broadcastID, parentID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		r, err := q.GetBroadcastLinkRecipient(ctx, sqlc.GetBroadcastLinkRecipientParams{CenterID: centerID, BroadcastID: broadcastID, ParentID: parentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBroadcastNotFound
		}
		if err != nil {
			return err
		}
		now := s.now()
		n, err := q.OpenBroadcastRecipient(ctx, sqlc.OpenBroadcastRecipientParams{OpenedAt: &now, CenterID: centerID, BroadcastID: broadcastID, ParentID: parentID})
		if err != nil || n == 0 {
			return err
		}
		if err := q.InsertWhatsAppNotification(ctx, sqlc.InsertWhatsAppNotificationParams{CenterID: centerID, ParentID: parentID,
			Kind: notify.KindBroadcast, StudentIds: r.StudentIds, CreatedBy: &actor, SentAt: now}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{CenterID: centerID, UserID: &actor, Action: audit.BroadcastWhatsAppOpen, Entity: "broadcast", EntityID: &broadcastID,
			After: map[string]any{"channel": "whatsapp_link", "students": r.StudentIds}})
	})
}

// dispatchBroadcasts — одна порция очереди бота. Родитель, до которого бот не достучался (отключил /stop, заблокировал,
// ошибка Telegram), переходит в список WhatsApp. Возвращает размер порции и число отправленных.
func (s *Service) dispatchBroadcasts(ctx context.Context) (batch, sent int, err error) {
	rows, err := s.q.ListQueuedBroadcastRecipients(ctx, broadcastBatch)
	if err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		status := statusSent
		lang := row.Lang
		r, ok := recipientOf(&row.ParentID, &row.TgStatus, row.TelegramChatID, &lang)
		if ok {
			key := fmt.Sprintf("%s:%s:%s", notify.KindBroadcast, row.BroadcastID, row.ParentID)
			id, claimed, err := s.claim(ctx, s.pool, claimInput{centerID: row.CenterID, parentID: row.ParentID, kind: notify.KindBroadcast,
				key: &key, students: row.StudentIds, actor: &row.CreatedBy})
			if err != nil {
				return len(rows), sent, err
			}
			// Ключ занят — сообщение уже уходило (перезапуск посреди порции): второй раз не шлём.
			if claimed {
				outcome, err := s.send(ctx, id, r, notify.Broadcast(s.langs.For(r.lang), row.CenterName, row.Body), nil, false)
				if err != nil {
					return len(rows), sent, err
				}
				if ctx.Err() != nil { // остановка сервера: получатель остаётся в очереди, ключ снят
					return len(rows), sent, ctx.Err()
				}
				if outcome == outcomeSent {
					sent++
				} else {
					ok = false
				}
			}
		}
		if !ok {
			status = statusNoContact
			if _, valid := WhatsAppPhone(row.ParentPhone); valid {
				status = statusWAPending
			}
		}
		if err := s.q.SetBroadcastRecipientStatus(ctx, sqlc.SetBroadcastRecipientStatusParams{Status: status, BroadcastID: row.BroadcastID, ParentID: row.ParentID}); err != nil {
			return len(rows), sent, err
		}
	}
	return len(rows), sent, nil
}
