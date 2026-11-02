package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"okuu/internal/httpx"
	"okuu/internal/payments"
)

// Публичная страница оплаты (B2): без входа, по токену из ссылки. Без телефонов, родителя и id.

// Лимит запросов к /pay по IP: токены не перебрать, но и страницу не задавить.
const (
	payRateLimit  = 30
	payRateWindow = time.Minute
)

type payLinkDTO struct {
	CenterName  string    `json:"center_name"`
	StudentName string    `json:"student_name"` // ФИО ученика, как записано в центре
	Amount      int64     `json:"amount"`
	Provider    string    `json:"provider"`              // manual | finik
	PayDetails  *string   `json:"pay_details,omitempty"` // реквизиты — только для manual
	Status      string    `json:"status"`                // pending | paid
	ExpiresAt   time.Time `json:"expires_at"`
}

type payStartDTO struct {
	Provider   string  `json:"provider"`
	Amount     int64   `json:"amount"`
	PayURL     *string `json:"pay_url,omitempty"`
	QRPayload  *string `json:"qr_payload,omitempty"`
	PayDetails *string `json:"pay_details,omitempty"`
}

// payLimited — 429, если IP превысил лимит; каждый запрос считается.
func (h *handlers) payLimited(w http.ResponseWriter, r *http.Request) bool {
	ip := "pay:" + h.clientIP(r)
	if blocked, retry := h.payLimiter.Blocked(ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
		httpx.Error(w, http.StatusTooManyRequests, "too_many_requests", "Слишком много запросов. Попробуйте через минуту")
		return true
	}
	h.payLimiter.Fail(ip)
	return false
}

// configDTO — GET /config: что включено на сервере (для экранов, которые от этого зависят).
type configDTO struct {
	OnlinePayments bool `json:"online_payments"` // ONLINE_PAYMENTS_ENABLED: оплата по ссылке и Finik
}

func (h *handlers) publicConfig(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, configDTO{OnlinePayments: h.payments != nil})
}

// getPayLink — GET /api/v1/pay/{token}. Неизвестная и истёкшая ссылка — одинаково 404; онлайн-оплата
// выключена (ONLINE_PAYMENTS_ENABLED=false) — тот же 404 на любую ссылку.
func (h *handlers) getPayLink(w http.ResponseWriter, r *http.Request) {
	if h.payLimited(w, r) {
		return
	}
	if h.payments == nil {
		h.fail(w, r, payments.ErrNotFound)
		return
	}
	l, err := h.payments.Get(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, payLinkDTO{
		CenterName: l.CenterName, StudentName: l.StudentName, Amount: l.Amount, Provider: l.Provider,
		PayDetails: l.PayDetails, Status: l.Status, ExpiresAt: l.ExpiresAt,
	})
}

// startPayment — POST /api/v1/pay/{token}/start: ссылка банка / QR или реквизиты.
func (h *handlers) startPayment(w http.ResponseWriter, r *http.Request) {
	if h.payLimited(w, r) {
		return
	}
	if h.payments == nil {
		h.fail(w, r, payments.ErrNotFound)
		return
	}
	s, err := h.payments.Start(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, payStartDTO{Provider: s.Provider, Amount: s.Amount, PayURL: s.PayURL, QRPayload: s.QRPayload, PayDetails: s.PayDetails})
}

// providerWebhook — POST /webhooks/finik: подпись проверяет провайдер (неверная — 401),
// обработка идемпотентна по id платежа.
func (h *handlers) providerWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, httpx.MaxBodyBytes)
	ev, err := h.payments.Online().ParseWebhook(r)
	if err == nil {
		err = h.payments.HandleWebhook(r.Context(), ev)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
