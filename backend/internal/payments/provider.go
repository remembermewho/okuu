// Package payments — оплата родителем по ссылке (B2): ссылки оплаты, провайдеры, вебхук, сверка.
// Провайдер спрятан за интерфейсом Provider. По умолчанию — Manual: страница показывает сумму
// и реквизиты центра, оплату отмечает администратор. Finik — payments/finik (пока заглушка).
package payments

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// Статусы платежа у провайдера.
const (
	StatusPending = "pending"
	StatusPaid    = "paid"
	StatusFailed  = "failed"
)

// ProviderManual — оплата по реквизитам центра, без автоподтверждения.
const ProviderManual = "manual"

var (
	// ErrBadSignature — подпись вебхука не сошлась.
	ErrBadSignature = errors.New("payments: неверная подпись вебхука")
	// ErrNotImplemented — провайдер этого не умеет (manual) или ещё не реализован (finik).
	ErrNotImplemented = errors.New("payments: не поддерживается провайдером")
)

// Center — центр, от имени которого создаётся платёж. TODO(finik): ключи мерчанта центра
// (centers.finik_creds, AES-GCM) — когда будет известен API Finik.
type Center struct {
	ID   uuid.UUID
	Name string
}

// Payment — созданный у провайдера платёж: ссылка банка и/или данные QR.
type Payment struct {
	ProviderPaymentID string
	PayURL            string
	QRPayload         string
}

// Event — статус платежа из вебхука или сверки. Amount — в тыйынах.
type Event struct {
	ProviderPaymentID string
	Status            string // pending | paid | failed
	Amount            int64
}

// Provider — платёжный провайдер. Name — он же ledger.method для его оплат.
type Provider interface {
	Name() string
	// CreatePayment создаёт платёж на amount; ref — наш id ссылки оплаты.
	CreatePayment(ctx context.Context, c Center, amount int64, ref string) (Payment, error)
	// ParseWebhook проверяет подпись (ErrBadSignature) и разбирает уведомление.
	ParseWebhook(r *http.Request) (Event, error)
	// CheckPayment — статус платежа у провайдера (сверка, если вебхук потерялся).
	CheckPayment(ctx context.Context, c Center, providerPaymentID string) (Event, error)
}

// Manual — оплата по реквизитам: внешнего платежа нет, подтверждает администратор.
type Manual struct{}

func (Manual) Name() string { return ProviderManual }

func (Manual) CreatePayment(context.Context, Center, int64, string) (Payment, error) {
	return Payment{}, nil
}

func (Manual) ParseWebhook(*http.Request) (Event, error) { return Event{}, ErrNotImplemented }

func (Manual) CheckPayment(context.Context, Center, string) (Event, error) {
	return Event{}, ErrNotImplemented
}
