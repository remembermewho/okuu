package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
	"okuu/internal/staff"
	"okuu/internal/validate"
)

type staffMemberDTO struct {
	UserID uuid.UUID `json:"user_id"`
	Name   string    `json:"name"`
	Phone  string    `json:"phone"`
	Role   string    `json:"role"`
}

type inviteDTO struct {
	ID        uuid.UUID `json:"id"`
	Role      string    `json:"role"`
	Name      *string   `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// createdInviteDTO — единственный ответ, где есть открытый токен (внутри url).
type createdInviteDTO struct {
	inviteDTO
	URL string `json:"url"`
}

type publicInviteDTO struct {
	CenterName string    `json:"center_name"`
	Role       string    `json:"role"`
	Name       *string   `json:"name"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type createInviteRequest struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

type acceptInviteRequest struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

type changeRoleRequest struct {
	Role string `json:"role"`
}

func (h *handlers) listStaff(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	list, err := h.staff.List(r.Context(), m.CenterID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	out := make([]staffMemberDTO, 0, len(list))
	for _, s := range list {
		out = append(out, toStaffDTO(s))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handlers) createInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	inv, err := h.staff.CreateInvite(r.Context(), m.UserID, m.CenterID, req.Role, req.Name)
	if err != nil {
		h.staffError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, createdInviteDTO{
		inviteDTO: toInviteDTO(inv.Invite),
		URL:       strings.TrimSuffix(h.appOrigin, "/") + "/invite/" + inv.Token,
	})
}

func (h *handlers) listInvites(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	list, err := h.staff.ListInvites(r.Context(), m.CenterID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	out := make([]inviteDTO, 0, len(list))
	for _, inv := range list {
		out = append(out, toInviteDTO(inv))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handlers) revokeInvite(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	id, ok := uuidParam(w, r, "inviteId")
	if !ok {
		return
	}
	if err := h.staff.RevokeInvite(r.Context(), m.UserID, m.CenterID, id); err != nil {
		h.staffError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) changeStaffRole(w http.ResponseWriter, r *http.Request) {
	var req changeRoleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	m, _ := auth.MembershipFrom(r.Context())
	userID, ok := uuidParam(w, r, "userId")
	if !ok {
		return
	}
	member, err := h.staff.ChangeRole(r.Context(), m.UserID, m.CenterID, userID, req.Role)
	if err != nil {
		h.staffError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toStaffDTO(member))
}

func (h *handlers) removeStaff(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	userID, ok := uuidParam(w, r, "userId")
	if !ok {
		return
	}
	if err := h.staff.Remove(r.Context(), m.UserID, m.CenterID, userID); err != nil {
		h.staffError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getInvite — публичная страница приглашения.
func (h *handlers) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := h.staff.GetInvite(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.staffError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, publicInviteDTO{CenterName: inv.CenterName, Role: inv.Role, Name: inv.Name, ExpiresAt: inv.ExpiresAt})
}

// acceptInvite — публичный: вошедший пользователь принимает без тела, новый — с {name, phone, password}.
// Отвечает как /me (с новым центром в списке); новому пользователю выставляет cookie сессии.
func (h *handlers) acceptInvite(w http.ResponseWriter, r *http.Request) {
	in := staff.AcceptInput{Token: chi.URLParam(r, "token"), UserAgent: r.UserAgent()}

	sess, extended, err := h.auth.Authenticate(r.Context(), auth.SessionToken(r))
	switch {
	case err == nil:
		in.UserID = &sess.UserID
		if extended {
			h.auth.SetSessionCookie(w, sess)
		}
	case errors.Is(err, auth.ErrNoSession):
		var req acceptInviteRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			badJSON(w)
			return
		}
		in.Name, in.Phone, in.Password = req.Name, req.Phone, req.Password
	default:
		h.internalError(w, r, err)
		return
	}

	res, err := h.staff.Accept(r.Context(), in)
	if err != nil {
		h.staffError(w, r, err)
		return
	}
	if res.Session != nil {
		h.auth.SetSessionCookie(w, *res.Session)
	}
	h.writeMe(w, r, http.StatusOK, res.UserID)
}

// staffError переводит ошибки staff (и валидации) в ответы API.
func (h *handlers) staffError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *validate.Error
	switch {
	case errors.As(err, &verr):
		httpx.Error(w, http.StatusBadRequest, "validation", verr.Message)
	case errors.Is(err, staff.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "Не найдено")
	case errors.Is(err, staff.ErrInviteInvalid):
		httpx.Error(w, http.StatusNotFound, "invite_invalid", "Приглашение недействительно или уже использовано")
	case errors.Is(err, staff.ErrLoginRequired):
		httpx.Error(w, http.StatusConflict, "login_required", "Этот телефон уже зарегистрирован. Войдите и примите приглашение снова")
	case errors.Is(err, staff.ErrAlreadyMember):
		httpx.Error(w, http.StatusConflict, "already_member", "Вы уже сотрудник этого центра")
	case errors.Is(err, staff.ErrOwnerProtected):
		httpx.Error(w, http.StatusConflict, "owner_protected", "Владельца центра нельзя изменить или удалить")
	case errors.Is(err, staff.ErrTeacherHasGroups):
		httpx.Error(w, http.StatusConflict, "teacher_has_groups", "У преподавателя есть группы — сначала назначьте им другого преподавателя")
	default:
		h.internalError(w, r, err)
	}
}

// uuidParam читает uuid из пути; некорректный — 404, как и несуществующий.
func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Не найдено")
		return uuid.Nil, false
	}
	return id, true
}

func toStaffDTO(m staff.Member) staffMemberDTO {
	return staffMemberDTO{UserID: m.UserID, Name: m.Name, Phone: m.Phone, Role: m.Role}
}

func toInviteDTO(inv staff.Invite) inviteDTO {
	return inviteDTO{ID: inv.ID, Role: inv.Role, Name: inv.Name, ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt}
}
