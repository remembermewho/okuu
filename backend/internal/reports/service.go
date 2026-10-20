// Package reports — отчёт владельцу за месяц и выгрузка в Excel (F7, экран S7).
// Месяц — календарный, в часовом поясе центра. Деньги — int64 в тыйынах.
package reports

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
)

// DebtEndNote — как считается долг на конец месяца (показывается в отчёте).
const DebtEndNote = "Сумма отрицательных балансов учеников по операциям, созданным до конца месяца. " +
	"Отменённые операции не учитываются — даже если их отменили уже после конца месяца."

// Collected — оплаты месяца по способам.
type Collected struct {
	Total    int64
	Cash     int64
	Transfer int64
	Card     int64
	Finik    int64
}

// Report — итоги месяца.
type Report struct {
	Month             string // ГГГГ-ММ
	Period            civil.Date
	PeriodEnd         civil.Date
	Charged           int64 // начисления за период + абонементы, созданные в месяце
	Collected         Collected
	Refunds           int64 // возвраты (положительное число)
	CollectedNet      int64 // оплаты − возвраты
	Corrections       int64 // поправки со знаком: < 0 — добавили долг
	DebtEnd           int64 // долг на конец месяца (положительное число), см. DebtEndNote
	DebtorsEnd        int
	StudentsActiveEnd int64 // с активной записью в последний день месяца
	Joined            int64 // первая запись ученика началась в этом месяце
	Left              int64 // перешли в «ушёл» в этом месяце
}

// Service — отчёты.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	now  func() time.Time
}

// NewService создаёт сервис; now — часы (nil — time.Now).
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), now: now}
}

// CurrentMonth — 1-е число текущего месяца в поясе центра (отчёт по умолчанию).
func (s *Service) CurrentMonth(ctx context.Context, centerID uuid.UUID) (civil.Date, error) {
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return civil.Date{}, err
	}
	today, err := civil.Today(s.now(), c.Timezone)
	return today.FirstOfMonth(), err
}

type bounds struct {
	period, periodEnd civil.Date
	from, to          time.Time
	tz                string
}

func (s *Service) bounds(ctx context.Context, centerID uuid.UUID, month civil.Date) (bounds, error) {
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return bounds{}, err
	}
	b := bounds{period: month.FirstOfMonth(), tz: c.Timezone}
	b.periodEnd = b.period.AddMonths(1).AddDays(-1)
	if b.from, err = civil.StartOfDay(b.period, c.Timezone); err != nil {
		return bounds{}, err
	}
	b.to, err = civil.StartOfDay(b.period.AddMonths(1), c.Timezone)
	return b, err
}

// Month — отчёт за месяц month (любая дата месяца).
func (s *Service) Month(ctx context.Context, centerID uuid.UUID, month civil.Date) (Report, error) {
	b, err := s.bounds(ctx, centerID, month)
	if err != nil {
		return Report{}, err
	}
	m, err := s.q.ReportMoney(ctx, sqlc.ReportMoneyParams{CenterID: centerID, Period: b.period, FromAt: b.from, ToAt: b.to})
	if err != nil {
		return Report{}, err
	}
	debtors, err := s.q.ReportDebtorsAt(ctx, sqlc.ReportDebtorsAtParams{CenterID: centerID, ToAt: b.to})
	if err != nil {
		return Report{}, err
	}
	st, err := s.q.ReportStudents(ctx, sqlc.ReportStudentsParams{CenterID: centerID, Period: b.period, PeriodEnd: b.periodEnd, FromAt: b.from, ToAt: b.to})
	if err != nil {
		return Report{}, err
	}
	r := Report{
		Month: fmt.Sprintf("%04d-%02d", b.period.Year(), b.period.Month()), Period: b.period, PeriodEnd: b.periodEnd,
		Charged:   m.Charged,
		Collected: Collected{Cash: m.PaidCash, Transfer: m.PaidTransfer, Card: m.PaidCard, Finik: m.PaidFinik},
		Refunds:   m.Refunds, Corrections: m.Corrections, DebtorsEnd: len(debtors),
		StudentsActiveEnd: st.ActiveEnd, Joined: st.Joined, Left: st.LeftCount,
	}
	r.Collected.Total = m.PaidCash + m.PaidTransfer + m.PaidCard + m.PaidFinik
	r.CollectedNet = r.Collected.Total - r.Refunds
	for _, d := range debtors {
		r.DebtEnd += -d.Balance
	}
	return r, nil
}

var kindLabels = map[string]string{"charge": "Начисление", "payment": "Оплата", "refund": "Возврат", "correction": "Поправка"}
var methodLabels = map[string]string{"cash": "Наличные", "transfer": "Перевод", "card": "Карта", "finik": "Finik"}

// ExportXLSX — выгрузка месяца: «Итоги», «Операции» (все операции месяца, отменённые — с пометкой),
// «Должники на конец месяца». Суммы — в сомах.
func (s *Service) ExportXLSX(ctx context.Context, centerID uuid.UUID, month civil.Date) ([]byte, error) {
	r, err := s.Month(ctx, centerID, month)
	if err != nil {
		return nil, err
	}
	b, err := s.bounds(ctx, centerID, month)
	if err != nil {
		return nil, err
	}
	ops, err := s.q.ReportOperations(ctx, sqlc.ReportOperationsParams{CenterID: centerID, FromAt: b.from, ToAt: b.to})
	if err != nil {
		return nil, err
	}
	debtors, err := s.q.ReportDebtorsAt(ctx, sqlc.ReportDebtorsAtParams{CenterID: centerID, ToAt: b.to})
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(b.tz)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	defer f.Close()
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	money, _ := f.NewStyle(&excelize.Style{CustomNumFmt: ptr("#,##0.00")})

	// Итоги.
	const sum = "Итоги"
	if err := f.SetSheetName("Sheet1", sum); err != nil {
		return nil, err
	}
	summary := []struct {
		label string
		value any
		money bool
	}{
		{"Месяц", r.Month, false},
		{"Начислено, сом", som(r.Charged), true},
		{"Собрано всего, сом", som(r.Collected.Total), true},
		{"  наличные", som(r.Collected.Cash), true},
		{"  перевод", som(r.Collected.Transfer), true},
		{"  карта", som(r.Collected.Card), true},
		{"  Finik", som(r.Collected.Finik), true},
		{"Возвраты, сом", som(r.Refunds), true},
		{"Собрано за вычетом возвратов, сом", som(r.CollectedNet), true},
		{"Поправки (± к балансу), сом", som(r.Corrections), true},
		{"Долг на конец месяца, сом", som(r.DebtEnd), true},
		{"Должников на конец месяца", r.DebtorsEnd, false},
		{"Активных учеников на конец месяца", r.StudentsActiveEnd, false},
		{"Пришло", r.Joined, false},
		{"Ушло", r.Left, false},
		{"Как считается долг", DebtEndNote, false},
	}
	for i, row := range summary {
		_ = f.SetCellValue(sum, fmt.Sprintf("A%d", i+1), row.label)
		_ = f.SetCellValue(sum, fmt.Sprintf("B%d", i+1), row.value)
		if row.money {
			_ = f.SetCellStyle(sum, fmt.Sprintf("B%d", i+1), fmt.Sprintf("B%d", i+1), money)
		}
	}
	_ = f.SetColWidth(sum, "A", "A", 38)
	_ = f.SetColWidth(sum, "B", "B", 20)

	// Операции.
	const opsSheet = "Операции"
	if _, err := f.NewSheet(opsSheet); err != nil {
		return nil, err
	}
	header := []string{"Дата и время", "Ученик", "Вид", "Сумма, сом", "Способ", "Комментарий", "Период", "Срок оплаты", "Внёс", "Отменена", "Причина отмены"}
	_ = f.SetSheetRow(opsSheet, "A1", &header)
	_ = f.SetRowStyle(opsSheet, 1, 1, bold)
	for i, op := range ops {
		row := []any{
			op.CreatedAt.In(loc).Format("02.01.2006 15:04"), op.StudentName, kindLabels[op.Kind], som(op.Amount),
			label(methodLabels, op.Method), deref(op.Comment), dateStr(op.Period), dateStr(op.DueDate),
			deref(op.CreatedByName), yesNo(op.CancelledAt != nil), deref(op.CancelReason),
		}
		if op.CreatedByName == nil {
			row[8] = "Система"
		}
		cell := fmt.Sprintf("A%d", i+2)
		_ = f.SetSheetRow(opsSheet, cell, &row)
		_ = f.SetCellStyle(opsSheet, fmt.Sprintf("D%d", i+2), fmt.Sprintf("D%d", i+2), money)
	}
	_ = f.SetColWidth(opsSheet, "A", "K", 18)

	// Должники на конец месяца.
	const debtSheet = "Должники на конец месяца"
	if _, err := f.NewSheet(debtSheet); err != nil {
		return nil, err
	}
	dh := []string{"Ученик", "Статус", "Родитель", "Телефон", "Долг, сом"}
	_ = f.SetSheetRow(debtSheet, "A1", &dh)
	_ = f.SetRowStyle(debtSheet, 1, 1, bold)
	statusLabels := map[string]string{"active": "учится", "paused": "пауза", "left": "ушёл"}
	for i, d := range debtors {
		row := []any{d.FullName, statusLabels[d.Status], deref(d.ParentName), deref(d.ParentPhone), som(-d.Balance)}
		_ = f.SetSheetRow(debtSheet, fmt.Sprintf("A%d", i+2), &row)
		_ = f.SetCellStyle(debtSheet, fmt.Sprintf("E%d", i+2), fmt.Sprintf("E%d", i+2), money)
	}
	_ = f.SetColWidth(debtSheet, "A", "E", 24)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// som — тыйыны в сомы для ячейки Excel. Только для показа: все расчёты — в int64 тыйынах.
func som(tyiyn int64) float64 { return float64(tyiyn) / 100 }

func ptr[T any](v T) *T { return &v }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func label(m map[string]string, v *string) string {
	if v == nil {
		return ""
	}
	if l, ok := m[*v]; ok {
		return l
	}
	return *v
}

func dateStr(d *civil.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func yesNo(b bool) string {
	if b {
		return "да"
	}
	return ""
}
