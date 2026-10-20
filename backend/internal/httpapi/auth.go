package httpapi

import (
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpx"
)

// meDTO — ответ GET /me, register и login.
type meDTO struct {
	ID      uuid.UUID     `json:"id"`
	Name    string        `json:"name"`
	Phone   string        `json:"phone"`
	Centers []meCenterDTO `json:"centers"`
}

type meCenterDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Role string    `json:"role"`
}

type registerRequest struct {
	Phone      string `json:"phone"`
	Password   string `json:"password"`
	Name       string `json:"name"`
	CenterName string `json:"center_name"`
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
	})
	if err != nil {
		h.authError(w, r, err)
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
		IP:        clientIP(r),
	})
	if err != nil {
		h.authError(w, r, err)
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
	dto := meDTO{ID: p.User.ID, Name: p.User.Name, Phone: p.User.Phone, Centers: make([]meCenterDTO, 0, len(p.Centers))}
	for _, c := range p.Centers {
		dto.Centers = append(dto.Centers, meCenterDTO{ID: c.CenterID, Name: c.CenterName, Role: c.Role})
	}
	httpx.JSON(w, status, dto)
}

// authError переводит ошибки auth в ответы API.
func (h *handlers) authError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *auth.ValidationError
	var tooMany *auth.TooManyAttemptsError
	switch {
	case errors.As(err, &verr):
		httpx.Error(w, http.StatusBadRequest, "validation", verr.Message)
	case errors.Is(err, auth.ErrPhoneTaken):
		httpx.Error(w, http.StatusConflict, "phone_taken", "Этот телефон уже зарегистрирован")
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "Неверный телефон или пароль")
	case errors.As(err, &tooMany):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(tooMany.RetryAfter.Seconds()))))
		httpx.Error(w, http.StatusTooManyRequests, "too_many_attempts", "Слишком много попыток входа. Попробуйте позже")
	default:
		h.internalError(w, r, err)
	}
}

func badJSON(w http.ResponseWriter) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса")
}

// clientIP — адрес клиента из соединения. За обратным прокси (Caddy) нужно будет
// брать X-Forwarded-For от доверенного прокси — см. отчёт шага 4.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
