// Package billing — начисления, баланс и долги (R3–R16). Деньги — int64 в тыйынах.
// Баланс нигде не хранится: это сумма неотменённых операций ledger (R13).
package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"okuu/internal/audit"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/validate"
)

// Виды скидки ученика.
const (
	DiscountFixed   = "fixed"   // value — тыйыны
	DiscountPercent = "percent" // value — проценты 0–100
)

// ErrDuplicateCharge — начисление за этот период по этой записи уже есть (уникальный индекс, R3).
var ErrDuplicateCharge = errors.New("начисление за период уже есть")

// Discount — скидка ученика; nil — без скидки.
type Discount struct {
	Kind  string `json:"kind"`
	Value int64  `json:"value"`
}

// ValidateDiscount проверяет скидку ученика: fixed — 0…maxFixed тыйын, percent — 0…100.
func ValidateDiscount(d *Discount, maxFixed int64) error {
	if d == nil {
		return nil
	}
	switch d.Kind {
	case DiscountFixed:
		if d.Value < 0 || d.Value > maxFixed {
			return validate.Errorf("discount", "Скидка в тыйынах — от 0 до %d", maxFixed)
		}
	case DiscountPercent:
		if d.Value < 0 || d.Value > 100 {
			return validate.Errorf("discount", "Скидка в процентах — от 0 до 100")
		}
	default:
		return validate.Errorf("discount", "Вид скидки — fixed или percent")
	}
	return nil
}

// DiscountAmount считает скидку от цены (R6): fixed — как есть; percent — floor(price * percent / 100).
// Фиксированная скидка больше цены урезается до цены (начисление на 0), capped = true —
// вызывающий передаёт это в ChargeInput.DiscountCapped, и в журнале остаётся пометка.
func DiscountAmount(price int64, d *Discount) (discount int64, capped bool, err error) {
	if d == nil {
		return 0, false, nil
	}
	switch d.Kind {
	case DiscountFixed:
		if d.Value > price {
			return price, true, nil
		}
		return d.Value, false, nil
	case DiscountPercent:
		// price ≤ 1e9, percent ≤ 100 — переполнения нет; оба неотрицательны, деление = floor.
		return price * d.Value / 100, false, nil
	}
	return 0, false, fmt.Errorf("неизвестный вид скидки %q", d.Kind)
}

// ChargeInput — начисление: amount = -(ListPrice - Discount).
type ChargeInput struct {
	CenterID     uuid.UUID
	StudentID    uuid.UUID
	EnrollmentID *uuid.UUID
	ListPrice    int64
	Discount     int64
	Period       *civil.Date // 1-е число месяца для monthly; nil для абонемента
	DueDate      civil.Date
	Comment      *string
	CreatedBy    *uuid.UUID // nil — система (billing.monthly)
	// DiscountCapped — фиксированная скидка ученика была больше цены и урезана до неё (начисление на 0).
	DiscountCapped bool
}

// Charge — созданное начисление.
type Charge struct {
	ID        uuid.UUID   `json:"id"`
	Amount    int64       `json:"amount"`
	ListPrice int64       `json:"list_price"`
	Discount  int64       `json:"discount"`
	Period    *civil.Date `json:"period"`
	DueDate   civil.Date  `json:"due_date"`
}

// CreateCharge пишет начисление в ledger и журнал (ledger.charge) через tx вызывающего.
// Повтор за тот же период по той же записи — ErrDuplicateCharge (транзакцию нужно откатить).
func CreateCharge(ctx context.Context, tx sqlc.DBTX, in ChargeInput) (Charge, error) {
	if in.ListPrice < 0 || in.Discount < 0 || in.Discount > in.ListPrice {
		return Charge{}, fmt.Errorf("начисление: list_price=%d discount=%d", in.ListPrice, in.Discount)
	}
	if in.DueDate.IsZero() {
		return Charge{}, errors.New("начисление: не задан срок оплаты")
	}
	if in.Period != nil && in.Period.Day() != 1 {
		return Charge{}, fmt.Errorf("начисление: period %s — не 1-е число", in.Period)
	}

	amount := -(in.ListPrice - in.Discount)
	row, err := sqlc.New(tx).InsertCharge(ctx, sqlc.InsertChargeParams{
		CenterID:     in.CenterID,
		StudentID:    in.StudentID,
		EnrollmentID: in.EnrollmentID,
		Amount:       amount,
		Period:       in.Period,
		DueDate:      &in.DueDate,
		ListPrice:    &in.ListPrice,
		Discount:     &in.Discount,
		Comment:      in.Comment,
		CreatedBy:    in.CreatedBy,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Charge{}, ErrDuplicateCharge
	}
	if err != nil {
		return Charge{}, fmt.Errorf("начисление: %w", err)
	}

	c := Charge{ID: row.ID, Amount: amount, ListPrice: in.ListPrice, Discount: in.Discount, Period: in.Period, DueDate: in.DueDate}
	after := map[string]any{
		"student_id": in.StudentID, "enrollment_id": in.EnrollmentID, "amount": c.Amount,
		"list_price": c.ListPrice, "discount": c.Discount, "period": c.Period, "due_date": c.DueDate,
		"comment": in.Comment,
	}
	if in.DiscountCapped {
		after["discount_capped"] = true
	}
	if err := audit.Record(ctx, tx, audit.Entry{
		CenterID: in.CenterID, UserID: in.CreatedBy, Action: audit.LedgerCharge, Entity: "ledger", EntityID: &c.ID,
		After: after,
	}); err != nil {
		return Charge{}, err
	}
	return c, nil
}

// Balance — баланс ученика: сумма неотменённых операций (R13). Отрицательный — долг.
func Balance(ctx context.Context, db sqlc.DBTX, centerID, studentID uuid.UUID) (int64, error) {
	return sqlc.New(db).StudentBalance(ctx, sqlc.StudentBalanceParams{CenterID: centerID, StudentID: studentID})
}
