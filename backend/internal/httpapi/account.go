package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"okuu/internal/auth"
	"okuu/internal/httpx"
)

// Личный кабинет (/me), выход на всех устройствах и сброс пароля по одноразовой ссылке.

type patchMeRequest struct {
	Name string `json:"name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type newPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

type passwordResetLinkDTO struct {
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

type publicResetDTO struct {
	Name       string  `json:"name"`
	CenterName *string `json:"center_name"`
}

func (h *handlers) patchMe(w http.ResponseWriter, r *http.Request) {
	var req patchMeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	userID, _ := auth.UserID(r.Context())
	if err := h.auth.UpdateName(r.Context(), userID, req.Name); err != nil {
		h.fail(w, r, err)
		return
	}
	h.writeMe(w, r, http.StatusOK, userID)
}

func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	userID, _ := auth.UserID(r.Context())
	err := h.auth.ChangePassword(r.Context(), auth.ChangePasswordInput{
		UserID: userID, SessionToken: auth.SessionToken(r), Current: req.CurrentPassword, New: req.NewPassword, IP: h.clientIP(r),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) logoutAll(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserID(r.Context())
	if err := h.auth.LogoutAll(r.Context(), userID); err != nil {
		h.internalError(w, r, err)
		return
	}
	h.auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) issuePasswordReset(w http.ResponseWriter, r *http.Request) {
	m, _ := auth.MembershipFrom(r.Context())
	userID, ok := uuidParam(w, r, "userId")
	if !ok {
		return
	}
	res, err := h.auth.IssueStaffPasswordReset(r.Context(), m.UserID, m.CenterID, userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, passwordResetLinkDTO{URL: resetURL(h.appOrigin, res.Token), Name: res.UserName, ExpiresAt: res.ExpiresAt})
}

func (h *handlers) getPasswordReset(w http.ResponseWriter, r *http.Request) {
	res, err := h.auth.GetPasswordReset(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, publicResetDTO{Name: res.UserName, CenterName: res.CenterName})
}

func (h *handlers) completePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req newPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		badJSON(w)
		return
	}
	if err := h.auth.CompletePasswordReset(r.Context(), chi.URLParam(r, "token"), req.NewPassword); err != nil {
		h.fail(w, r, err)
		return
	}
	// Если сброс делали в браузере с открытой сессией — её больше нет.
	h.auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// resetURL — ссылка сброса пароля: {APP_ORIGIN}/reset/<токен> (её же печатает команда поддержки).
func resetURL(appOrigin, tok string) string {
	return strings.TrimSuffix(appOrigin, "/") + "/reset/" + tok
}

// ResetURL — для команды server user reset-link.
func ResetURL(appOrigin, tok string) string { return resetURL(appOrigin, tok) }
