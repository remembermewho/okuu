package reminders

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
)

// WhatsApp без API: программа готовит текст и ссылку wa.me, сотрудник отправляет со своего телефона.
// Поэтому отметка — «ссылка открыта», а не «доставлено».

var (
	// ErrNoPhone — у ученика нет родителя с телефоном.
	ErrNoPhone = errors.New("у родителя нет телефона")
	// ErrBadPhone — телефон родителя не приводится к 996XXXXXXXXX.
	ErrBadPhone = errors.New("некорректный телефон родителя")
	// ErrNoDebt — напоминать не о чем.
	ErrNoDebt = errors.New("у ученика нет долга")
)

// WhatsAppPhone — телефон для ссылки wa.me: только цифры, 996XXXXXXXXX. Принимает 0XXX…, +996…, с пробелами.
func WhatsAppPhone(raw string) (string, bool) {
	p, err := auth.NormalizePhone(raw)
	if err != nil {
		return "", false
	}
	return strings.TrimPrefix(p, "+"), true
}

// WhatsAppPhonePtr — то же для необязательного телефона; nil — номера нет или он некорректный.
func WhatsAppPhonePtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	if p, ok := WhatsAppPhone(*raw); ok {
		return &p
	}
	return nil
}

// WhatsAppStudent — ученик в WhatsApp-напоминании.
type WhatsAppStudent struct {
	ID   uuid.UUID
	Name string
	Debt int64
}

// WhatsAppMessage — готовое напоминание родителю: текст и ссылка. Phone, URL пустые — телефона нет или он некорректный.
type WhatsAppMessage struct {
	ParentID    uuid.UUID
	ParentName  string
	Phone       string
	BadPhone    bool // телефон записан, но не приводится к 996XXXXXXXXX
	Students    []WhatsAppStudent
	Total       int64
	Text        string
	URL         string
	OpenedToday bool
}

// whatsAppForParents — по сообщению на родителя: все его дети с долгом, текст — общий с Telegram (notify.debtReminder).
// Порядок — как в ListReminderStudents; родители без долга не попадают.
func (s *Service) whatsAppForParents(ctx context.Context, c sqlc.Center, today civil.Date, parents []uuid.UUID) ([]WhatsAppMessage, error) {
	if len(parents) == 0 {
		return nil, nil
	}
	rows, err := s.q.ListReminderStudents(ctx, sqlc.ListReminderStudentsParams{CenterID: c.ID, ParentIds: parents, OnlyDebt: true})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	debts, err := s.loadDebts(ctx, c.ID, c.Timezone, today, ids)
	if err != nil {
		return nil, err
	}

	type draft struct {
		msg      WhatsAppMessage
		lang     string
		children []notify.ChildDebt
	}
	byParent := map[uuid.UUID]*draft{}
	var order []*draft
	for _, r := range rows {
		if r.ParentID == nil {
			continue
		}
		d := byParent[*r.ParentID]
		if d == nil {
			d = &draft{msg: WhatsAppMessage{ParentID: *r.ParentID, ParentName: deref(r.ParentName)}, lang: notify.LangRU}
			if r.ParentLang != nil {
				d.lang = *r.ParentLang
			}
			if phone := WhatsAppPhonePtr(r.ParentPhone); phone != nil {
				d.msg.Phone = *phone
			} else if r.ParentPhone != nil && strings.TrimSpace(*r.ParentPhone) != "" {
				d.msg.BadPhone = true
			}
			if r.ParentLastWhatsappAt != nil {
				on, err := civil.DateIn(*r.ParentLastWhatsappAt, c.Timezone)
				if err != nil {
					return nil, err
				}
				d.msg.OpenedToday = on == today
			}
			byParent[*r.ParentID] = d
			order = append(order, d)
		}
		debt := debts[r.ID].debt
		d.children = append(d.children, childDebt(r.FullName, debt, today))
		d.msg.Students = append(d.msg.Students, WhatsAppStudent{ID: r.ID, Name: r.FullName, Debt: debt.Debt})
		d.msg.Total += debt.Debt
	}

	details := payDetails(c.PayDetails)
	out := make([]WhatsAppMessage, 0, len(order))
	for _, d := range order {
		d.msg.Text = notify.DebtReminderWhatsApp(s.langs.For(d.lang), notify.KindManual, c.Name, today, d.children, details)
		if d.msg.Phone != "" {
			d.msg.URL = notify.WhatsAppURL(d.msg.Phone, d.msg.Text)
		}
		out = append(out, d.msg)
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// WhatsAppReminder — текст и ссылка напоминания об ученике (все дети его родителя с долгом). Ничего не записывает:
// «Скопировать текст» и демо-центр. Телефона нет или он некорректный — текст есть, ссылки нет.
func (s *Service) WhatsAppReminder(ctx context.Context, centerID, studentID uuid.UUID) (WhatsAppMessage, error) {
	c, today, err := s.centerToday(ctx, centerID)
	if err != nil {
		return WhatsAppMessage{}, err
	}
	return s.whatsAppOfStudent(ctx, c, today, studentID)
}

func (s *Service) centerToday(ctx context.Context, centerID uuid.UUID) (sqlc.Center, civil.Date, error) {
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return sqlc.Center{}, civil.Date{}, err
	}
	today, err := civil.Today(s.now(), c.Timezone)
	return c, today, err
}

func (s *Service) whatsAppOfStudent(ctx context.Context, c sqlc.Center, today civil.Date, studentID uuid.UUID) (WhatsAppMessage, error) {
	rows, err := s.q.ListReminderStudents(ctx, sqlc.ListReminderStudentsParams{CenterID: c.ID, Ids: []uuid.UUID{studentID}})
	if err != nil {
		return WhatsAppMessage{}, err
	}
	if len(rows) == 0 {
		return WhatsAppMessage{}, ErrNotFound
	}
	if rows[0].Balance >= 0 {
		return WhatsAppMessage{}, ErrNoDebt
	}
	if rows[0].ParentID == nil {
		return WhatsAppMessage{}, ErrNoPhone
	}
	msgs, err := s.whatsAppForParents(ctx, c, today, []uuid.UUID{*rows[0].ParentID})
	if err != nil {
		return WhatsAppMessage{}, err
	}
	if len(msgs) == 0 { // долг погасили между запросами
		return WhatsAppMessage{}, ErrNoDebt
	}
	return msgs[0], nil
}

// OpenWhatsAppReminder — сотрудник нажал кнопку WhatsApp: отдаёт ссылку и записывает, что она открыта
// (notifications с каналом whatsapp_link, отметка в строке должника, журнал). На лимиты Telegram не влияет:
// открытая ссылка не значит, что родитель получил сообщение.
func (s *Service) OpenWhatsAppReminder(ctx context.Context, actor, centerID, studentID uuid.UUID) (WhatsAppMessage, error) {
	c, today, err := s.centerToday(ctx, centerID)
	if err != nil {
		return WhatsAppMessage{}, err
	}
	if c.IsDemo {
		return WhatsAppMessage{}, ErrDemoCenter
	}
	msg, err := s.whatsAppOfStudent(ctx, c, today, studentID)
	if err != nil {
		return WhatsAppMessage{}, err
	}
	if msg.BadPhone {
		return WhatsAppMessage{}, ErrBadPhone
	}
	if msg.URL == "" {
		return WhatsAppMessage{}, ErrNoPhone
	}
	now := s.now()
	ids := make([]uuid.UUID, 0, len(msg.Students))
	for _, st := range msg.Students {
		ids = append(ids, st.ID)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.InsertWhatsAppNotification(ctx, sqlc.InsertWhatsAppNotificationParams{
			CenterID: c.ID, ParentID: msg.ParentID, Kind: notify.KindManual, StudentIds: ids, CreatedBy: &actor, SentAt: now,
		}); err != nil {
			return err
		}
		if err := q.TouchParentWhatsApp(ctx, sqlc.TouchParentWhatsAppParams{OpenedAt: &now, CenterID: c.ID, ID: msg.ParentID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: c.ID, UserID: &actor, Action: audit.ReminderWhatsApp, Entity: "reminder", EntityID: &msg.ParentID,
			After: map[string]any{"channel": "whatsapp_link", "students": ids, "amount": msg.Total},
		})
	})
	if err != nil {
		return WhatsAppMessage{}, err
	}
	msg.OpenedToday = true
	return msg, nil
}
