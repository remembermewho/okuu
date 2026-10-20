package httpapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/billing"
	"okuu/internal/civil"
	"okuu/internal/jobs"
)

func runMonthly(t *testing.T, c *catalog, today civil.Date) billing.MonthlyResult {
	t.Helper()
	res, err := billing.RunMonthly(context.Background(), c.api.pool, nil, c.center, today)
	if err != nil {
		t.Fatalf("RunMonthly(%s): %v", today, err)
	}
	return res
}

// chargesFor — начисления записи ученика за период (неотменённые).
func chargesFor(t *testing.T, c *catalog, sid uuid.UUID, period civil.Date) []ledgerRow {
	t.Helper()
	var out []ledgerRow
	for _, r := range ledgerOf(t, c.api, sid) {
		if r.Kind == "charge" && r.Period != nil && *r.Period == period {
			out = append(out, r)
		}
	}
	return out
}

func billedThrough(t *testing.T, c *catalog, sid uuid.UUID) *civil.Date {
	t.Helper()
	var d *civil.Date
	if err := c.api.pool.QueryRow(context.Background(),
		`SELECT billed_through FROM enrollments WHERE student_id = $1 ORDER BY start_date LIMIT 1`, sid).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

// R3–R7: кому и сколько начисляет billing.monthly за следующий месяц.
func TestBillingMonthly(t *testing.T) {
	c := newCatalog(t, "+996705000001")
	p0 := c.today.FirstOfMonth()
	p1 := p0.AddMonths(1)
	cur := map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"}

	active := c.student(t, "Активный", "+996705000101", nil)
	c.enroll(t, active.ID, cur)

	paused := c.student(t, "На паузе", "+996705000102", nil)
	c.enroll(t, paused.ID, cur)
	c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+paused.ID.String()), map[string]any{"status": "paused"})

	future := c.student(t, "С 4-го числа следующего месяца", "+996705000103", nil)
	c.enroll(t, future.ID, map[string]any{"group_id": c.group.ID, "start_date": p1.AddDays(3).String()})

	later := c.student(t, "Через два месяца", "+996705000104", nil)
	c.enroll(t, later.ID, map[string]any{"group_id": c.group.ID, "start_date": p1.AddMonths(1).String()})

	ended := c.student(t, "Закончил", "+996705000105", nil)
	e := c.enroll(t, ended.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})
	c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+ended.ID.String()+"/enrollments/"+e.Enrollment.ID.String()+"/end"), nil)

	left := c.student(t, "Ушёл", "+996705000106", nil)
	c.enroll(t, left.ID, cur)
	c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+left.ID.String()), map[string]any{"status": "left"})

	pkg := c.student(t, "Абонемент", "+996705000107", nil)
	c.enroll(t, pkg.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": c.today.String()})

	// Цена тарифа и скидка изменились после записи — новое начисление по новым (R6).
	t2 := createTariff(t, c.owner, c.center, monthly("Шахматы, месяц", 200_000))
	g2 := createGroup(t, c.owner, c.center, map[string]any{"name": "Шахматы", "default_tariff_id": t2.ID})
	repriced := c.student(t, "Новая цена", "+996705000108", nil)
	c.enroll(t, repriced.ID, map[string]any{"group_id": g2.ID, "start_date": c.today.String(), "first_month": "full"})
	c.owner.do(http.MethodPatch, centerPath(c.center, "/tariffs/"+t2.ID.String()), map[string]any{"price": 250_000})
	c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+repriced.ID.String()), map[string]any{"discount": discountJSON{"percent", 10}})

	// Фиксированная скидка больше новой цены — начисление на 0 (скидка урезана до цены), пометка discount_capped.
	t3 := createTariff(t, c.owner, c.center, monthly("Дорогой", 150_000))
	g3 := createGroup(t, c.owner, c.center, map[string]any{"name": "Дорогая группа", "default_tariff_id": t3.ID})
	broken := c.student(t, "Скидка больше цены", "+996705000109", &discountJSON{"fixed", 100_000})
	c.enroll(t, broken.ID, map[string]any{"group_id": g3.ID, "start_date": c.today.String(), "first_month": "full"})
	c.owner.do(http.MethodPatch, centerPath(c.center, "/tariffs/"+t3.ID.String()), map[string]any{"price": 80_000})

	// До 1-го числа следующего месяца — текущий период уже решён при записи (R5): ничего.
	if res := runMonthly(t, c, c.today); res.Charged != 0 || res.Paused != 0 {
		t.Fatalf("в текущем месяце повторно: %+v", res)
	}

	res := runMonthly(t, c, p1)
	if res.Period != p1 || res.Charged != 4 || res.Paused != 1 || res.Failed != 0 {
		t.Fatalf("RunMonthly(%s) = %+v, want charged 4 (активный, будущий, новая цена, скидка больше цены), paused 1, failed 0", p1, res)
	}
	due := p1.AddDays(0 + 5) // billing_day 1 → P + 0 + due_days 5 (R4)

	check := func(name string, sid uuid.UUID, wantAmount int64, wantBilled *civil.Date) {
		t.Helper()
		ch := chargesFor(t, c, sid, p1)
		switch {
		case wantAmount == 0 && len(ch) != 0:
			t.Errorf("%s: начисление не ожидалось: %+v", name, ch)
		case wantAmount != 0 && (len(ch) != 1 || ch[0].Amount != wantAmount || *ch[0].DueDate != due):
			t.Errorf("%s: начисления = %+v, want одно %d со сроком %s", name, ch, wantAmount, due)
		}
		got := billedThrough(t, c, sid)
		if (got == nil) != (wantBilled == nil) || (got != nil && *got != *wantBilled) {
			t.Errorf("%s: billed_through = %v, want %v", name, got, wantBilled)
		}
	}
	check("активный", active.ID, -300_000, &p1)
	check("пауза (R7)", paused.ID, 0, &p1)
	check("начинает в P1", future.ID, -300_000, &p1)
	check("начинает в P2", later.ID, 0, nil)
	check("завершённая запись", ended.ID, 0, &p0)
	check("ушёл (R7)", left.ID, 0, &p0)
	check("абонемент", pkg.ID, 0, nil)
	check("новая цена и скидка (R6)", repriced.ID, -225_000, &p1) // 250 000 − 10 %
	// Скидка больше цены: одно начисление на 0, discount = list_price = новая цена, billed_through = P1.
	if ch := chargesFor(t, c, broken.ID, p1); len(ch) != 1 || ch[0].Amount != 0 || *ch[0].ListPrice != 80_000 || *ch[0].Discount != 80_000 {
		t.Errorf("скидка больше цены: начисления = %+v, want одно на 0 (80000 − 80000)", ch)
	}
	if b := billedThrough(t, c, broken.ID); b == nil || *b != p1 {
		t.Errorf("скидка больше цены: billed_through = %v, want %s", b, p1)
	}

	// Начисления от системы: created_by = NULL, в журнале user_id = NULL.
	var sys int
	_ = c.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger WHERE center_id = $1 AND period = $2 AND created_by IS NULL`,
		c.center, p1).Scan(&sys)
	charges := filterAudit(auditLog(t, c.api, c.center), "ledger.charge")
	var system, capped int
	for _, a := range charges {
		if a.UserID == nil {
			system++
			if a.After["discount_capped"] == true {
				capped++
			}
		}
	}
	if sys != 4 || system != 4 || capped != 1 {
		t.Errorf("системных начислений %d, в журнале с user_id NULL %d, с discount_capped %d; want 4, 4, 1", sys, system, capped)
	}

	// Повторный запуск — ничего нового.
	if res := runMonthly(t, c, p1.AddDays(5)); res.Charged != 0 || res.Paused != 0 || res.Failed != 0 {
		t.Fatalf("повторный запуск: %+v", res)
	}
	// Отменили начисление — billed_through не сбрасывается, повторно не начисляем.
	ch := ledgerOf(t, c.api, active.ID)
	var p1Charge uuid.UUID
	_ = c.api.pool.QueryRow(context.Background(), `SELECT id FROM ledger WHERE student_id = $1 AND period = $2`, active.ID, p1).Scan(&p1Charge)
	if rec := c.cancel(t, c.owner, p1Charge, "Каникулы, не берём"); rec.Code != 200 {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if res := runMonthly(t, c, p1.AddDays(6)); res.Charged != 0 {
		t.Fatalf("после отмены начислено снова: %+v", res)
	}
	if n := len(ledgerOf(t, c.api, active.ID)); n != len(ch) {
		t.Errorf("операций %d, want %d", n, len(ch))
	}
}

// Прошлые месяцы не догоняем: пропущенный запуск за P1 не создаёт начисления за P1 при запуске в P2.
func TestBillingMonthlyDoesNotCatchUp(t *testing.T) {
	c := newCatalog(t, "+996705100001")
	s := c.student(t, "Ученик", "+996705100101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	p2 := c.today.FirstOfMonth().AddMonths(2)

	if res := runMonthly(t, c, p2); res.Charged != 1 {
		t.Fatalf("RunMonthly(P2) = %+v", res)
	}
	var periods []civil.Date
	for _, r := range ledgerOf(t, c.api, s.ID) {
		periods = append(periods, *r.Period)
	}
	if len(periods) != 2 || periods[0] != c.today.FirstOfMonth() || periods[1] != p2 {
		t.Errorf("периоды начислений = %v, want текущий и P2 (без P1)", periods)
	}
}

// billing_day = 10: до 10-го числа ничего, с 10-го — начисления со сроком 10 + due_days − 1.
func TestBillingMonthlyBillingDay(t *testing.T) {
	c := newCatalog(t, "+996705200001")
	if _, err := c.api.pool.Exec(context.Background(), `UPDATE centers SET billing_day = 10 WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	s := c.student(t, "Ученик", "+996705200101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	p1 := c.today.FirstOfMonth().AddMonths(1)

	for _, day := range []int{1, 5, 9} {
		if res := runMonthly(t, c, p1.AddDays(day-1)); res.Charged != 0 {
			t.Fatalf("%d-е число: %+v", day, res)
		}
	}
	if b := billedThrough(t, c, s.ID); b == nil || *b != c.today.FirstOfMonth() {
		t.Fatalf("billed_through до billing_day = %v", b)
	}
	if res := runMonthly(t, c, p1.AddDays(9)); res.Charged != 1 {
		t.Fatalf("10-е число: %+v", res)
	}
	ch := chargesFor(t, c, s.ID, p1)
	if len(ch) != 1 || *ch[0].DueDate != p1.AddDays(9+5) {
		t.Fatalf("начисление = %+v, want срок %s (R4: billing_day + due_days)", ch, p1.AddDays(9+5))
	}
}

// Параллельные запуски (два экземпляра сервера) — ровно одно начисление на запись.
func TestBillingMonthlyConcurrent(t *testing.T) {
	c := newCatalog(t, "+996705300001")
	var ids []uuid.UUID
	for i := range 5 {
		s := c.student(t, "Ученик "+string(rune('А'+i)), "+99670530010"+string(rune('0'+i)), nil)
		c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})
		ids = append(ids, s.ID)
	}
	p1 := c.today.FirstOfMonth().AddMonths(1)

	const runs = 4
	results := make([]billing.MonthlyResult, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = runMonthly(t, c, p1)
		}()
	}
	wg.Wait()

	total := 0
	for _, r := range results {
		total += r.Charged
		if r.Failed != 0 {
			t.Errorf("ошибки: %+v", r)
		}
	}
	if total != len(ids) {
		t.Errorf("начислено всего %d, want %d", total, len(ids))
	}
	for _, sid := range ids {
		if n := len(chargesFor(t, c, sid, p1)); n != 1 {
			t.Errorf("ученик %s: %d начислений за %s", sid, n, p1)
		}
	}
}

// Планировщик: запуск под pg_try_advisory_lock; занят — пропускаем; «сегодня» — в поясе центра.
func TestBillingJobAdvisoryLock(t *testing.T) {
	c := newCatalog(t, "+996705400001")
	s := c.student(t, "Ученик", "+996705400101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	p1 := c.today.FirstOfMonth().AddMonths(1)
	bishkek, _ := time.LoadLocation("Asia/Bishkek")
	// 31-е/30-е, 20:00 UTC — в Бишкеке уже 1-е число следующего месяца, 02:00.
	now := time.Date(p1.Year(), p1.Month(), 1, 2, 0, 0, 0, bishkek)
	ctx := context.Background()

	// Другой процесс держит блокировку — задача пропускается.
	conn, err := c.api.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, jobs.BillingLockKey); err != nil {
		t.Fatal(err)
	}
	ran, err := jobs.RunBillingOnce(ctx, c.api.pool, nil, now)
	if err != nil || ran {
		t.Fatalf("при занятой блокировке: ran=%v err=%v", ran, err)
	}
	if n := len(chargesFor(t, c, s.ID, p1)); n != 0 {
		t.Fatalf("начислено при занятой блокировке: %d", n)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, jobs.BillingLockKey); err != nil {
		t.Fatal(err)
	}
	conn.Release()

	// Блокировка свободна — запуск идёт, начисление за P1 (по бишкекской дате, хотя в UTC ещё прошлый месяц).
	if ran, err := jobs.RunBillingOnce(ctx, c.api.pool, nil, now); err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
	if n := len(chargesFor(t, c, s.ID, p1)); n != 1 {
		t.Fatalf("начислений за %s: %d, want 1", p1, n)
	}
	// Блокировка отпущена после запуска: следующий запуск снова выполняется (и ничего не дублирует).
	if ran, err := jobs.RunBillingOnce(ctx, c.api.pool, nil, now.Add(time.Hour)); err != nil || !ran {
		t.Fatalf("второй запуск: ran=%v err=%v", ran, err)
	}
	if n := len(chargesFor(t, c, s.ID, p1)); n != 1 {
		t.Fatalf("после второго запуска начислений %d, want 1", n)
	}
}
