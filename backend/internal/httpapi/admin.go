package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/httpx"
)

// Панель суперадмина (/api/v1/admin/*). Только агрегаты: ни учеников, ни родителей, ни операций,
// ни журнала центров в ответах нет — тест TestAdminNoPersonalData это проверяет.

type adminOwnerDTO struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type adminCenterDTO struct {
	ID             uuid.UUID     `json:"id"`
	Name           string        `json:"name"`
	Status         string        `json:"status"`
	BlockedReason  *string       `json:"blocked_reason"`
	Plan           string        `json:"plan"`
	PaidUntil      *civil.Date   `json:"paid_until"`
	PaymentOverdue bool          `json:"payment_overdue"` // paid_until прошёл; центр при этом НЕ блокируется
	IsDemo         bool          `json:"is_demo"`
	CreatedAt      time.Time     `json:"created_at"`
	Owner          adminOwnerDTO `json:"owner"`
	ActiveStudents int64         `json:"active_students"`
	LastActivity   *time.Time    `json:"last_activity"` // последний вход любого сотрудника; null — не входили
}

type adminStudentsDTO struct {
	Active int64 `json:"active"`
	Paused int64 `json:"paused"`
	Left   int64 `json:"left"`
}

type adminCenterCardDTO struct {
	adminCenterDTO
	Timezone        string             `json:"timezone"`
	Students        adminStudentsDTO   `json:"students"`
	Groups          int64              `json:"groups"`
	Staff           int64              `json:"staff"`
	TelegramParents int64              `json:"telegram_parents"`
	Audit           []platformAuditDTO `json:"audit"`
}

type platformAuditDTO struct {
	ID         int64           `json:"id"`
	At         time.Time       `json:"at"`
	Action     string          `json:"action"`
	AdminName  *string         `json:"admin_name"` // null — консольная команда
	CenterID   *uuid.UUID      `json:"center_id"`
	CenterName *string         `json:"center_name"`
	Details    json.RawMessage `json:"details"`
}

type registrationLinkDTO struct {
	ID            uuid.UUID     `json:"id"`
	Note          string        `json:"note"`
	Status        string        `json:"status"` // active | used | revoked | expired
	CreatedAt     time.Time     `json:"created_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	UsedAt        *time.Time    `json:"used_at"`
	RevokedAt     *time.Time    `json:"revoked_at"`
	Center        *centerRefDTO `json:"center"`
	CreatedByName *string       `json:"created_by_name"` // null — консоль
}

type centerRefDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type issuedRegistrationLinkDTO struct {
	ID        uuid.UUID `json:"id"`
	Note      string    `json:"note"`
	URL       string    `json:"url"` // показывается один раз: в БД только хэш токена
	ExpiresAt time.Time `json:"expires_at"`
}

type ownerResetDTO struct {
	URL       string    `json:"url"`
	UserName  string    `json:"user_name"`
	ExpiresAt time.Time `json:"expires_at"`
}

func adminCenterOf(r sqlc.ListAdminCentersRow, today civil.Date) adminCenterDTO {
	return adminCenterDTO{
		ID: r.ID, Name: r.Name, Status: r.Status, BlockedReason: r.BlockedReason, Plan: r.Plan, PaidUntil: r.PaidUntil,
		PaymentOverdue: r.PaidUntil != nil && r.PaidUntil.Before(today),
		IsDemo:         r.IsDemo, CreatedAt: r.CreatedAt, Owner: adminOwnerDTO{Name: r.OwnerName, Phone: r.OwnerPhone},
		ActiveStudents: r.ActiveStudents, LastActivity: r.LastActivity,
	}
}

func platformAuditOf(rows []sqlc.ListPlatformAuditRow) []platformAuditDTO {
	out := make([]platformAuditDTO, 0, len(rows))
	for _, a := range rows {
		d := json.RawMessage(a.Details)
		if len(d) == 0 {
			d = json.RawMessage("null")
		}
		out = append(out, platformAuditDTO{ID: a.ID, At: a.At, Action: a.Action, AdminName: a.AdminName, CenterID: a.CenterID, CenterName: a.CenterName, Details: d})
	}
	return out
}

func adminID(r *http.Request) uuid.UUID {
	id, _ := auth.UserID(r.Context())
	return id
}

func (h *handlers) adminListCenters(w http.ResponseWriter, r *http.Request) {
	rows, err := h.platform.ListCenters(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	today := h.platform.Today()
	items := make([]adminCenterDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminCenterOf(row, today))
	}
	httpx.JSON(w, http.StatusOK, listOf(items))
}

func (h *handlers) adminGetCenter(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	c, err := h.platform.GetCenter(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	row := c.GetAdminCenterRow
	base := adminCenterOf(sqlc.ListAdminCentersRow{
		ID: row.ID, Name: row.Name, Status: row.Status, BlockedReason: row.BlockedReason, Plan: row.Plan, PaidUntil: row.PaidUntil,
		IsDemo: row.IsDemo, CreatedAt: row.CreatedAt, OwnerName: row.OwnerName, OwnerPhone: row.OwnerPhone,
		ActiveStudents: row.ActiveStudents, LastActivity: row.LastActivity,
	}, h.platform.Today())
	httpx.JSON(w, http.StatusOK, adminCenterCardDTO{
		adminCenterDTO: base, Timezone: row.Timezone,
		Students: adminStudentsDTO{Active: row.ActiveStudents, Paused: row.PausedStudents, Left: row.LeftStudents},
		Groups:   row.GroupsCount, Staff: row.StaffCount, TelegramParents: row.TelegramParents, Audit: platformAuditOf(c.Audit),
	})
}

// adminCenterAction — approve, block, unblock: ответ — обновлённая карточка.
func (h *handlers) adminCenterAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, "id")
		if !ok {
			return
		}
		var err error
		switch action {
		case "approve":
			err = h.platform.Approve(r.Context(), adminID(r), id)
		case "unblock":
			err = h.platform.Unblock(r.Context(), adminID(r), id)
		case "block":
			var req struct {
				Reason string `json:"reason"`
			}
			if httpx.DecodeJSON(w, r, &req) != nil {
				badJSON(w)
				return
			}
			err = h.platform.Block(r.Context(), adminID(r), id, req.Reason)
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.adminGetCenter(w, r)
	}
}

func (h *handlers) adminPatchCenter(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Plan      string      `json:"plan"`
		PaidUntil *civil.Date `json:"paid_until"`
	}
	if httpx.DecodeJSON(w, r, &req) != nil {
		badJSON(w)
		return
	}
	if err := h.platform.SetPlan(r.Context(), adminID(r), id, req.Plan, req.PaidUntil); err != nil {
		h.fail(w, r, err)
		return
	}
	h.adminGetCenter(w, r)
}

func (h *handlers) adminOwnerPasswordReset(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.platform.OwnerPasswordReset(r.Context(), adminID(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, ownerResetDTO{URL: resetURL(h.appOrigin, res.Token), UserName: res.UserName, ExpiresAt: res.ExpiresAt})
}

func (h *handlers) adminListLinks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.platform.ListRegistrationLinks(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	now := h.platform.Now()
	items := make([]registrationLinkDTO, 0, len(rows))
	for _, l := range rows {
		dto := registrationLinkDTO{ID: l.ID, Note: l.Note, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt, UsedAt: l.UsedAt,
			RevokedAt: l.RevokedAt, CreatedByName: l.CreatedByName}
		switch {
		case l.UsedAt != nil:
			dto.Status = "used"
		case l.RevokedAt != nil:
			dto.Status = "revoked"
		case !l.ExpiresAt.After(now):
			dto.Status = "expired"
		default:
			dto.Status = "active"
		}
		if l.CenterID != nil && l.CenterName != nil {
			dto.Center = &centerRefDTO{ID: *l.CenterID, Name: *l.CenterName}
		}
		items = append(items, dto)
	}
	httpx.JSON(w, http.StatusOK, listOf(items))
}

func (h *handlers) adminCreateLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if httpx.DecodeJSON(w, r, &req) != nil {
		badJSON(w)
		return
	}
	admin := adminID(r)
	link, err := h.platform.CreateRegistrationLink(r.Context(), &admin, req.Note)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, issuedRegistrationLinkDTO{ID: link.ID, Note: link.Note, URL: RegistrationURL(h.appOrigin, link.Token), ExpiresAt: link.ExpiresAt})
}

func (h *handlers) adminRevokeLink(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.platform.RevokeRegistrationLink(r.Context(), adminID(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) adminListAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			validationError(w, "limit", "limit — число от 1 до 200")
			return
		}
		limit = n
	}
	rows, err := h.platform.ListAudit(r.Context(), limit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, listOf(platformAuditOf(rows)))
}
