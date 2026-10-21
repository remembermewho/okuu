package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
)

type operationResp struct {
	ID           uuid.UUID   `json:"id"`
	StudentID    uuid.UUID   `json:"student_id"`
	Kind         string      `json:"kind"`
	Amount       int64       `json:"amount"`
	Method       *string     `json:"method"`
	Comment      *string     `json:"comment"`
	Period       *civil.Date `json:"period"`
	DueDate      *civil.Date `json:"due_date"`
	CreatedBy    *uuid.UUID  `json:"created_by"`
	Cancelled    bool        `json:"cancelled"`
	CancelReason *string     `json:"cancel_reason"`
	CancelledBy  *uuid.UUID  `json:"cancelled_by"`
}

type debtorResp struct {
	StudentID uuid.UUID `json:"student_id"`
	FullName  string    `json:"full_name"`
	Status    string    `json:"status"`
	Groups    []refJSON `json:"groups"`
	Parent    *struct {
		Name           string     `json:"name"`
		Phone          string     `json:"phone"`
		TgStatus       string     `json:"tg_status"`
		LastReminderAt *time.Time `json:"last_reminder_at"`
	} `json:"parent"`
	Debt          int64      `json:"debt"`
	DaysOverdue   int        `json:"days_overdue"`
	OldestDueDate civil.Date `json:"oldest_due_date"`
	DueInDays     int        `json:"due_in_days"`
	RemindedToday bool       `json:"reminded_today"`
}

type refJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type debtorsResp struct {
	Items      []debtorResp `json:"items"`
	NextCursor *string      `json:"next_cursor"`
	Summary    struct {
		Period       civil.Date `json:"period"`
		Charged      int64      `json:"charged"`
		Collected    int64      `json:"collected"`
		DebtTotal    int64      `json:"debt_total"`
		DebtorsCount int        `json:"debtors_count"`
		PendingTotal int64      `json:"pending_total"`
		PendingCount int        `json:"pending_count"`
	} `json:"summary"`
}

// op — POST /payments от имени владельца с заданным Idempotency-Key ("" — без заголовка).
func (c *catalog) op(t *testing.T, sid uuid.UUID, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	if key != "" {
		c.owner.headers = map[string]string{"Idempotency-Key": key}
		defer func() { c.owner.headers = nil }()
	}
	return c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+sid.String()+"/payments"), body)
}

func (c *catalog) mustOp(t *testing.T, sid uuid.UUID, body map[string]any) operationResp {
	t.Helper()
	rec := c.op(t, sid, uuid.NewString(), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("operation %v: %d %s", body, rec.Code, rec.Body)
	}
	return decode[operationResp](t, rec)
}

func (c *catalog) cancel(t *testing.T, as *client, id uuid.UUID, reason string) *httptest.ResponseRecorder {
	t.Helper()
	return as.do(http.MethodPost, centerPath(c.center, "/ledger/"+id.String()+"/cancel"), map[string]string{"reason": reason})
}

func pay(amount int64) map[string]any {
	return map[string]any{"kind": "payment", "amount": amount, "method": "cash"}
}

func TestPaymentsRefundsCorrections(t *testing.T) {
	c := newCatalog(t, "+996704000001")
	s := c.student(t, "Плательщик", "+996704000101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"}) // -300000

	// Оплата: 201, баланс растёт (R13), частичная оплата разрешена.
	rec := c.op(t, s.ID, "pay-1", map[string]any{"kind": "payment", "amount": 100_000, "method": "transfer", "comment": "  через MBank "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("payment: %d %s", rec.Code, rec.Body)
	}
	p := decode[operationResp](t, rec)
	if p.Kind != "payment" || p.Amount != 100_000 || *p.Method != "transfer" || *p.Comment != "через MBank" || p.Cancelled || p.CreatedBy == nil {
		t.Fatalf("payment = %+v", p)
	}
	if b := c.card(t, s.ID).Balance; b != -200_000 {
		t.Errorf("balance = %d, want -200000", b)
	}

	// Повтор с тем же ключом и телом — 200 и та же операция, второй строки нет.
	rec = c.op(t, s.ID, "pay-1", map[string]any{"kind": "payment", "amount": 100_000, "method": "transfer", "comment": "через MBank"})
	if rec.Code != http.StatusOK || decode[operationResp](t, rec).ID != p.ID {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body)
	}
	// Тот же ключ, другие данные — 422.
	other := c.student(t, "Другой", "+996704000102", nil)
	for name, tt := range map[string]struct {
		sid  uuid.UUID
		body map[string]any
	}{
		"другая сумма":          {s.ID, map[string]any{"kind": "payment", "amount": 100_001, "method": "transfer", "comment": "через MBank"}},
		"другой способ":         {s.ID, map[string]any{"kind": "payment", "amount": 100_000, "method": "cash", "comment": "через MBank"}},
		"без комментария":       {s.ID, map[string]any{"kind": "payment", "amount": 100_000, "method": "transfer"}},
		"возврат вместо оплаты": {s.ID, map[string]any{"kind": "refund", "amount": 100_000, "method": "transfer", "comment": "через MBank"}},
		"другой ученик":         {other.ID, map[string]any{"kind": "payment", "amount": 100_000, "method": "transfer", "comment": "через MBank"}},
	} {
		t.Run("mismatch "+name, func(t *testing.T) {
			assertError(t, c.op(t, tt.sid, "pay-1", tt.body), http.StatusUnprocessableEntity, "idempotency_mismatch")
		})
	}

	// Возврат уменьшает баланс (R16); поправка — со знаком и причиной.
	r := c.mustOp(t, s.ID, map[string]any{"kind": "refund", "amount": 30_000, "method": "cash"})
	if r.Amount != -30_000 {
		t.Errorf("refund amount = %d, want -30000", r.Amount)
	}
	plus := c.mustOp(t, s.ID, map[string]any{"kind": "correction", "amount": 50_000, "comment": "Перенос долга из Excel: переплата"})
	minus := c.mustOp(t, s.ID, map[string]any{"kind": "correction", "amount": -20_000, "comment": "Долг за сентябрь из Excel"})
	if plus.Amount != 50_000 || minus.Amount != -20_000 || plus.Method != nil {
		t.Errorf("corrections = %+v / %+v", plus, minus)
	}
	want := int64(-300_000 + 100_000 - 30_000 + 50_000 - 20_000)
	if b := c.card(t, s.ID).Balance; b != want || b != dbBalance(t, c.api, s.ID) {
		t.Errorf("balance = %d, want %d (ledger %d)", b, want, dbBalance(t, c.api, s.ID))
	}

	invalid := []struct {
		name  string
		key   string
		body  map[string]any
		field string
	}{
		{"без Idempotency-Key", "", pay(100), "Idempotency-Key"},
		{"длинный ключ", strings.Repeat("k", 101), pay(100), "Idempotency-Key"},
		{"оплата 0", "k-0", pay(0), "amount"},
		{"оплата отрицательная", "k-1", pay(-100), "amount"},
		{"без способа", "k-2", map[string]any{"kind": "payment", "amount": 100}, "method"},
		{"способ finik вручную", "k-3", map[string]any{"kind": "payment", "amount": 100, "method": "finik"}, "method"},
		{"неизвестный способ", "k-4", map[string]any{"kind": "payment", "amount": 100, "method": "barter"}, "method"},
		{"возврат без способа", "k-5", map[string]any{"kind": "refund", "amount": 100}, "method"},
		{"поправка без причины", "k-6", map[string]any{"kind": "correction", "amount": 100}, "comment"},
		{"поправка 0", "k-7", map[string]any{"kind": "correction", "amount": 0, "comment": "x"}, "amount"},
		{"поправка со способом", "k-8", map[string]any{"kind": "correction", "amount": 100, "comment": "x", "method": "cash"}, "method"},
		{"неизвестный вид", "k-9", map[string]any{"kind": "charge", "amount": 100}, "kind"},
		{"длинный комментарий", "k-10", map[string]any{"kind": "payment", "amount": 100, "method": "cash", "comment": strings.Repeat("я", 501)}, "comment"},
		{"сумма больше максимума", "k-11", pay(10_000_000_001), "amount"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			assertField(t, c.op(t, s.ID, tt.key, tt.body), http.StatusBadRequest, tt.field)
		})
	}
	assertError(t, c.op(t, uuid.New(), "k-404", pay(100)), http.StatusNotFound, "not_found")
	// Дробная сумма — не тыйыны.
	assertError(t, c.op(t, s.ID, "k-frac", map[string]any{"kind": "payment", "amount": 100.5, "method": "cash"}), http.StatusBadRequest, "bad_request")

	var rows int
	_ = c.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger WHERE student_id = $1 AND kind <> 'charge'`, s.ID).Scan(&rows)
	if rows != 4 {
		t.Errorf("операций %d, want 4 (оплата, возврат, 2 поправки)", rows)
	}
	if got := strings.Join(auditActions(filterAudit(auditLog(t, c.api, c.center), "ledger.payment", "ledger.refund", "ledger.correction")), ","); got !=
		"ledger.payment,ledger.refund,ledger.correction,ledger.correction" {
		t.Errorf("audit = %s", got)
	}
}

// Одновременные запросы с одним Idempotency-Key — одна операция, у всех ответов один id.
func TestPaymentIdempotencyConcurrent(t *testing.T) {
	c := newCatalog(t, "+996704100001")
	s := c.student(t, "Ученик", "+996704100101", nil)

	const n = 6
	ids := make([]uuid.UUID, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl := c.api.client(t, "10.7.0.1")
			for k, v := range c.owner.cookies { // своя копия: клиент пишет в карту cookie
				cl.cookies[k] = v
			}
			cl.headers = map[string]string{"Idempotency-Key": "same-key"}
			rec := cl.do(http.MethodPost, centerPath(c.center, "/students/"+s.ID.String()+"/payments"), pay(70_000))
			codes[i] = rec.Code
			if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
				ids[i] = decode[operationResp](t, rec).ID
			}
		}()
	}
	wg.Wait()
	created := 0
	for i := range n {
		if codes[i] == http.StatusCreated {
			created++
		} else if codes[i] != http.StatusOK {
			t.Errorf("запрос %d: статус %d", i, codes[i])
		}
		if ids[i] != ids[0] {
			t.Errorf("разные операции: %v", ids)
		}
	}
	if created != 1 {
		t.Errorf("201 получили %d запросов, want 1 (коды %v)", created, codes)
	}
	if b := dbBalance(t, c.api, s.ID); b != 70_000 {
		t.Errorf("balance = %d, want 70000 (одна оплата)", b)
	}
}

func TestCancelOperations(t *testing.T) {
	c := newCatalog(t, "+996704200001")
	admin, _ := addStaff(t, c.api, c.owner, c.center, "admin", "+996704200002")
	s := c.student(t, "Ученик", "+996704200101", nil)
	enr := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	p := c.mustOp(t, s.ID, pay(300_000))

	// Отмена оплаты: причина обязательна (3–500), отменённая не входит в баланс (R15).
	assertField(t, c.cancel(t, admin, p.ID, "ок"), http.StatusBadRequest, "reason")
	assertField(t, c.cancel(t, admin, p.ID, strings.Repeat("я", 501)), http.StatusBadRequest, "reason")
	rec := c.cancel(t, admin, p.ID, "  Ошибка кассира  ")
	got := decode[operationResp](t, rec)
	if rec.Code != http.StatusOK || !got.Cancelled || *got.CancelReason != "Ошибка кассира" || got.CancelledBy == nil {
		t.Fatalf("cancel: %d %+v", rec.Code, got)
	}
	if b := c.card(t, s.ID).Balance; b != -300_000 {
		t.Errorf("balance после отмены оплаты = %d, want -300000", b)
	}
	assertError(t, c.cancel(t, c.owner, p.ID, "ещё раз"), http.StatusConflict, "already_cancelled")
	assertError(t, c.cancel(t, c.owner, uuid.New(), "нет такой"), http.StatusNotFound, "not_found")

	// Отмена начисления не сбрасывает billed_through.
	rec = c.cancel(t, c.owner, enr.Charge.ID, "Решили не брать за первый месяц")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel charge: %d %s", rec.Code, rec.Body)
	}
	if e := c.card(t, s.ID).Enrollments[0]; e.BilledThrough == nil || *e.BilledThrough != c.today.FirstOfMonth() {
		t.Errorf("billed_through после отмены начисления = %v", e.BilledThrough)
	}
	if b := c.card(t, s.ID).Balance; b != 0 {
		t.Errorf("balance = %d, want 0", b)
	}

	// Оплату Finik отменяет только владелец (R15).
	var finikID uuid.UUID
	if err := c.api.pool.QueryRow(context.Background(), `INSERT INTO ledger (center_id, student_id, kind, amount, method, external_id)
		VALUES ($1, $2, 'payment', 300000, 'finik', 'finik-tx-1') RETURNING id`, c.center, s.ID).Scan(&finikID); err != nil {
		t.Fatal(err)
	}
	assertError(t, c.cancel(t, admin, finikID, "Ошибочный платёж"), http.StatusForbidden, "forbidden")
	if rec := c.cancel(t, c.owner, finikID, "Ошибочный платёж"); rec.Code != http.StatusOK {
		t.Fatalf("owner cancel finik: %d %s", rec.Code, rec.Body)
	}

	cancels := filterAudit(auditLog(t, c.api, c.center), "ledger.cancel")
	if len(cancels) != 3 || cancels[0].After["reason"] != "Ошибка кассира" || cancels[0].Before["amount"] != float64(300_000) {
		t.Errorf("ledger.cancel = %+v", cancels)
	}
}

func TestStudentLedgerHistory(t *testing.T) {
	c := newCatalog(t, "+996704300001")
	s := c.student(t, "История", "+996704300101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	p1 := c.mustOp(t, s.ID, pay(100_000))
	p2 := c.mustOp(t, s.ID, pay(50_000))
	c.cancel(t, c.owner, p1.ID, "Задвоили")
	r := c.mustOp(t, s.ID, map[string]any{"kind": "refund", "amount": 10_000, "method": "card"})

	path := centerPath(c.center, "/students/"+s.ID.String()+"/ledger")
	all := decode[itemsResp[operationResp]](t, c.owner.do(http.MethodGet, path, nil))
	if all.Items == nil || len(*all.Items) != 4 || all.NextCursor != nil {
		t.Fatalf("history = %+v", all)
	}
	items := *all.Items
	// Новые сверху; отменённая — с флагом и причиной.
	if items[0].ID != r.ID || items[1].ID != p2.ID || items[2].ID != p1.ID || items[3].Kind != "charge" {
		t.Errorf("порядок: %v %v %v %v", items[0].Kind, items[1].Kind, items[2].Kind, items[3].Kind)
	}
	if !items[2].Cancelled || *items[2].CancelReason != "Задвоили" || items[1].Cancelled {
		t.Errorf("флаг отмены: %+v", items[2])
	}

	// Постранично по 3.
	page1 := decode[itemsResp[operationResp]](t, c.owner.do(http.MethodGet, path+"?limit=3", nil))
	if len(*page1.Items) != 3 || page1.NextCursor == nil {
		t.Fatalf("page1 = %+v", page1)
	}
	page2 := decode[itemsResp[operationResp]](t, c.owner.do(http.MethodGet, path+"?limit=3&cursor="+*page1.NextCursor, nil))
	if len(*page2.Items) != 1 || (*page2.Items)[0].ID != items[3].ID || page2.NextCursor != nil {
		t.Fatalf("page2 = %+v", page2)
	}

	assertField(t, c.owner.do(http.MethodGet, path+"?limit=0", nil), http.StatusBadRequest, "limit")
	assertField(t, c.owner.do(http.MethodGet, path+"?cursor=zzz", nil), http.StatusBadRequest, "cursor")
	assertError(t, c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+uuid.NewString()+"/ledger"), nil), http.StatusNotFound, "not_found")
}

// R14: должник — баланс < 0 и срок самого старого непокрытого начисления прошёл; итоги месяца.
func TestDebtors(t *testing.T) {
	c := newCatalog(t, "+996704400001")
	ctx := context.Background()
	g2 := createGroup(t, c.owner, c.center, map[string]any{"name": "Шахматы", "default_tariff_id": c.monthly.ID})
	full := map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"}
	overdue := func(t *testing.T, sid uuid.UUID, days int) {
		t.Helper()
		if _, err := c.api.pool.Exec(ctx, `UPDATE ledger SET due_date = $2 WHERE student_id = $1 AND kind = 'charge'`,
			sid, c.today.AddDays(-days)); err != nil {
			t.Fatal(err)
		}
	}

	a := c.student(t, "Асанов А", "+996704400101", nil) // 300 000 просрочено 10 дней, оплатил 100 000 → долг 200 000
	c.enroll(t, a.ID, full)
	overdue(t, a.ID, 10)
	c.mustOp(t, a.ID, pay(100_000))

	b := c.student(t, "Бекова Б", "+996704400102", nil) // абонемент, срок не наступил — не должник
	c.enroll(t, b.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": c.today.String()})

	cc := c.student(t, "Ваня В", "+996704400103", nil) // оплатил полностью, потом возврат сегодня — срок сегодня, не должник
	c.enroll(t, cc.ID, full)
	overdue(t, cc.ID, 3)
	c.mustOp(t, cc.ID, pay(300_000))
	c.mustOp(t, cc.ID, map[string]any{"kind": "refund", "amount": 50_000, "method": "cash"})
	cancelled := c.mustOp(t, cc.ID, pay(999))
	c.cancel(t, c.owner, cancelled.ID, "Ошибка")

	d := c.student(t, "Долгов Д", "+996704400104", nil) // ушёл, долг 300 000 просрочен 20 дней — виден с пометкой (R7)
	c.enroll(t, d.ID, full)
	overdue(t, d.ID, 20)
	c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+d.ID.String()), map[string]any{"status": "left"})

	e := c.student(t, "Ешенов Е", "+996704400105", nil) // группа «Шахматы», 300 000 просрочено 1 день
	c.enroll(t, e.ID, map[string]any{"group_id": g2.ID, "start_date": c.today.String(), "first_month": "full"})
	overdue(t, e.ID, 1)

	// Прошлый месяц: начисление и оплата — не входят в итоги текущего месяца.
	f := c.student(t, "Фёдоров Ф", "+996704400106", nil)
	prev := c.today.FirstOfMonth().AddMonths(-1)
	if _, err := c.api.pool.Exec(ctx, `INSERT INTO ledger (center_id, student_id, kind, amount, period, due_date, created_at)
		VALUES ($1, $2, 'charge', -300000, $3, $4, $5), ($1, $2, 'payment', 300000, NULL, NULL, $5)`,
		c.center, f.ID, prev, prev.AddDays(5), time.Date(prev.Year(), prev.Month(), 3, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	res := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors"), nil))
	var names []string
	for _, it := range res.Items {
		names = append(names, it.FullName)
	}
	// По убыванию долга; при равном долге — по имени.
	if strings.Join(names, "|") != "Долгов Д|Ешенов Е|Асанов А" || res.NextCursor != nil {
		t.Fatalf("должники = %v", names)
	}
	byName := map[string]debtorResp{}
	for _, it := range res.Items {
		byName[it.FullName] = it
	}
	if x := byName["Асанов А"]; x.Debt != 200_000 || x.DaysOverdue != 10 || x.OldestDueDate != c.today.AddDays(-10) ||
		x.Parent == nil || x.Parent.Phone != "+996704400101" || x.Parent.TgStatus != "none" || len(x.Groups) != 1 {
		t.Errorf("Асанов = %+v", x)
	}
	if x := byName["Долгов Д"]; x.Debt != 300_000 || x.DaysOverdue != 20 || x.Status != "left" || len(x.Groups) != 0 {
		t.Errorf("Долгов = %+v", x)
	}
	if x := byName["Ешенов Е"]; x.DaysOverdue != 1 || x.Groups[0].Name != "Шахматы" {
		t.Errorf("Ешенов = %+v", x)
	}

	s := res.Summary
	if s.Period != c.today.FirstOfMonth() || s.Charged != 4*300_000+400_000 || s.Collected != 100_000+300_000-50_000 ||
		s.DebtTotal != 800_000 || s.DebtorsCount != 3 {
		t.Errorf("summary = %+v, want charged 1600000, collected 350000, debt 800000, count 3", s)
	}
	// Долг без просрочки: Бекова (абонемент 400 000, срок через 5 дней) и Ваня (возврат 50 000 сегодня).
	if s.PendingTotal != 450_000 || s.PendingCount != 2 {
		t.Errorf("pending = %d / %d, want 450000 / 2", s.PendingTotal, s.PendingCount)
	}

	// Фильтр по группе: в «Шахматах» — только Ешенов; итоги — по всему центру.
	g := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?group="+g2.ID.String()), nil))
	if len(g.Items) != 1 || g.Items[0].FullName != "Ешенов Е" || g.Summary != res.Summary {
		t.Errorf("?group = %+v", g)
	}
	// Ушедший виден и в фильтре по группе, где он учился.
	g = decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?group="+c.group.ID.String()), nil))
	if len(g.Items) != 2 || g.Items[0].FullName != "Долгов Д" {
		t.Errorf("?group A1 = %+v", g.Items)
	}
	assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?group=xyz"), nil), http.StatusBadRequest, "group")

	// «Ожидают оплаты»: по ближайшему сроку — Ваня (возврат сегодня, 0 дней), Бекова (через 5 дней).
	explicit := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?status=overdue"), nil))
	if len(explicit.Items) != 3 {
		t.Errorf("status=overdue = %d строк", len(explicit.Items))
	}
	p := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?status=pending"), nil))
	if len(p.Items) != 2 || p.Items[0].FullName != "Ваня В" || p.Items[1].FullName != "Бекова Б" || p.Summary != res.Summary {
		t.Fatalf("pending = %+v", p.Items)
	}
	if x := p.Items[0]; x.Debt != 50_000 || x.DueInDays != 0 || x.DaysOverdue != 0 || x.OldestDueDate != c.today {
		t.Errorf("Ваня = %+v", x)
	}
	if x := p.Items[1]; x.Debt != 400_000 || x.DueInDays != 5 || x.OldestDueDate != c.today.AddDays(5) {
		t.Errorf("Бекова = %+v", x)
	}
	// Фильтр по группе работает и в «Ожидают оплаты».
	pg := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?status=pending&group="+g2.ID.String()), nil))
	if len(pg.Items) != 0 {
		t.Errorf("pending в «Шахматах» = %+v", pg.Items)
	}
	assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?status=all"), nil), http.StatusBadRequest, "status")

	// «Напоминали сегодня»: last_reminder_at родителя — сегодня в поясе центра; вчера — нет.
	if _, err := c.api.pool.Exec(ctx, `UPDATE parents SET last_reminder_at = $2 WHERE phone = $1`, "+996704400101", c.api.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.pool.Exec(ctx, `UPDATE parents SET last_reminder_at = $2 WHERE phone = $1`, "+996704400105", c.api.clock.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	res = decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors"), nil))
	for _, it := range res.Items {
		want := it.FullName == "Асанов А"
		if it.RemindedToday != want {
			t.Errorf("%s reminded_today = %v", it.FullName, it.RemindedToday)
		}
		if it.FullName == "Ешенов Е" && (it.Parent.LastReminderAt == nil) {
			t.Error("Ешенов: нет last_reminder_at")
		}
	}
}

func TestMoneyEndpointsTeacherForbidden(t *testing.T) {
	c := newCatalog(t, "+996704500001")
	s := c.student(t, "Ученик", "+996704500101", nil)
	enr := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996704500002")
	teacher.headers = map[string]string{"Idempotency-Key": "teacher"}

	for _, ep := range []endpoint{
		{http.MethodPost, "/students/" + s.ID.String() + "/payments", pay(100), true},
		{http.MethodGet, "/students/" + s.ID.String() + "/ledger", nil, true},
		{http.MethodPost, "/ledger/" + enr.Charge.ID.String() + "/cancel", map[string]string{"reason": "учитель"}, true},
		{http.MethodGet, "/debtors", nil, false},
	} {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			assertError(t, teacher.do(ep.method, centerPath(c.center, ep.path), ep.body), http.StatusForbidden, "forbidden")
		})
	}
	if b := dbBalance(t, c.api, s.ID); b != -300_000 {
		t.Errorf("баланс изменился: %d", b)
	}
}
