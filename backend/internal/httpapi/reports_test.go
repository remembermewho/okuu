package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"okuu/internal/civil"
	"okuu/internal/reports"
)

type reportResp struct {
	Month     string     `json:"month"`
	Period    civil.Date `json:"period"`
	PeriodEnd civil.Date `json:"period_end"`
	Charged   int64      `json:"charged"`
	Collected struct {
		Total, Cash, Transfer, Card, Finik int64
	} `json:"collected"`
	Refunds           int64  `json:"refunds"`
	CollectedNet      int64  `json:"collected_net"`
	Corrections       int64  `json:"corrections"`
	DebtEnd           int64  `json:"debt_end"`
	DebtEndNote       string `json:"debt_end_note"`
	DebtorsEnd        int    `json:"debtors_end"`
	StudentsActiveEnd int64  `json:"students_active_end"`
	Joined            int64  `json:"joined"`
	Left              int64  `json:"left"`
}

// reportFixture — данные за два прошлых месяца M1 и M2 с операциями на границе месяцев по Бишкеку.
type reportFixture struct {
	c      *catalog
	m1, m2 civil.Date
}

func newReportFixture(t *testing.T) reportFixture {
	t.Helper()
	c := newCatalog(t, "+996708000001")
	ctx := context.Background()
	m1 := c.today.FirstOfMonth().AddMonths(-2)
	m2 := c.today.FirstOfMonth().AddMonths(-1)
	m3 := c.today.FirstOfMonth()
	bishkek, _ := time.LoadLocation("Asia/Bishkek")
	at := func(d civil.Date, h, m int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, bishkek)
	}

	enroll := func(sid uuid.UUID, tariffID uuid.UUID, start civil.Date, end *civil.Date) {
		if _, err := c.api.pool.Exec(ctx, `INSERT INTO enrollments (center_id, student_id, group_id, tariff_id, start_date, end_date)
			VALUES ($1, $2, $3, $4, $5, $6)`, c.center, sid, c.group.ID, tariffID, start, end); err != nil {
			t.Fatal(err)
		}
	}
	op := func(sid uuid.UUID, kind string, amount int64, method string, period *civil.Date, created time.Time, cancelled bool) {
		var m, ext *string
		if method != "" {
			m = &method
		}
		if method == "finik" {
			e := uuid.NewString()
			ext = &e
		}
		var cancelledAt *time.Time
		var reason *string
		if cancelled {
			ca, r := created.Add(time.Hour), "ошибка"
			cancelledAt, reason = &ca, &r
		}
		due := created
		if _, err := c.api.pool.Exec(ctx, `INSERT INTO ledger (center_id, student_id, kind, amount, method, external_id, period, due_date,
			created_at, cancelled_at, cancel_reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11)`,
			c.center, sid, kind, amount, m, ext, period, due.Format("2006-01-02"), created, cancelledAt, reason); err != nil {
			t.Fatal(err)
		}
	}
	s := func(name, phone string) uuid.UUID { return c.student(t, name, phone, nil).ID }
	a, b, cc, d := s("Асанов А", "+996708000101"), s("Бекова Б", "+996708000102"), s("Ваня В", "+996708000103"), s("Долгов Д", "+996708000104")
	s("Без записей", "+996708000105")

	enroll(a, c.monthly.ID, m1.AddDays(4), nil)
	enroll(b, c.monthly.ID, m2.AddDays(2), nil)
	enroll(cc, c.pkg.ID, m2.AddDays(9), nil)
	dEnd := m2.AddDays(15)
	enroll(d, c.monthly.ID, m1, &dEnd)
	if _, err := c.api.pool.Exec(ctx, `UPDATE students SET status = 'left', left_at = $2 WHERE id = $1`, d, at(dEnd, 12, 0)); err != nil {
		t.Fatal(err)
	}

	op(a, "charge", -300_000, "", &m1, at(m1, 10, 0), false)
	op(a, "charge", -300_000, "", &m2, at(m2, 5, 0), false)
	op(a, "payment", 100_000, "cash", nil, at(m2, 0, 0).Add(-30*time.Minute), false) // 23:30 по Бишкеку, последний день M1
	op(a, "payment", 200_000, "cash", nil, at(m2, 0, 30), false)                     // 00:30 по Бишкеку = ещё M1 по UTC
	op(b, "charge", -300_000, "", &m2, at(m2.AddDays(2), 12, 0), false)
	op(b, "payment", 50_000, "transfer", nil, at(m2.AddDays(3), 12, 0), false)
	op(b, "refund", -10_000, "cash", nil, at(m2.AddDays(4), 12, 0), false)
	op(b, "payment", 20_000, "card", nil, at(m2.AddDays(5), 12, 0), true) // отменена
	op(cc, "charge", -400_000, "", nil, at(m2.AddDays(9), 12, 0), false)  // абонемент
	op(cc, "payment", 30_000, "finik", nil, at(m2.AddDays(10), 12, 0), false)
	op(cc, "correction", -15_000, "", nil, at(m2.AddDays(11), 12, 0), false)
	op(d, "charge", -300_000, "", &m1, at(m1, 10, 0), false)
	op(d, "charge", -300_000, "", &m2, at(m2.AddDays(1), 10, 0), false)
	op(d, "payment", 300_000, "cash", nil, at(m3, 0, 1), false) // уже в текущем месяце — в долг на конец M2 не входит
	return reportFixture{c: c, m1: m1, m2: m2}
}

func (f reportFixture) report(t *testing.T, as *client, m civil.Date) *reportResp {
	t.Helper()
	rec := as.do(http.MethodGet, centerPath(f.c.center, "/reports/month?m="+m.String()[:7]), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("report %s: %d %s", m, rec.Code, rec.Body)
	}
	r := decode[reportResp](t, rec)
	return &r
}

func TestMonthReport(t *testing.T) {
	f := newReportFixture(t)

	r := f.report(t, f.c.owner, f.m2)
	want := reportResp{Month: f.m2.String()[:7], Period: f.m2, PeriodEnd: f.m2.AddMonths(1).AddDays(-1),
		Charged: 300_000 + 300_000 + 400_000 + 300_000, Refunds: 10_000, CollectedNet: 270_000, Corrections: -15_000,
		DebtEnd: 300_000 + 260_000 + 385_000 + 600_000, DebtorsEnd: 4, StudentsActiveEnd: 3, Joined: 2, Left: 1}
	want.Collected.Cash, want.Collected.Transfer, want.Collected.Finik, want.Collected.Total = 200_000, 50_000, 30_000, 280_000
	want.DebtEndNote = reports.DebtEndNote
	if *r != want {
		t.Errorf("отчёт %s:\n got  %+v\n want %+v", f.m2, *r, want)
	}
	if !strings.Contains(r.DebtEndNote, "отменили") {
		t.Errorf("нет описания debt_end: %q", r.DebtEndNote)
	}

	// M1: граница — оплата в 23:30 последнего дня по Бишкеку — здесь, а в 00:30 — уже в M2.
	r1 := f.report(t, f.c.owner, f.m1)
	if r1.Charged != 600_000 || r1.Collected.Cash != 100_000 || r1.Collected.Total != 100_000 || r1.DebtEnd != 200_000+300_000 ||
		r1.Joined != 2 || r1.Left != 0 || r1.StudentsActiveEnd != 2 || r1.Refunds != 0 {
		t.Errorf("отчёт %s = %+v", f.m1, *r1)
	}

	// Без ?m — текущий месяц; формат месяца строгий; только владелец.
	if rec := f.c.owner.do(http.MethodGet, centerPath(f.c.center, "/reports/month"), nil); decode[reportResp](t, rec).Period != f.c.today.FirstOfMonth() {
		t.Errorf("месяц по умолчанию: %s", rec.Body)
	}
	for _, bad := range []string{"2026-13", "2026-1", "10.2026", "2026-10-01"} {
		assertField(t, f.c.owner.do(http.MethodGet, centerPath(f.c.center, "/reports/month?m="+bad), nil), http.StatusBadRequest, "m")
	}
	admin, _ := addStaff(t, f.c.api, f.c.owner, f.c.center, "admin", "+996708000002")
	assertError(t, admin.do(http.MethodGet, centerPath(f.c.center, "/reports/month"), nil), http.StatusForbidden, "forbidden")
	assertError(t, admin.do(http.MethodGet, centerPath(f.c.center, "/reports/month/export.xlsx"), nil), http.StatusForbidden, "forbidden")
}

func TestMonthReportXLSX(t *testing.T) {
	f := newReportFixture(t)
	rec := f.c.owner.do(http.MethodGet, centerPath(f.c.center, "/reports/month/export.xlsx?m="+f.m2.String()[:7]), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "okuu-report-"+f.m2.String()[:7]+".xlsx") {
		t.Fatalf("export: %d %v", rec.Code, rec.Header())
	}
	x, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("xlsx не открывается: %v", err)
	}
	defer x.Close()
	if got := strings.Join(x.GetSheetList(), "|"); got != "Итоги|Операции|Должники на конец месяца" {
		t.Fatalf("листы = %s", got)
	}
	raw := excelize.Options{RawCellValue: true}

	sum, _ := x.GetRows("Итоги", raw)
	values := map[string]string{}
	for _, r := range sum {
		if len(r) >= 2 {
			values[r[0]] = r[1]
		}
	}
	for label, want := range map[string]string{
		"Месяц": f.m2.String()[:7], "Начислено, сом": "13000", "Собрано всего, сом": "2800", "  Finik": "300",
		"Возвраты, сом": "100", "Поправки (± к балансу), сом": "-150", "Долг на конец месяца, сом": "15450",
		"Должников на конец месяца": "4", "Пришло": "2", "Ушло": "1",
	} {
		if values[label] != want {
			t.Errorf("Итоги «%s» = %q, want %q", label, values[label], want)
		}
	}

	ops, _ := x.GetRows("Операции", raw)
	if len(ops) != 1+10 { // заголовок + 10 операций, созданных в M2 (по Бишкеку)
		t.Fatalf("строк на листе «Операции»: %d, want 11", len(ops))
	}
	cancelled := 0
	for _, r := range ops[1:] {
		if len(r) > 9 && r[9] == "да" {
			cancelled++
			if r[3] != "200" || r[2] != "Оплата" || r[4] != "Карта" || r[10] != "ошибка" {
				t.Errorf("отменённая операция: %v", r)
			}
		}
	}
	if cancelled != 1 {
		t.Errorf("отменённых в выгрузке: %d, want 1", cancelled)
	}

	debt, _ := x.GetRows("Должники на конец месяца", raw)
	if len(debt) != 5 || debt[1][0] != "Долгов Д" || debt[1][4] != "6000" || debt[1][1] != "ушёл" {
		t.Errorf("должники = %v", debt)
	}
}

func TestStudentLeftAt(t *testing.T) {
	c := newCatalog(t, "+996708100001")
	s := c.student(t, "Ученик", "+996708100101", nil)
	path := centerPath(c.center, "/students/"+s.ID.String())
	leftAt := func() *time.Time {
		var v *time.Time
		if err := c.api.pool.QueryRow(context.Background(), `SELECT left_at FROM students WHERE id = $1`, s.ID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"status": "left"})
	first := leftAt()
	// Без left_date последний день — сегодня: left_at — начало сегодняшнего дня в поясе центра.
	if want := time.Date(c.today.Year(), c.today.Month(), c.today.Day(), 0, 0, 0, 0, bishkek); first == nil || !first.Equal(want) {
		t.Fatalf("left_at после ухода = %v, want %v", first, want)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"full_name": "Ученик Переименован"}) // статус не менялся — дата та же
	if v := leftAt(); v == nil || !v.Equal(*first) {
		t.Errorf("left_at сдвинулся без смены статуса: %v → %v", first, v)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"status": "active"})
	if v := leftAt(); v != nil {
		t.Errorf("left_at после возврата = %v, want NULL", v)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"status": "paused"})
	if v := leftAt(); v != nil {
		t.Errorf("left_at на паузе = %v", v)
	}
}
