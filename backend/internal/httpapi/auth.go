package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/civil"
	"okuu/internal/httpx"
)

// meDTO — ответ GET /me, register и login.
type meDTO struct {
	ID      uuid.UUID     `json:"id"`
	Name    string        `json:"name"`
	Phone   string        `json:"phone"`
	Centers []meCenterDTO `json:"centers"`
	// IsPlatformAdmin — суперадмин платформы: в интерфейсе виден раздел /admin.
	IsPlatformAdmin bool `json:"is_platform_admin"`
}

type meCenterDTO struct {
	ID     uuid.UUID  `json:"id"`
	Name   string     `json:"name"`
	Role   string     `json:"role"`
	Status string     `json:"status"` // pending — заявка, blocked — приостановлен: данные центра недоступны
	Today  civil.Date `json:"today"`  // «сегодня» в часовом поясе центра
	IsDemo bool       `json:"is_demo"`
}

type registerRequest struct {
	Phone      string `json:"phone"`
	Password   string `json:"password"`
	Name       string `json:"name"`
	CenterName string `json:"center_name"`
	// RegistrationToken — из ссылки {APP_ORIGIN}/register/{токен}; в режиме invite обязателен.
	RegistrationToken string `json:"registration_token"`
}

// registrationDTO — GET /auth/registration: как устроена регистрация (для страницы /register).
type registrationDTO struct {
	Mode            string  `json:"mode"`             // invite | approval | open
	SupportWhatsApp *string `json:"support_whatsapp"` // номер для wa.me; null — не задан
}

func (h *handlers) registrationInfo(w http.ResponseWriter, r *http.Request) {
	dto := registrationDTO{Mode: h.auth.RegistrationMode()}
	if h.supportWhatsApp != "" {
		dto.SupportWhatsApp = &h.supportWhatsApp
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// checkRegistrationLink — GET /auth/registration/{token}: ссылка действует (200) или нет (404).
func (h *handlers) checkRegistrationLink(w http.ResponseWriter, r *http.Request) {
	ok, err := h.auth.RegistrationLinkActive(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if !ok {
		h.fail(w, r, auth.ErrRegistrationInvalid)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"valid": true})
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	sess, err := h.auth.Register(r.Context(), auth.RegisterInput{
		Phone:      req.Phone,
		Password:   req.Password,
		Name:       req.Name,
		CenterName: req.CenterName,
		UserAgent:  r.UserAgent(),
		Token:      req.RegistrationToken,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.auth.SetSessionCookie(w, sess)
	h.writeMe(w, r, http.StatusCreated, sess.UserID)
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	sess, err := h.auth.Login(r.Context(), auth.LoginInput{
		Phone:     req.Phone,
		Password:  req.Password,
		UserAgent: r.UserAgent(),
		IP:        h.clientIP(r),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.auth.SetSessionCookie(w, sess)
	h.writeMe(w, r, http.StatusOK, sess.UserID)
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.Logout(r.Context(), auth.SessionToken(r)); err != nil {
		h.internalError(w, r, err)
		return
	}
	h.auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserID(r.Context())
	h.writeMe(w, r, http.StatusOK, userID)
}

func (h *handlers) writeMe(w http.ResponseWriter, r *http.Request, status int, userID uuid.UUID) {
	p, err := h.auth.Profile(r.Context(), userID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	dto := meDTO{ID: p.User.ID, Name: p.User.Name, Phone: p.User.Phone, Centers: make([]meCenterDTO, 0, len(p.Centers)),
		IsPlatformAdmin: p.User.IsPlatformAdmin}
	for _, c := range p.Centers {
		dto.Centers = append(dto.Centers, meCenterDTO{ID: c.CenterID, Name: c.CenterName, Role: c.Role, Status: c.CenterStatus, Today: c.Today, IsDemo: c.IsDemo})
	}
	httpx.JSON(w, status, dto)
}
