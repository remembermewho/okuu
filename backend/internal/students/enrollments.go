package students

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"okuu/internal/audit"
	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/tariffs"
	"okuu/internal/validate"
)

// Выбор за первый месяц при записи на месячный тариф (R5).
const (
	FirstMonthFull   = "full"   // начислить цену тарифа со скидкой
	FirstMonthManual = "manual" // начислить manual_amount
	FirstMonthNone   = "none"   // не начислять
)

// EnrollInput — запись ученика в группу.
type EnrollInput struct {
	GroupID      uuid.UUID
	TariffID     *uuid.UUID // nil — тариф группы по умолчанию
	StartDate    civil.Date
	FirstMonth   string
	ManualAmount *int64
}

// MaxPastYears — дата начала записи не раньше чем столько лет назад (защита от опечаток).
const MaxPastYears = 5

// Enroll записывает ученика в группу и в той же транзакции создаёт начисление:
//   - monthly, начало не позже конца текущего месяца — текущий месяц по first_month (R5),
//     billed_through = текущий период при любом выборе, в том числе none;
//     срок: начало в текущем месяце — начало + due_days; начало раньше (перенос из Excel) —
//     billing_day + due_days текущего месяца (R4), как у остальных учеников;
//   - monthly, начало в будущем месяце — ничего, billed_through = NULL (начислит billing.monthly, R3);
//   - package — сразу цена со скидкой, lessons_left = lessons_count, срок = сегодня + due_days (R8).
//
// Прошлые месяцы не начисляются никогда: прошлые долги — поправкой (correction) или импортом.
func (s *Service) Enroll(ctx context.Context, actor, centerID, studentID uuid.UUID, in EnrollInput) (Enrollment, *billing.Charge, error) {
	if in.GroupID == uuid.Nil {
		return Enrollment{}, nil, validate.Errorf("group_id", "Группа — обязательное поле")
	}
	if in.StartDate.IsZero() {
		return Enrollment{}, nil, validate.Errorf("start_date", "Дата начала — обязательное поле")
	}
	switch in.FirstMonth {
	case "", FirstMonthFull, FirstMonthNone:
		if in.ManualAmount != nil {
			return Enrollment{}, nil, validate.Errorf("manual_amount", "manual_amount — только при first_month = manual")
		}
	case FirstMonthManual:
		if in.ManualAmount == nil || *in.ManualAmount < 0 || *in.ManualAmount > tariffs.MaxPrice {
			return Enrollment{}, nil, validate.Errorf("manual_amount", "Сумма за первый месяц — от 0 до %d тыйын", tariffs.MaxPrice)
		}
	default:
		return Enrollment{}, nil, validate.Errorf("first_month", "first_month — full, manual или none")
	}

	var (
		enr    Enrollment
		charge *billing.Charge
	)
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		center, err := s.center(ctx, q, centerID)
		if err != nil {
			return err
		}
		today := center.Today
		student, err := lockStudent(ctx, q, centerID, studentID)
		if err != nil {
			return err
		}
		if earliest := today.AddMonths(-12 * MaxPastYears); in.StartDate.Before(earliest) {
			return validate.Unprocessablef("start_date", "Дата начала — не раньше %s (%d лет назад)", earliest, MaxPastYears)
		}

		group, err := q.LockActiveGroup(ctx, sqlc.LockActiveGroupParams{CenterID: centerID, ID: in.GroupID})
		if errors.Is(err, pgx.ErrNoRows) {
			return validate.Unprocessablef("group_id", "Группа не найдена в этом центре или в архиве")
		}
		if err != nil {
			return err
		}
		tariffID := in.TariffID
		if tariffID == nil {
			tariffID = group.DefaultTariffID
		}
		if tariffID == nil {
			return validate.Unprocessablef("tariff_id", "У группы нет тарифа по умолчанию — укажите tariff_id")
		}
		tariff, err := q.LockActiveTariff(ctx, sqlc.LockActiveTariffParams{CenterID: centerID, ID: *tariffID})
		if errors.Is(err, pgx.ErrNoRows) {
			return validate.Unprocessablef("tariff_id", "Тариф не найден в этом центре или в архиве")
		}
		if err != nil {
			return err
		}

		active, err := q.HasActiveEnrollment(ctx, sqlc.HasActiveEnrollmentParams{CenterID: centerID, StudentID: studentID, GroupID: group.ID})
		if err != nil {
			return err
		}
		if active {
			return ErrAlreadyEnrolled
		}

		// Параметры начисления считаем до вставки записи.
		discount := discountOf(student.DiscountKind, student.DiscountValue)
		var chargeIn *billing.ChargeInput
		var (
			lessonsLeft   *int32
			billedThrough *civil.Date
		)
		switch tariff.Kind {
		case tariffs.KindPackage:
			lessonsLeft = tariff.LessonsCount
			disc, capped, err := billing.DiscountAmount(tariff.Price, discount)
			if err != nil {
				return err
			}
			chargeIn = &billing.ChargeInput{ListPrice: tariff.Price, Discount: disc, DueDate: today.AddDays(center.DueDays), DiscountCapped: capped}
		case tariffs.KindMonthly:
			period := today.FirstOfMonth()
			if !in.StartDate.Before(period.AddMonths(1)) {
				break // будущий месяц — начислит billing.monthly
			}
			// Решение за текущий период принято здесь при любом first_month — billing.monthly его не повторит.
			billedThrough = &period
			due := in.StartDate.AddDays(center.DueDays)
			if in.StartDate.Before(period) {
				due = period.AddDays(center.BillingDay - 1 + center.DueDays) // R4 для давно учащихся
			}
			switch in.FirstMonth {
			case "":
				return validate.Errorf("first_month", "Ученик учится в текущем месяце — выберите first_month: full, manual или none")
			case FirstMonthFull:
				disc, capped, err := billing.DiscountAmount(tariff.Price, discount)
				if err != nil {
					return err
				}
				chargeIn = &billing.ChargeInput{ListPrice: tariff.Price, Discount: disc, Period: &period, DueDate: due, DiscountCapped: capped}
			case FirstMonthManual:
				comment := "R5: сумма за первый месяц вручную"
				chargeIn = &billing.ChargeInput{ListPrice: *in.ManualAmount, Period: &period, DueDate: due, Comment: &comment}
			case FirstMonthNone:
			}
		}

		row, err := q.CreateEnrollment(ctx, sqlc.CreateEnrollmentParams{
			CenterID: centerID, StudentID: studentID, GroupID: group.ID, TariffID: tariff.ID,
			StartDate: in.StartDate, LessonsLeft: lessonsLeft, BilledThrough: billedThrough,
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAlreadyEnrolled // параллельная запись в ту же группу (уникальный индекс)
		}
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.EnrollCreate, Entity: "enrollment", EntityID: &row.ID,
			After: map[string]any{
				"student_id": studentID, "group_id": group.ID, "tariff_id": tariff.ID, "start_date": in.StartDate,
				"lessons_left": lessonsLeft, "first_month": in.FirstMonth, "manual_amount": in.ManualAmount,
				"billed_through": billedThrough,
			},
		}); err != nil {
			return err
		}

		if chargeIn != nil {
			chargeIn.CenterID, chargeIn.StudentID, chargeIn.EnrollmentID, chargeIn.CreatedBy = centerID, studentID, &row.ID, &actor
			c, err := billing.CreateCharge(ctx, tx, *chargeIn)
			if err != nil {
				return err
			}
			charge = &c
		}
		enr, err = s.enrollmentView(ctx, q, centerID, studentID, row.ID)
		return err
	})
	return enr, charge, err
}

// EndEnrollment завершает запись датой endDate (nil — сегодня): не раньше начала и не позже
// сегодняшнего дня; запись с будущим началом — только датой начала (отмена до старта).
func (s *Service) EndEnrollment(ctx context.Context, actor, centerID, studentID, id uuid.UUID, endDate *civil.Date) (Enrollment, error) {
	var enr Enrollment
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		today, err := s.centerToday(ctx, q, centerID)
		if err != nil {
			return err
		}
		// Завершить запись можно и у ушедшего ученика — запрет только на новые записи и продления.
		if _, err := lockStudent(ctx, q, centerID, studentID); err != nil && !errors.Is(err, ErrStudentLeft) {
			return err
		}
		cur, err := lockEnrollment(ctx, q, centerID, studentID, id)
		if err != nil {
			return err
		}
		if cur.EndDate != nil {
			return ErrEnrollmentEnded
		}
		// Запись, которая ещё не началась, отменяется датой начала (как и при status=left).
		latest := today
		if cur.StartDate.After(today) {
			latest = cur.StartDate
		}
		end := latest
		if endDate != nil {
			end = *endDate
		}
		if end.Before(cur.StartDate) || end.After(latest) {
			return validate.Errorf("end_date", "Дата завершения — от %s до %s", cur.StartDate, latest)
		}
		if err := q.EndEnrollment(ctx, sqlc.EndEnrollmentParams{CenterID: centerID, ID: id, EndDate: &end}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.EnrollEnd, Entity: "enrollment", EntityID: &id,
			Before: map[string]any{"end_date": nil}, After: map[string]any{"end_date": end},
		}); err != nil {
			return err
		}
		enr, err = s.enrollmentView(ctx, q, centerID, studentID, id)
		return err
	})
	return enr, err
}

// Renew продлевает абонемент: новое начисление по текущей цене тарифа со скидкой ученика
// и lessons_left += lessons_count — отрицательный остаток вычитается из нового (R10).
func (s *Service) Renew(ctx context.Context, actor, centerID, studentID, id uuid.UUID) (Enrollment, billing.Charge, error) {
	var (
		enr    Enrollment
		charge billing.Charge
	)
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		center, err := s.center(ctx, q, centerID)
		if err != nil {
			return err
		}
		today := center.Today
		student, err := lockStudent(ctx, q, centerID, studentID)
		if err != nil {
			return err
		}
		cur, err := lockEnrollment(ctx, q, centerID, studentID, id)
		if err != nil {
			return err
		}
		if cur.EndDate != nil {
			return ErrEnrollmentEnded
		}
		tariff, err := q.LockTariff(ctx, sqlc.LockTariffParams{CenterID: centerID, ID: cur.TariffID})
		if err != nil {
			return err
		}
		if tariff.Kind != tariffs.KindPackage || tariff.LessonsCount == nil {
			return ErrNotPackage
		}
		disc, capped, err := billing.DiscountAmount(tariff.Price, discountOf(student.DiscountKind, student.DiscountValue))
		if err != nil {
			return err
		}

		var before int32
		if cur.LessonsLeft != nil {
			before = *cur.LessonsLeft
		}
		after := before + *tariff.LessonsCount
		if err := q.SetLessonsLeft(ctx, sqlc.SetLessonsLeftParams{CenterID: centerID, ID: id, LessonsLeft: &after}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.EnrollRenew, Entity: "enrollment", EntityID: &id,
			Before: map[string]any{"lessons_left": before}, After: map[string]any{"lessons_left": after},
		}); err != nil {
			return err
		}
		comment := "Продление абонемента"
		charge, err = billing.CreateCharge(ctx, tx, billing.ChargeInput{
			CenterID: centerID, StudentID: studentID, EnrollmentID: &id, ListPrice: tariff.Price, Discount: disc,
			DueDate: today.AddDays(center.DueDays), Comment: &comment, CreatedBy: &actor, DiscountCapped: capped,
		})
		if err != nil {
			return err
		}
		enr, err = s.enrollmentView(ctx, q, centerID, studentID, id)
		return err
	})
	return enr, charge, err
}

// lockStudent блокирует ученика (порядок блокировок: ученик → запись) и отказывает ушедшему для новых действий.
func lockStudent(ctx context.Context, q *sqlc.Queries, centerID, studentID uuid.UUID) (sqlc.Student, error) {
	st, err := q.GetStudentForUpdate(ctx, sqlc.GetStudentForUpdateParams{CenterID: centerID, ID: studentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, err
	}
	if st.Status == StatusLeft {
		return st, ErrStudentLeft
	}
	return st, nil
}

func lockEnrollment(ctx context.Context, q *sqlc.Queries, centerID, studentID, id uuid.UUID) (sqlc.Enrollment, error) {
	e, err := q.GetEnrollmentForUpdate(ctx, sqlc.GetEnrollmentForUpdateParams{CenterID: centerID, StudentID: studentID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Service) enrollmentView(ctx context.Context, q *sqlc.Queries, centerID, studentID, id uuid.UUID) (Enrollment, error) {
	list, err := s.listEnrollments(ctx, q, centerID, studentID)
	if err != nil {
		return Enrollment{}, err
	}
	for _, e := range list {
		if e.ID == id {
			return e, nil
		}
	}
	return Enrollment{}, ErrNotFound
}
