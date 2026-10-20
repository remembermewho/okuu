// Package finik — провайдер оплаты Finik. ЗАГЛУШКА: включается FINIK_ENABLED=true, но все
// методы возвращают payments.ErrNotImplemented.
//
// TODO(finik): API Finik ещё не известен (открытый вопрос в ТЗ: ссылка на оплату, вебхук,
// подпись, работа с ключами каждого центра). Когда будет документация:
//   - CreatePayment: POST в Finik с ключами мерчанта центра (centers.finik_creds, AES-GCM
//     ключом ENCRYPTION_KEY) → id платежа + ссылка/QR;
//   - ParseWebhook: проверка подписи (HMAC или ключ Finik) → payments.ErrBadSignature;
//   - CheckPayment: запрос статуса для payments.reconcile.
//
// Логика вокруг провайдера (ссылки, идемпотентность по external_id, ledger, уведомление paid,
// сверка) уже готова и проверена тестами на фейковом провайдере.
package finik

import (
	"context"
	"net/http"

	"okuu/internal/payments"
)

// Name — ledger.method оплат Finik.
const Name = "finik"

// Provider — Finik (заглушка).
type Provider struct {
	apiKey string // TODO(finik): ключ платформы, если он нужен помимо ключей центров
}

// New создаёт провайдер; ключ в логи не попадает.
func New(apiKey string) *Provider { return &Provider{apiKey: apiKey} }

func (p *Provider) Name() string { return Name }

func (p *Provider) CreatePayment(context.Context, payments.Center, int64, string) (payments.Payment, error) {
	return payments.Payment{}, payments.ErrNotImplemented // TODO(finik)
}

func (p *Provider) ParseWebhook(*http.Request) (payments.Event, error) {
	return payments.Event{}, payments.ErrNotImplemented // TODO(finik)
}

func (p *Provider) CheckPayment(context.Context, payments.Center, string) (payments.Event, error) {
	return payments.Event{}, payments.ErrNotImplemented // TODO(finik)
}

// String не даёт напечатать ключ.
func (p *Provider) String() string { return "finik.Provider" }
