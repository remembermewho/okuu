package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/billing"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/token"
)

// LinkTTL — срок ссылки оплаты.
const LinkTTL = 24 * time.Hour

var (
	// ErrNotFound — ссылки нет или она истекла: снаружи одинаково 404.
	ErrNotFound = errors.New("ссылка оплаты не найдена")
	// ErrAlreadyPaid — по ссылке уже заплатили.
	ErrAlreadyPaid = errors.New("ссылка уже оплачена")
	// ErrNoDebt — у ученика нет долга: платить нечего.
	ErrNoDebt = errors.New("долга нет")
	// ErrStudentNotFound — ученика (или его родителя) нет в центре.
	ErrStudentNotFound = errors.New("ученик не найден")
	// ErrProviderUnavailable — провайдер не создал платёж (или выключен).
	ErrProviderUnavailable = errors.New("платёжный провайдер недоступен")
	// ErrBadEvent — уведомление провайдера без нужных данных.
	ErrBadEvent = errors.New("некорректное уведомление провайдера")
)

// Options — зависимости сервиса.
type Options struct {
	Pool      *pgxpool.Pool
	Online    Provider // онлайн-провайдер (Finik); nil — только реквизиты (manual)
	AppOrigin string   // адрес фронтенда: ссылка оплаты — AppOrigin/pay/<токен>
	Now       func() time.Time
	Logger    *slog.Logger
}

// Service — ссылки оплаты и приём оплат от провайдера.
type Service struct {
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	online Provider
	origin string
	now    func() time.Time
	logger *slog.Logger
}

// NewService создаёт сервис.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: sqlc.New(o.Pool), online: o.Online, origin: strings.TrimSuffix(o.AppOrigin, "/"), now: o.Now, logger: o.Logger}
	if s.now == nil {
		s.now = time.Now
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	return s
}

// Online — онлайн-провайдер или nil (для вебхука).
func (s *Service) Online() Provider { return s.online }

// Link — созданная ссылка оплаты; URL с токеном отдаётся родителю один раз.
type Link struct {
	ID        uuid.UUID
	URL       string
	Amount    int64
	ExpiresAt time.Time
}

// CreateLink — ссылка оплаты ребёнка на сумму его текущего долга, на 24 часа. Провайдер —
// онлайн, если он включён и центр подключил Finik, иначе реквизиты центра.
func (s *Service) CreateLink(ctx context.Context, centerID, studentID uuid.UUID) (Link, error) {
	st, err := s.q.GetStudentForPayLink(ctx, sqlc.GetStudentForPayLinkParams{CenterID: centerID, ID: studentID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && st.ParentID == nil) {
		return Link{}, ErrStudentNotFound
	}
	if err != nil {
		return Link{}, err
	}
	if st.Balance >= 0 {
		return Link{}, ErrNoDebt
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return Link{}, err
	}
	provider := ProviderManual
	if s.online != nil && len(c.FinikCreds) > 0 {
		provider = s.online.Name()
	}
	raw, err := token.New()
	if err != nil {
		return Link{}, err
	}
	row, err := s.q.CreatePaymentLink(ctx, sqlc.CreatePaymentLinkParams{
		Token: token.HashHex(raw), CenterID: centerID, ParentID: *st.ParentID, StudentID: studentID,
		Amount: -st.Balance, Provider: provider, ExpiresAt: s.now().Add(LinkTTL),
	})
	if err != nil {
		return Link{}, fmt.Errorf("ссылка оплаты: %w", err)
	}
	return Link{ID: row.ID, URL: s.origin + "/pay/" + raw, Amount: row.Amount, ExpiresAt: row.ExpiresAt}, nil
}

// PublicLink — то, что видит родитель на странице оплаты: центр, ученик (ФИО как записано
// в центре), сумма. Без телефонов, имени родителя и id.
type PublicLink struct {
	CenterName  string
	StudentName string
	Amount      int64
	Provider    string
	PayDetails  *string // реквизиты — только для manual
	Status      string  // pending | paid
	ExpiresAt   time.Time
}

// Get — публичная страница ссылки. Неизвестная и истёкшая — одинаково ErrNotFound.
func (s *Service) Get(ctx context.Context, raw string) (PublicLink, error) {
	l, err := s.lookup(ctx, raw)
	if err != nil {
		return PublicLink{}, err
	}
	p := PublicLink{CenterName: l.CenterName, StudentName: l.FullName, Amount: l.Amount,
		Provider: l.Provider, Status: l.Status, ExpiresAt: l.ExpiresAt}
	if l.Provider == ProviderManual {
		p.PayDetails = l.PayDetails
	}
	return p, nil
}

func (s *Service) lookup(ctx context.Context, raw string) (sqlc.GetPayLinkRow, error) {
	if !validToken(raw) {
		return sqlc.GetPayLinkRow{}, ErrNotFound
	}
	l, err := s.q.GetPayLink(ctx, token.HashHex(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	if err != nil {
		return l, err
	}
	if l.Status == "expired" || (l.Status == "pending" && !s.now().Before(l.ExpiresAt)) {
		return l, ErrNotFound
	}
	return l, nil
}

// validToken — токен ссылки: 43 символа base64url (32 байта).
func validToken(s string) bool {
	if len(s) != 43 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// Started — что делать родителю: перейти по ссылке банка / показать QR (онлайн) или
// перевести по реквизитам (manual).
type Started struct {
	Provider   string
	Amount     int64
	PayURL     *string
	QRPayload  *string
	PayDetails *string
}

// Start — «Оплатить» на странице: у онлайн-провайдера создаётся платёж (повторное нажатие
// возвращает тот же); для manual — реквизиты центра.
func (s *Service) Start(ctx context.Context, raw string) (Started, error) {
	l, err := s.lookup(ctx, raw)
	if err != nil {
		return Started{}, err
	}
	if l.Status == "paid" {
		return Started{}, ErrAlreadyPaid
	}
	if l.Provider == ProviderManual {
		return Started{Provider: ProviderManual, Amount: l.Amount, PayDetails: l.PayDetails}, nil
	}
	if s.online == nil || s.online.Name() != l.Provider {
		return Started{}, ErrProviderUnavailable
	}

	out := Started{Provider: l.Provider, Amount: l.Amount}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		cur, err := q.LockPayLink(ctx, l.ID)
		if err != nil {
			return err
		}
		if cur.Status == "paid" {
			return ErrAlreadyPaid
		}
		if cur.ProviderPaymentID != nil {
			out.PayURL, out.QRPayload = cur.PayUrl, cur.QrPayload
			return nil
		}
		p, err := s.online.CreatePayment(ctx, Center{ID: l.CenterID, Name: l.CenterName}, l.Amount, l.ID.String())
		if err != nil {
			s.logger.Error("оплата: провайдер не создал платёж", "provider", l.Provider, "link_id", l.ID, "err", err)
			return fmt.Errorf("%w: %w", ErrProviderUnavailable, err)
		}
		if p.ProviderPaymentID == "" {
			return fmt.Errorf("%w: пустой id платежа", ErrProviderUnavailable)
		}
		out.PayURL, out.QRPayload = nonEmpty(p.PayURL), nonEmpty(p.QRPayload)
		return q.SetPayLinkPayment(ctx, sqlc.SetPayLinkPaymentParams{
			Provider: l.Provider, ProviderPaymentID: &p.ProviderPaymentID, PayUrl: out.PayURL, QrPayload: out.QRPayload, ID: l.ID,
		})
	})
	return out, err
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Apply — уведомление провайдера (вебхук или сверка). Идемпотентно по id платежа
// (ledger.external_id): повтор ничего не меняет. paid → оплата в ledger (method = провайдер),
// ссылка paid, родителю «оплата получена». Сумма ≠ сумме ссылки — записываем как пришла и
// предупреждаем в журнале и логе.
func (s *Service) Apply(ctx context.Context, ev Event) error {
	if s.online == nil {
		return ErrProviderUnavailable
	}
	if ev.ProviderPaymentID == "" {
		return ErrBadEvent
	}
	if ev.Status != StatusPaid {
		return nil // pending / failed: ссылка остаётся, родитель может попробовать снова
	}
	if ev.Amount <= 0 || ev.Amount > billing.MaxOperationAmount {
		return fmt.Errorf("%w: сумма %d", ErrBadEvent, ev.Amount)
	}
	provider := s.online.Name()
	var mismatch *sqlc.LockPayLinkByProviderPaymentRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		l, err := q.LockPayLinkByProviderPayment(ctx, sqlc.LockPayLinkByProviderPaymentParams{Provider: provider, ProviderPaymentID: &ev.ProviderPaymentID})
		if errors.Is(err, pgx.ErrNoRows) {
			s.logger.Warn("оплата: уведомление о неизвестном платеже", "provider", provider, "provider_payment_id", ev.ProviderPaymentID)
			return nil
		}
		if err != nil {
			return err
		}
		comment := "Онлайн-оплата по ссылке"
		ledgerID, err := q.InsertProviderPayment(ctx, sqlc.InsertProviderPaymentParams{
			CenterID: l.CenterID, StudentID: l.StudentID, Amount: ev.Amount, Method: &provider,
			ExternalID: &ev.ProviderPaymentID, Comment: &comment,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // уже записано: вебхук пришёл повторно
		}
		if err != nil {
			return err
		}
		paidAt := s.now()
		if err := q.MarkPayLinkPaid(ctx, sqlc.MarkPayLinkPaidParams{PaidAt: &paidAt, LedgerID: &ledgerID, ID: l.ID}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: l.CenterID, Action: "ledger." + billing.KindPayment, Entity: "ledger", EntityID: &ledgerID,
			After: map[string]any{"student_id": l.StudentID, "kind": billing.KindPayment, "amount": ev.Amount, "method": provider,
				"external_id": ev.ProviderPaymentID, "payment_link_id": l.ID},
		}); err != nil {
			return err
		}
		if ev.Amount != l.Amount {
			mismatch = &l
			if err := audit.Record(ctx, tx, audit.Entry{
				CenterID: l.CenterID, Action: audit.PaymentAmountMismatch, Entity: "payment_link", EntityID: &l.ID,
				After: map[string]any{"link_amount": l.Amount, "paid_amount": ev.Amount, "external_id": ev.ProviderPaymentID},
			}); err != nil {
				return err
			}
		}
		return notify.Enqueue(ctx, tx, notify.PaidEvent(l.CenterID, l.StudentID, ledgerID))
	})
	if err == nil && mismatch != nil {
		s.logger.Warn("оплата: сумма отличается от ссылки", "link_id", mismatch.ID, "link_amount", mismatch.Amount, "paid_amount", ev.Amount)
	}
	return err
}

// HandleWebhook — вебхук онлайн-провайдера: подпись проверяет провайдер.
func (s *Service) HandleWebhook(ctx context.Context, ev Event) error { return s.Apply(ctx, ev) }

// ReconcileResult — итог payments.reconcile.
type ReconcileResult struct {
	Checked, Paid int
	Expired       int64
}

// Reconcile — payments.reconcile: начатые у провайдера платежи без вебхука перепроверяются,
// истёкшие ссылки становятся expired. Сначала проверка: оплата в последнюю минуту не потеряется.
func (s *Service) Reconcile(ctx context.Context) (ReconcileResult, error) {
	var res ReconcileResult
	if s.online != nil {
		rows, err := s.q.ListPendingProviderLinks(ctx, s.online.Name())
		if err != nil {
			return res, err
		}
		for _, l := range rows {
			ev, err := s.online.CheckPayment(ctx, Center{ID: l.CenterID, Name: l.CenterName}, l.ProviderPaymentID)
			if errors.Is(err, ErrNotImplemented) {
				break // TODO(finik): у заглушки проверки нет
			}
			res.Checked++
			if err != nil {
				s.logger.Warn("payments.reconcile: статус платежа", "link_id", l.ID, "err", err)
				continue
			}
			if ev.Status != StatusPaid {
				continue
			}
			ev.ProviderPaymentID = l.ProviderPaymentID
			if err := s.Apply(ctx, ev); err != nil {
				s.logger.Error("payments.reconcile: оплата не записана", "link_id", l.ID, "err", err)
				continue
			}
			res.Paid++
		}
	}
	n, err := s.q.ExpirePayLinks(ctx, s.now())
	res.Expired = n
	return res, err
}
