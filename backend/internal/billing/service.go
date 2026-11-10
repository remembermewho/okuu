package billing

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/notify"
	"okuu/internal/validate"
)

// Способы оплаты. finik — только из вебхука Finik (позже), вручную не вносится.
const (
	MethodCash     = "cash"
	MethodTransfer = "transfer"
	MethodCard     = "card"
	MethodFinik    = "finik"
)

const (
	// MaxOperationAmount — 100 000 000 сом в тыйынах: защита от опечаток в сумме.
	MaxOperationAmount = 10_000_000_000
	MaxCommentLen      = 500
	MaxIdempotencyKey  = 100
	DefaultLimit       = 50
	MaxLimit           = 200
)

var (
	ErrStudentNotFound   = errors.New("ученик не найден")
	ErrOperationNotFound = errors.New("операция не найдена")
	// ErrAlreadyCancelled — операция уже отменена (R15).
	ErrAlreadyCancelled = errors.New("операция уже отменена")
	// ErrIdempotencyMismatch — Idempotency-Key уже использован для другой операции.
	ErrIdempotencyMismatch = errors.New("Idempotency-Key использован с другими данными")
	// ErrFinikOwnerOnly — оплату из Finik отменяет только владелец (R15).
	ErrFinikOwnerOnly = errors.New("оплату Finik отменяет только владелец")
)

// Operation — операция ledger для API.
type Operation struct {
	ID           uuid.UUID
	StudentID    uuid.UUID
	Kind         string
	Amount       int64
	Method       *string
	Comment      *string
	EnrollmentID *uuid.UUID
	Period       *civil.Date
	DueDate      *civil.Date
	ListPrice    *int64
	Discount     *int64
	CreatedAt    time.Time
	CreatedBy    *uuid.UUID
	CancelledAt  *time.Time
	CancelledBy  *uuid.UUID
	CancelReason *string
}

// Cancelled — операция отменена и не входит в баланс.
func (o Operation) Cancelled() bool { return o.CancelledAt != nil }

// Service — оплаты, отмены, история, должники.
type Service struct {
	pool   *pgxpool.Pool
	q      *sqlc.Queries
	now    func() time.Time
	logger *slog.Logger
}

// NewService создаёт сервис; now — часы (nil — time.Now).
func NewService(pool *pgxpool.Pool, now func() time.Time, logger *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{pool: pool, q: sqlc.New(pool), now: now, logger: logger}
}

// OperationInput — ручная операция: payment / refund (amount > 0, method обязателен)
// или correction (amount со знаком ≠ 0, comment обязателен).
type OperationInput struct {
	Kind           string
	Amount         int64
	Method         *string
	Comment        *string
	IdempotencyKey string
}

// AddOperation вносит оплату, возврат или поправку (R13, R16). Повтор с тем же
// Idempotency-Key и теми же данными возвращает ту же операцию (created=false);
// с другими данными — ErrIdempotencyMismatch.
func (s *Service) AddOperation(ctx context.Context, actor, centerID, studentID uuid.UUID, in OperationInput) (op Operation, created bool, err error) {
	in, signed, err := validateOperation(in)
	if err != nil {
		return Operation{}, false, err
	}
	if op, ok, err := s.replay(ctx, centerID, studentID, in, signed); ok || err != nil {
		return op, false, err
	}

	var row sqlc.Ledger
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		exists, err := q.StudentExists(ctx, sqlc.StudentExistsParams{CenterID: centerID, ID: studentID})
		if err != nil {
			return err
		}
		if !exists {
			return ErrStudentNotFound
		}
		row, err = q.InsertOperation(ctx, sqlc.InsertOperationParams{
			CenterID: centerID, StudentID: studentID, Kind: in.Kind, Amount: signed, Method: in.Method,
			Comment: in.Comment, CreatedBy: &actor, IdempotencyKey: &in.IdempotencyKey,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errKeyTaken // параллельный запрос с тем же ключом успел раньше
		}
		if err != nil {
			return err
		}
		// «Спасибо, оплата получена» — родителю, вместе с операцией (не сохранилась — не уйдёт).
		if in.Kind == KindPayment {
			if err := notify.Enqueue(ctx, tx, notify.PaidEvent(centerID, studentID, row.ID)); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: "ledger." + in.Kind, Entity: "ledger", EntityID: &row.ID,
			After: map[string]any{"student_id": studentID, "kind": in.Kind, "amount": signed, "method": in.Method, "comment": in.Comment},
		})
	})
	if errors.Is(err, errKeyTaken) {
		op, ok, err := s.replay(ctx, centerID, studentID, in, signed)
		if !ok && err == nil {
			err = fmt.Errorf("Idempotency-Key %q: операция не найдена после конфликта", in.IdempotencyKey)
		}
		return op, false, err
	}
	if err != nil {
		return Operation{}, false, err
	}
	return fromLedger(row), true, nil
}

var errKeyTaken = errors.New("idempotency key taken")

// CreateCorrectionTx — поправка (correction) в транзакции вызывающего: начальный долг при импорте.
// amount со знаком (долг — отрицательный), comment обязателен; пишет ledger.correction в журнал.
func CreateCorrectionTx(ctx context.Context, tx pgx.Tx, actor, centerID, studentID uuid.UUID, amount int64, comment string) (uuid.UUID, error) {
	if amount == 0 || amount > MaxOperationAmount || amount < -MaxOperationAmount || strings.TrimSpace(comment) == "" {
		return uuid.Nil, fmt.Errorf("поправка: amount=%d comment=%q", amount, comment)
	}
	row, err := sqlc.New(tx).InsertOperation(ctx, sqlc.InsertOperationParams{
		CenterID: centerID, StudentID: studentID, Kind: KindCorrection, Amount: amount, Comment: &comment, CreatedBy: &actor,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("поправка: %w", err)
	}
	if err := audit.Record(ctx, tx, audit.Entry{
		CenterID: centerID, UserID: &actor, Action: "ledger." + KindCorrection, Entity: "ledger", EntityID: &row.ID,
		After: map[string]any{"student_id": studentID, "kind": KindCorrection, "amount": amount, "comment": comment},
	}); err != nil {
		return uuid.Nil, err
	}
	return row.ID, nil
}

// replay ищет операцию по ключу: ok=true — нашлась и совпадает; другие данные — ErrIdempotencyMismatch.
func (s *Service) replay(ctx context.Context, centerID, studentID uuid.UUID, in OperationInput, signed int64) (Operation, bool, error) {
	row, err := s.q.GetOperationByKey(ctx, sqlc.GetOperationByKeyParams{CenterID: centerID, IdempotencyKey: &in.IdempotencyKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, err
	}
	if row.StudentID != studentID || row.Kind != in.Kind || row.Amount != signed ||
		!sameStr(row.Method, in.Method) || !sameStr(row.Comment, in.Comment) {
		return Operation{}, false, ErrIdempotencyMismatch
	}
	return fromLedger(row), true, nil
}

func validateOperation(in OperationInput) (OperationInput, int64, error) {
	key := in.IdempotencyKey
	if n := utf8.RuneCountInString(key); n < 1 || n > MaxIdempotencyKey || strings.TrimSpace(key) != key {
		return in, 0, validate.Errorf("Idempotency-Key", "Заголовок Idempotency-Key обязателен: 1–%d символов без пробелов по краям", MaxIdempotencyKey)
	}
	if in.Comment != nil {
		c := strings.TrimSpace(*in.Comment)
		if c == "" {
			in.Comment = nil
		} else if utf8.RuneCountInString(c) > MaxCommentLen || strings.ContainsRune(c, utf8.RuneError) {
			return in, 0, validate.Errorf("comment", "Комментарий — до %d символов в UTF-8", MaxCommentLen)
		} else {
			in.Comment = &c
		}
	}

	switch in.Kind {
	case KindPayment, KindRefund:
		if in.Amount <= 0 || in.Amount > MaxOperationAmount {
			return in, 0, validate.Errorf("amount", "Сумма — больше 0 и не больше %d тыйын", int64(MaxOperationAmount))
		}
		if in.Method == nil {
			return in, 0, validate.Errorf("method", "Способ оплаты — cash, transfer или card")
		}
		switch *in.Method {
		case MethodCash, MethodTransfer, MethodCard:
		case MethodFinik:
			return in, 0, validate.Errorf("method", "Оплаты Finik приходят только автоматически")
		default:
			return in, 0, validate.Errorf("method", "Способ оплаты — cash, transfer или card")
		}
		if in.Kind == KindRefund {
			return in, -in.Amount, nil // возврат уменьшает баланс (R16)
		}
		return in, in.Amount, nil
	case KindCorrection:
		if in.Amount == 0 || in.Amount > MaxOperationAmount || in.Amount < -MaxOperationAmount {
			return in, 0, validate.Errorf("amount", "Поправка — сумма со знаком, не 0, по модулю до %d тыйын", int64(MaxOperationAmount))
		}
		if in.Method != nil {
			return in, 0, validate.Errorf("method", "У поправки нет способа оплаты")
		}
		if in.Comment == nil {
			return in, 0, validate.Errorf("comment", "У поправки обязательна причина в comment")
		}
		return in, in.Amount, nil
	}
	return in, 0, validate.Errorf("kind", "kind — payment, refund или correction")
}

// Cancel отменяет операцию с причиной (R15). Оплату Finik — только владелец.
// Отмена начисления не сбрасывает billed_through: отменили — значит, решили не начислять.
func (s *Service) Cancel(ctx context.Context, actor uuid.UUID, isOwner bool, centerID, id uuid.UUID, reason string) (Operation, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 || strings.ContainsRune(reason, utf8.RuneError) {
		return Operation{}, validate.Errorf("reason", "Причина отмены — от 3 до 500 символов")
	}
	var row sqlc.Ledger
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		cur, err := q.GetLedgerForUpdate(ctx, sqlc.GetLedgerForUpdateParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOperationNotFound
		}
		if err != nil {
			return err
		}
		if cur.CancelledAt != nil {
			return ErrAlreadyCancelled
		}
		if cur.Method != nil && *cur.Method == MethodFinik && !isOwner {
			return ErrFinikOwnerOnly
		}
		row, err = q.CancelOperation(ctx, sqlc.CancelOperationParams{CenterID: centerID, ID: id, CancelledBy: &actor, CancelReason: &reason})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.LedgerCancel, Entity: "ledger", EntityID: &id,
			Before: map[string]any{"cancelled": false, "kind": cur.Kind, "amount": cur.Amount, "student_id": cur.StudentID},
			After:  map[string]any{"cancelled": true, "reason": reason},
		})
	})
	if err != nil {
		return Operation{}, err
	}
	return fromLedger(row), nil
}

// History — операции ученика, новые сверху, включая отменённые; nextCursor == "" — конец.
func (s *Service) History(ctx context.Context, centerID, studentID uuid.UUID, limit int, cursor string) ([]Operation, string, error) {
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, "", validate.Errorf("limit", "limit — от 1 до %d", MaxLimit)
	}
	p := sqlc.ListStudentLedgerParams{CenterID: centerID, StudentID: studentID, Lim: int32(limit + 1)}
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		p.AfterCreatedAt, p.AfterID = &c.At, &c.ID
	}
	exists, err := s.q.StudentExists(ctx, sqlc.StudentExistsParams{CenterID: centerID, ID: studentID})
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", ErrStudentNotFound
	}
	rows, err := s.q.ListStudentLedger(ctx, p)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = encodeCursor(ledgerCursor{At: last.CreatedAt, ID: last.ID})
	}
	out := make([]Operation, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromLedger(r))
	}
	return out, next, nil
}

// GroupRef — группа ученика в списке должников.
type GroupRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Debtor — должник по R14.
type Debtor struct {
	StudentID      uuid.UUID
	FullName       string
	Status         string // left — «ушёл», долг остаётся (R7)
	Groups         []GroupRef
	ParentName     *string
	ParentPhone    *string
	ParentTgStatus *string
	// ParentLastReminder — когда родителю последний раз ушло напоминание; RemindedToday — сегодня в поясе центра.
	ParentLastReminder *time.Time
	RemindedToday      bool
	// ParentLastWhatsApp — когда открывали WhatsApp-напоминание; WhatsAppOpenedToday — сегодня в поясе центра.
	ParentLastWhatsApp  *time.Time
	WhatsAppOpenedToday bool
	Debt                int64
	DaysOverdue         int        // просрочено: дней с самого старого непогашенного срока
	OldestDueDate       civil.Date // самый ранний непогашенный срок (у ожидающих — ближайший)
	DueInDays           int        // ожидают оплаты: дней до срока (0 — срок сегодня)
}

// Списки должников.
const (
	DebtorsOverdue = "overdue" // R14: долг, срок прошёл
	DebtorsPending = "pending" // отрицательный баланс, срок ещё не наступил (или сегодня)
)

// Summary — итоги текущего месяца по всему центру.
type Summary struct {
	Period       civil.Date
	Charged      int64
	Collected    int64
	DebtTotal    int64
	DebtorsCount int
	// PendingTotal / PendingCount — отрицательный баланс без просрочки: срок ещё не наступил.
	PendingTotal int64
	PendingCount int
}

// Debtors — список и итоги месяца. status = overdue: должники (balance < 0 и срок прошёл), по убыванию
// долга; pending: отрицательный баланс без просрочки, по ближайшему сроку. groupID != nil — только ученики,
// у которых была запись в эту группу; итоги — по всему центру.
func (s *Service) Debtors(ctx context.Context, centerID uuid.UUID, groupID *uuid.UUID, status string) ([]Debtor, Summary, error) {
	if status == "" {
		status = DebtorsOverdue
	}
	if status != DebtorsOverdue && status != DebtorsPending {
		return nil, Summary{}, validate.Errorf("status", "status — overdue или pending")
	}
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return nil, Summary{}, err
	}
	today, err := civil.Today(s.now(), c.Timezone)
	if err != nil {
		return nil, Summary{}, err
	}

	overdue, pending, err := s.debtors(ctx, centerID, nil, today, c.Timezone)
	if err != nil {
		return nil, Summary{}, err
	}
	period := today.FirstOfMonth()
	sum := Summary{Period: period, DebtorsCount: len(overdue), PendingCount: len(pending)}
	for _, d := range overdue {
		sum.DebtTotal += d.Debt
	}
	for _, d := range pending {
		sum.PendingTotal += d.Debt
	}
	m, err := s.q.MonthSummary(ctx, sqlc.MonthSummaryParams{CenterID: centerID, Period: period, NextPeriod: period.AddMonths(1), Tz: c.Timezone})
	if err != nil {
		return nil, Summary{}, err
	}
	sum.Charged, sum.Collected = m.Charged, m.Collected

	if groupID != nil {
		if overdue, pending, err = s.debtors(ctx, centerID, groupID, today, c.Timezone); err != nil {
			return nil, Summary{}, err
		}
	}
	if status == DebtorsPending {
		return pending, sum, nil
	}
	return overdue, sum, nil
}

// Overdue — кто из учеников ids должник по R14 (долг и срок прошёл), как во вкладке «Просрочено»;
// у ожидающих оплаты (срок сегодня или позже) — false. tz — пояс центра, today — сегодня в нём.
func Overdue(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID, tz string, today civil.Date, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListActiveOperations(ctx, sqlc.ListActiveOperationsParams{CenterID: centerID, StudentIds: ids})
	if err != nil {
		return nil, err
	}
	ops := map[uuid.UUID][]Op{}
	for _, r := range rows {
		on, err := civil.DateIn(r.CreatedAt, tz)
		if err != nil {
			return nil, err
		}
		ops[r.StudentID] = append(ops[r.StudentID], Op{ID: r.ID, Kind: r.Kind, Amount: r.Amount, DueDate: r.DueDate, CreatedAt: r.CreatedAt, CreatedOn: on})
	}
	for _, id := range ids {
		out[id] = ComputeDebt(ops[id], today).IsDebtor
	}
	return out, nil
}

// debtors — должники (просрочено) и ожидающие оплаты (отрицательный баланс, срок не наступил).
func (s *Service) debtors(ctx context.Context, centerID uuid.UUID, groupID *uuid.UUID, today civil.Date, tz string) (overdue, pending []Debtor, err error) {
	overdue, pending = []Debtor{}, []Debtor{}
	cands, err := s.q.ListDebtCandidates(ctx, sqlc.ListDebtCandidatesParams{CenterID: centerID, GroupID: groupID})
	if err != nil || len(cands) == 0 {
		return overdue, pending, err
	}
	ids := make([]uuid.UUID, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.ID)
	}
	rows, err := s.q.ListActiveOperations(ctx, sqlc.ListActiveOperationsParams{CenterID: centerID, StudentIds: ids})
	if err != nil {
		return nil, nil, err
	}
	ops := map[uuid.UUID][]Op{}
	for _, r := range rows {
		on, err := civil.DateIn(r.CreatedAt, tz)
		if err != nil {
			return nil, nil, err
		}
		ops[r.StudentID] = append(ops[r.StudentID], Op{ID: r.ID, Kind: r.Kind, Amount: r.Amount, DueDate: r.DueDate, CreatedAt: r.CreatedAt, CreatedOn: on})
	}

	for _, c := range cands {
		d := ComputeDebt(ops[c.ID], today)
		if d.Balance >= 0 || d.OldestDue == nil {
			continue
		}
		var groups []GroupRef
		if err := json.Unmarshal(c.Groups, &groups); err != nil {
			return nil, nil, fmt.Errorf("группы ученика %s: %w", c.ID, err)
		}
		row := Debtor{
			StudentID: c.ID, FullName: c.FullName, Status: c.Status, Groups: groups,
			ParentName: c.ParentName, ParentPhone: c.ParentPhone, ParentTgStatus: c.ParentTgStatus,
			ParentLastReminder: c.ParentLastReminderAt,
			Debt:               d.Debt, DaysOverdue: d.DaysOverdue, OldestDueDate: *d.OldestDue,
		}
		if c.ParentLastReminderAt != nil {
			on, err := civil.DateIn(*c.ParentLastReminderAt, tz)
			if err != nil {
				return nil, nil, err
			}
			row.RemindedToday = on == today
		}
		row.ParentLastWhatsApp = c.ParentLastWhatsappAt
		if c.ParentLastWhatsappAt != nil {
			on, err := civil.DateIn(*c.ParentLastWhatsappAt, tz)
			if err != nil {
				return nil, nil, err
			}
			row.WhatsAppOpenedToday = on == today
		}
		if d.IsDebtor {
			overdue = append(overdue, row)
			continue
		}
		if days := d.OldestDue.DaysSince(today); days > 0 {
			row.DueInDays = days
		}
		pending = append(pending, row)
	}
	byName := func(a, b Debtor) int {
		if c := cmp.Compare(a.FullName, b.FullName); c != 0 {
			return c
		}
		return cmp.Compare(a.StudentID.String(), b.StudentID.String())
	}
	slices.SortFunc(overdue, func(a, b Debtor) int {
		if c := cmp.Compare(b.Debt, a.Debt); c != 0 {
			return c
		}
		return byName(a, b)
	})
	// Ожидающие — по ближайшему сроку, затем по сумме.
	slices.SortFunc(pending, func(a, b Debtor) int {
		if c := a.OldestDueDate.Compare(b.OldestDueDate); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Debt, a.Debt); c != 0 {
			return c
		}
		return byName(a, b)
	})
	return overdue, pending, nil
}

func fromLedger(r sqlc.Ledger) Operation {
	return Operation{
		ID: r.ID, StudentID: r.StudentID, Kind: r.Kind, Amount: r.Amount, Method: r.Method, Comment: r.Comment,
		EnrollmentID: r.EnrollmentID, Period: r.Period, DueDate: r.DueDate, ListPrice: r.ListPrice, Discount: r.Discount,
		CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, CancelledAt: r.CancelledAt, CancelledBy: r.CancelledBy,
		CancelReason: r.CancelReason,
	}
}

func sameStr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

type ledgerCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

func encodeCursor(c ledgerCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (ledgerCursor, error) {
	var c ledgerCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID == uuid.Nil || c.At.IsZero() {
		return ledgerCursor{}, validate.Errorf("cursor", "Некорректный cursor — возьмите next_cursor из прошлого ответа")
	}
	return c, nil
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
