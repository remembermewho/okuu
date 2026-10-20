package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
)

// EffectiveBillingDay — день начисления, действующий для периода period. Смена billing_day в настройках
// действует со следующего периода: до next_from работает старый день, с next_from — новый.
func EffectiveBillingDay(day int16, next *int16, nextFrom *civil.Date, period civil.Date) int {
	if next != nil && nextFrom != nil && !period.Before(*nextFrom) {
		return int(*next)
	}
	return int(day)
}

// MonthlyResult — итог billing.monthly по центру за период.
type MonthlyResult struct {
	Period  civil.Date
	Charged int // создано начислений
	Paused  int // пауза на дату начисления — без начисления (R7), billed_through поставлен
	Skipped int // решение за период уже принято (параллельный запуск, начисление уже было, ученик ушёл)
	Failed  int // ошибки по отдельным записям (в логе); повторятся при следующем запуске
}

// RunMonthly — ежемесячные начисления центра (R3, R4, R6, R7) на дату today (в поясе центра).
// До billing_day ничего не делает. Период P — 1-е число месяца today; прошлые месяцы не догоняет.
// Берёт месячные записи, активные в P, у которых billed_through IS NULL или < P:
// ученик active — начисление по текущей цене тарифа и скидке, срок P + (billing_day − 1) + due_days;
// paused — без начисления. В обоих случаях в той же транзакции billed_through = P.
// Каждая запись — в своей транзакции; ошибка одной не останавливает остальные.
// Повторный и параллельный запуск безопасны: запись блокируется и перепроверяется.
func RunMonthly(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, centerID uuid.UUID, today civil.Date) (MonthlyResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	q := sqlc.New(pool)
	center, err := q.GetCenter(ctx, centerID)
	if err != nil {
		return MonthlyResult{}, fmt.Errorf("центр %s: %w", centerID, err)
	}
	period := today.FirstOfMonth()
	res := MonthlyResult{Period: period}
	billingDay := EffectiveBillingDay(center.BillingDay, center.BillingDayNext, center.BillingDayNextFrom, period)
	if today.Day() < billingDay {
		return res, nil
	}
	due := period.AddDays(billingDay - 1 + int(center.DueDays))

	ids, err := q.ListMonthlyBillingCandidates(ctx, sqlc.ListMonthlyBillingCandidatesParams{
		CenterID: centerID, Period: period, PeriodEnd: period.AddMonths(1).AddDays(-1),
	})
	if err != nil {
		return res, err
	}
	for _, id := range ids {
		outcome, err := billOne(ctx, pool, centerID, id, period, due)
		if err != nil {
			res.Failed++
			logger.Error("billing.monthly: запись не обработана", "center_id", centerID, "enrollment_id", id, "period", period, "err", err)
			continue
		}
		switch outcome {
		case outcomeCharged:
			res.Charged++
		case outcomePaused:
			res.Paused++
		default:
			res.Skipped++
		}
	}
	return res, nil
}

type billOutcome int

const (
	outcomeSkipped billOutcome = iota
	outcomeCharged
	outcomePaused
)

func billOne(ctx context.Context, pool *pgxpool.Pool, centerID, enrollmentID uuid.UUID, period, due civil.Date) (billOutcome, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)

	e, err := q.LockEnrollmentForBilling(ctx, sqlc.LockEnrollmentForBillingParams{CenterID: centerID, ID: enrollmentID, Period: period})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcomeSkipped, nil // решение за P уже принято (параллельный запуск)
	}
	if err != nil {
		return 0, err
	}
	setBilled := func() error {
		return q.SetBilledThrough(ctx, sqlc.SetBilledThroughParams{CenterID: centerID, ID: enrollmentID, Period: period})
	}

	outcome := outcomeSkipped
	switch e.Status {
	case "left":
		return outcomeSkipped, nil // R7: ушедшим больше не начисляем
	case "paused":
		outcome = outcomePaused // R7: пауза на дату начисления — без начисления
	default:
		has, err := q.HasChargeForPeriod(ctx, sqlc.HasChargeForPeriodParams{EnrollmentID: &enrollmentID, Period: period})
		if err != nil {
			return 0, err
		}
		if has {
			break // начисление за P уже есть — только отмечаем решение
		}
		tariff, err := q.LockTariff(ctx, sqlc.LockTariffParams{CenterID: centerID, ID: e.TariffID})
		if err != nil {
			return 0, err
		}
		// Фиксированная скидка больше цены — начисление на 0 с пометкой discount_capped в журнале.
		disc, capped, err := DiscountAmount(tariff.Price, discountOf(e.DiscountKind, e.DiscountValue))
		if err != nil {
			return 0, err
		}
		if _, err := CreateCharge(ctx, tx, ChargeInput{
			CenterID: centerID, StudentID: e.StudentID, EnrollmentID: &enrollmentID,
			ListPrice: tariff.Price, Discount: disc, Period: &period, DueDate: due, DiscountCapped: capped,
		}); err != nil {
			return 0, err
		}
		outcome = outcomeCharged
	}
	if err := setBilled(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return outcome, nil
}

func discountOf(kind *string, value *int64) *Discount {
	if kind == nil || value == nil {
		return nil
	}
	return &Discount{Kind: *kind, Value: *value}
}
