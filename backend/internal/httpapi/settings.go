package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/httpx"
	"okuu/internal/optional"
	"okuu/internal/reports"
	"okuu/internal/settings"
)

// --- Настройки центра (S8) ---

type settingsDTO struct {
	Name               string      `json:"name"`
	Phone              *string     `json:"phone"`
	Timezone           string      `json:"timezone"`
	BillingDay         int         `json:"billing_day"`
	BillingDayNext     *int        `json:"billing_day_next"`      // новый день, ещё не вступивший в силу
	BillingDayNextFrom *civil.Date `json:"billing_day_next_from"` // с какого периода
	DueDays            int         `json:"due_days"`
	ExcusedDeducts     bool        `json:"excused_deducts"`
	RemindersOn        bool        `json:"reminders_on"`
	OverdueRepeat      string      `json:"overdue_repeat"` // weekly | off
	NotifyAbsent       bool        `json:"notify_absent"`  // сообщать родителю «ребёнок не пришёл»
	PayDetails         *string     `json:"pay_details"`
	Plan               string      `json:"plan"`
	FinikConnected     bool        `json:"finik_connected"` // сами ключи Finik наружу не отдаются
}

type patchSettingsRequest struct {
	Name           optional.Field[string] `json:"name"`
	Phone          optional.Field[string] `json:"phone"`
	Timezone       optional.Field[string] `json:"timezone"`
	BillingDay     optional.Field[int]    `json:"billing_day"`
	DueDays        optional.Field[int]    `json:"due_days"`
	ExcusedDeducts optional.Field[bool]   `json:"excused_deducts"`
	RemindersOn    optional.Field[bool]   `json:"reminders_on"`
	OverdueRepeat  optional.Field[string] `json:"overdue_repeat"`
	NotifyAbsent   optional.Field[bool]   `json:"notify_absent"`
	PayDetails     optional.Field[string] `json:"pay_details"`
}

func (h *handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	s, err := h.settings.Get(r.Context(), m.CenterID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toSettingsDTO(s))
}

func (h *handlers) patchSettings(w http.ResponseWriter, r *http.Request) {
	var req patchSettingsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	s, err := h.settings.Update(r.Context(), m.UserID, m.CenterID, settings.Patch(req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toSettingsDTO(s))
}

func toSettingsDTO(s settings.Settings) settingsDTO {
	return settingsDTO{
		Name: s.Name, Phone: s.Phone, Timezone: s.Timezone, BillingDay: s.BillingDay, BillingDayNext: s.BillingDayNext,
		BillingDayNextFrom: s.BillingDayNextFrom, DueDays: s.DueDays, ExcusedDeducts: s.ExcusedDeducts,
		RemindersOn: s.RemindersOn, OverdueRepeat: s.OverdueRepeat, NotifyAbsent: s.NotifyAbsent, PayDetails: s.PayDetails, Plan: s.Plan, FinikConnected: s.FinikConnected,
	}
}

// --- Отчёт за месяц (S7, F7) ---

type collectedDTO struct {
	Total    int64 `json:"total"`
	Cash     int64 `json:"cash"`
	Transfer int64 `json:"transfer"`
	Card     int64 `json:"card"`
	Finik    int64 `json:"finik"`
}

type reportDTO struct {
	Month             string       `json:"month"`
	Period            civil.Date   `json:"period"`
	PeriodEnd         civil.Date   `json:"period_end"`
	Charged           int64        `json:"charged"`
	Collected         collectedDTO `json:"collected"`
	Refunds           int64        `json:"refunds"`
	CollectedNet      int64        `json:"collected_net"`
	Corrections       int64        `json:"corrections"`
	DebtEnd           int64        `json:"debt_end"`
	DebtEndNote       string       `json:"debt_end_note"`
	DebtorsEnd        int          `json:"debtors_end"`
	StudentsActiveEnd int64        `json:"students_active_end"`
	Joined            int64        `json:"joined"`
	Left              int64        `json:"left"`
}

// monthParam — ?m=ГГГГ-ММ, по умолчанию текущий месяц центра.
func (h *handlers) monthParam(w http.ResponseWriter, r *http.Request) (civil.Date, bool) {
	v := r.URL.Query().Get("m")
	if v == "" {
		m, _ := auth.MembershipFrom(r.Context())
		d, err := h.reports.CurrentMonth(r.Context(), m.CenterID)
		if err != nil {
			h.fail(w, r, err)
			return civil.Date{}, false
		}
		return d, true
	}
	d, err := civil.Parse(v + "-01")
	if err != nil || len(v) != 7 {
		validationError(w, "m", "m — месяц в формате ГГГГ-ММ")
		return civil.Date{}, false
	}
	return d, true
}

func (h *handlers) monthReport(w http.ResponseWriter, r *http.Request) {
	month, ok := h.monthParam(w, r)
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	rep, err := h.reports.Month(r.Context(), m.CenterID, month)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, reportDTO{
		Month: rep.Month, Period: rep.Period, PeriodEnd: rep.PeriodEnd, Charged: rep.Charged,
		Collected: collectedDTO{Total: rep.Collected.Total, Cash: rep.Collected.Cash, Transfer: rep.Collected.Transfer,
			Card: rep.Collected.Card, Finik: rep.Collected.Finik},
		Refunds: rep.Refunds, CollectedNet: rep.CollectedNet, Corrections: rep.Corrections, DebtEnd: rep.DebtEnd,
		DebtEndNote: reports.DebtEndNote, DebtorsEnd: rep.DebtorsEnd, StudentsActiveEnd: rep.StudentsActiveEnd,
		Joined: rep.Joined, Left: rep.Left,
	})
}

func (h *handlers) monthReportXLSX(w http.ResponseWriter, r *http.Request) {
	month, ok := h.monthParam(w, r)
	if !ok {
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	data, err := h.reports.ExportXLSX(r.Context(), m.CenterID, month)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	name := "okuu-report-" + month.String()[:7] + ".xlsx"
	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(data)
}

// --- Журнал действий (S9, F8) ---

type auditUserDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type auditItemDTO struct {
	ID       int64           `json:"id"`
	At       time.Time       `json:"at"`
	User     *auditUserDTO   `json:"user"`      // null — система
	UserName string          `json:"user_name"` // «Система», если user = null
	Action   string          `json:"action"`
	Entity   string          `json:"entity"`
	EntityID *uuid.UUID      `json:"entity_id"`
	Before   json.RawMessage `json:"before"`
	After    json.RawMessage `json:"after"`
	// Для строки журнала, без разбора before/after на клиенте:
	Students []auditStudentDTO `json:"students"` // ledger.* — ученик операции, attendance.* — отмеченные; иначе []
	Amount   *int64            `json:"amount"`   // ledger.* — сумма операции в тыйынах, со знаком (долг < 0)
	Lesson   *auditLessonDTO   `json:"lesson"`   // attendance.* — занятие
}

type auditStudentDTO struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

type auditLessonDTO struct {
	Date      civil.Date `json:"date"`
	GroupName string     `json:"group_name"`
}

func (h *handlers) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := audit.Filter{Action: q.Get("action"), Cursor: q.Get("cursor")}
	if v := q.Get("user"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			validationError(w, "user", "user — id сотрудника")
			return
		}
		f.UserID = &id
	}
	for _, p := range []struct {
		name string
		dst  **civil.Date
	}{{"from", &f.From}, {"to", &f.To}} {
		if v := q.Get(p.name); v != "" {
			d, err := civil.Parse(v)
			if err != nil {
				validationError(w, p.name, p.name+" — дата ГГГГ-ММ-ДД")
				return
			}
			*p.dst = &d
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > audit.MaxLimit {
			validationError(w, "limit", "limit — число от 1 до "+strconv.Itoa(audit.MaxLimit))
			return
		}
		f.Limit = n
	}

	m, _ := auth.MembershipFrom(r.Context())
	rows, next, err := audit.List(r.Context(), h.pool, m.CenterID, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]auditItemDTO, 0, len(rows))
	for _, a := range rows {
		it := auditItemDTO{ID: a.ID, At: a.At, UserName: audit.SystemName, Action: a.Action, Entity: a.Entity,
			EntityID: a.EntityID, Before: rawJSON(a.Before), After: rawJSON(a.After), Students: []auditStudentDTO{}}
		switch {
		case a.LedgerStudentID != nil && a.LedgerStudentName != nil:
			it.Students = append(it.Students, auditStudentDTO{ID: *a.LedgerStudentID, FullName: *a.LedgerStudentName})
			it.Amount = a.LedgerAmount
		case len(a.MarkedStudents) > 0:
			if err := json.Unmarshal(a.MarkedStudents, &it.Students); err != nil {
				h.fail(w, r, err)
				return
			}
		}
		if a.LessonDate != nil && a.LessonGroup != nil {
			it.Lesson = &auditLessonDTO{Date: *a.LessonDate, GroupName: *a.LessonGroup}
		}
		if a.UserID != nil {
			name := ""
			if a.UserName != nil {
				name = *a.UserName
			}
			it.User, it.UserName = &auditUserDTO{ID: *a.UserID, Name: name}, name
		}
		out = append(out, it)
	}
	httpx.JSON(w, http.StatusOK, pageOf(out, next))
}

func rawJSON(b []byte) json.RawMessage {
	if b == nil {
		return json.RawMessage("null")
	}
	return b
}
