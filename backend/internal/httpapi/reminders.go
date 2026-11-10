package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
	"okuu/internal/reminders"
)

type remindersRequest struct {
	StudentIDs []uuid.UUID `json:"student_ids"`
	All        bool        `json:"all"`
}

type skippedDTO struct {
	StudentID uuid.UUID `json:"student_id"`
	Reason    string    `json:"reason"` // no_telegram | stopped | blocked | rate_limited | no_debt | send_failed
}

type remindersResponse struct {
	Sent     int `json:"sent"`     // учеников, о которых напомнили
	Messages int `json:"messages"` // сообщений родителям (дети одного родителя — одно сообщение)
	// учеников во всех отправленных сообщениях, с братьями и сёстрами, которых показали для полноты
	StudentsInMessages int          `json:"students_in_messages"`
	Skipped            []skippedDTO `json:"skipped"`
	// родители, до которых бот не дошёл: готовые тексты и ссылки wa.me
	WhatsApp []whatsAppReminderDTO `json:"whatsapp"`
}

type whatsAppStudentDTO struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
	Debt     int64     `json:"debt"` // тыйыны, > 0
}

// whatsAppReminderDTO — напоминание родителю для отправки со своего WhatsApp.
type whatsAppReminderDTO struct {
	ParentID    uuid.UUID            `json:"parent_id"`
	ParentName  string               `json:"parent_name"`
	Phone       *string              `json:"phone"` // 996XXXXXXXXX; null — ссылки нет
	Students    []whatsAppStudentDTO `json:"students"`
	Total       int64                `json:"total"`
	Text        string               `json:"text"`
	URL         *string              `json:"url"`
	OpenedToday bool                 `json:"opened_today"`
}

func toWhatsAppDTO(m reminders.WhatsAppMessage) whatsAppReminderDTO {
	dto := whatsAppReminderDTO{ParentID: m.ParentID, ParentName: m.ParentName, Total: m.Total, Text: m.Text, OpenedToday: m.OpenedToday,
		Students: make([]whatsAppStudentDTO, 0, len(m.Students))}
	if m.URL != "" {
		dto.Phone, dto.URL = &m.Phone, &m.URL
	}
	for _, s := range m.Students {
		dto.Students = append(dto.Students, whatsAppStudentDTO{ID: s.ID, FullName: s.Name, Debt: s.Debt})
	}
	return dto
}

// getWhatsAppReminder — GET: текст и ссылка, без записи («Скопировать текст», демо-центр).
func (h *handlers) getWhatsAppReminder(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	msg, err := h.reminders.WhatsAppReminder(r.Context(), m.CenterID, studentID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWhatsAppDTO(msg))
}

// openWhatsAppReminder — POST: кнопка WhatsApp; отдаёт ссылку и записывает, что она открыта.
func (h *handlers) openWhatsAppReminder(w http.ResponseWriter, r *http.Request) {
	studentID, ok := uuidParam(w, r, "studentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	msg, err := h.reminders.OpenWhatsAppReminder(r.Context(), m.UserID, m.CenterID, studentID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWhatsAppDTO(msg))
}

type broadcastRequest struct {
	Audience string      `json:"audience"` // center | groups
	GroupIDs []uuid.UUID `json:"group_ids"`
	Text     string      `json:"text"`
}

func (b broadcastRequest) input(key string) reminders.BroadcastInput {
	return reminders.BroadcastInput{Audience: b.Audience, GroupIDs: b.GroupIDs, Text: b.Text, IdempotencyKey: key}
}

type broadcastPreviewDTO struct {
	Telegram     int    `json:"telegram"`
	WhatsApp     int    `json:"whatsapp"`
	NoContact    int    `json:"no_contact"`
	TelegramText string `json:"telegram_text"`
	WhatsAppText string `json:"whatsapp_text"`
}

type broadcastCountsDTO struct {
	TelegramSent   int `json:"telegram_sent"`
	TelegramQueued int `json:"telegram_queued"`
	WhatsAppTotal  int `json:"whatsapp_total"`
	WhatsAppOpened int `json:"whatsapp_opened"`
	NoContact      int `json:"no_contact"`
}

type broadcastDTO struct {
	ID         uuid.UUID          `json:"id"`
	CreatedAt  time.Time          `json:"created_at"`
	Author     string             `json:"author"`
	Audience   string             `json:"audience"`
	GroupNames []string           `json:"group_names"`
	Text       string             `json:"text"`
	Counts     broadcastCountsDTO `json:"counts"`
}

type broadcastLinkDTO struct {
	ParentID   uuid.UUID `json:"parent_id"`
	ParentName string    `json:"parent_name"`
	Students   []string  `json:"students"`
	Text       string    `json:"text"`
	URL        *string   `json:"url"` // null — написать некуда
	Opened     bool      `json:"opened"`
}

type broadcastDetailDTO struct {
	broadcastDTO
	Links []broadcastLinkDTO `json:"links"`
}

func toBroadcastDTO(b reminders.BroadcastSummary) broadcastDTO {
	return broadcastDTO{ID: b.ID, CreatedAt: b.CreatedAt, Author: b.Author, Audience: b.Audience, GroupNames: b.GroupNames, Text: b.Text,
		Counts: broadcastCountsDTO{TelegramSent: b.Counts.TelegramSent, TelegramQueued: b.Counts.TelegramQueued, WhatsAppTotal: b.Counts.WhatsAppTotal,
			WhatsAppOpened: b.Counts.WhatsAppOpened, NoContact: b.Counts.NoContact}}
}

func toBroadcastDetailDTO(d reminders.BroadcastDetail) broadcastDetailDTO {
	out := broadcastDetailDTO{broadcastDTO: toBroadcastDTO(d.BroadcastSummary), Links: make([]broadcastLinkDTO, 0, len(d.Links))}
	for _, l := range d.Links {
		link := broadcastLinkDTO{ParentID: l.ParentID, ParentName: l.ParentName, Students: l.Students, Text: l.Text, Opened: l.Opened}
		if l.URL != "" {
			link.URL = &l.URL
		}
		out.Links = append(out.Links, link)
	}
	return out
}

// previewBroadcast — POST /broadcasts/preview: сколько родителей по каналам и текст сообщения.
func (h *handlers) previewBroadcast(w http.ResponseWriter, r *http.Request) {
	var req broadcastRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	p, err := h.reminders.PreviewBroadcast(r.Context(), m.CenterID, req.input(""))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, broadcastPreviewDTO{Telegram: p.Telegram, WhatsApp: p.WhatsApp, NoContact: p.NoContact,
		TelegramText: p.TelegramText, WhatsAppText: p.WhatsAppText})
}

// createBroadcast — POST /broadcasts: новая — 201; повтор с тем же Idempotency-Key — 200.
func (h *handlers) createBroadcast(w http.ResponseWriter, r *http.Request) {
	var req broadcastRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	d, created, err := h.reminders.CreateBroadcast(r.Context(), m.UserID, m.CenterID, req.input(r.Header.Get("Idempotency-Key")))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, toBroadcastDetailDTO(d))
}

func (h *handlers) listBroadcasts(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	rows, err := h.reminders.ListBroadcasts(r.Context(), m.CenterID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]broadcastDTO, 0, len(rows))
	for _, b := range rows {
		items = append(items, toBroadcastDTO(b))
	}
	httpx.JSON(w, http.StatusOK, listOf(items))
}

func (h *handlers) getBroadcast(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "broadcastId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	d, err := h.reminders.GetBroadcast(r.Context(), m.CenterID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toBroadcastDetailDTO(d))
}

// openBroadcastLink — сотрудник открыл ссылку WhatsApp получателя рассылки.
func (h *handlers) openBroadcastLink(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "broadcastId")
	if !ok {
		return
	}
	parentID, ok := uuidParam(w, r, "parentId")
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	if err := h.reminders.OpenBroadcastLink(r.Context(), m.UserID, m.CenterID, id, parentID); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendReminders — POST /c/{id}/reminders: «Напомнить» по ученикам или по всем должникам (R17–R18).
func (h *handlers) sendReminders(w http.ResponseWriter, r *http.Request) {
	var req remindersRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	res, err := h.reminders.Manual(r.Context(), m.UserID, m.CenterID, reminders.ManualInput{StudentIDs: req.StudentIDs, All: req.All})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := remindersResponse{Sent: res.Sent, Messages: res.Messages, StudentsInMessages: res.InMessages,
		Skipped: make([]skippedDTO, 0, len(res.Skipped)), WhatsApp: make([]whatsAppReminderDTO, 0, len(res.WhatsApp))}
	for _, s := range res.Skipped {
		out.Skipped = append(out.Skipped, skippedDTO{StudentID: s.StudentID, Reason: s.Reason})
	}
	for _, m := range res.WhatsApp {
		out.WhatsApp = append(out.WhatsApp, toWhatsAppDTO(m))
	}
	httpx.JSON(w, http.StatusOK, out)
}
