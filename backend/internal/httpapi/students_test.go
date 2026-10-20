package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
)

type parentResp struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	TgStatus string    `json:"tg_status"`
}

type discountJSON struct {
	Kind  string `json:"kind"`
	Value int64  `json:"value"`
}

type enrollmentResp struct {
	ID    uuid.UUID `json:"id"`
	Group struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"group"`
	Tariff struct {
		ID    uuid.UUID `json:"id"`
		Kind  string    `json:"kind"`
		Price int64     `json:"price"`
	} `json:"tariff"`
	StartDate     civil.Date  `json:"start_date"`
	EndDate       *civil.Date `json:"end_date"`
	LessonsLeft   *int32      `json:"lessons_left"`
	BilledThrough *civil.Date `json:"billed_through"`
	Active        bool        `json:"active"`
}

type studentResp struct {
	ID             uuid.UUID        `json:"id"`
	FullName       string           `json:"full_name"`
	BirthDate      *civil.Date      `json:"birth_date"`
	Status         string           `json:"status"`
	Discount       *discountJSON    `json:"discount"`
	Parent         *parentResp      `json:"parent"`
	Enrollments    []enrollmentResp `json:"enrollments"`
	Balance        int64            `json:"balance"`
	ParentExisting bool             `json:"parent_existing"`
}

type chargeResp struct {
	ID        uuid.UUID   `json:"id"`
	Amount    int64       `json:"amount"`
	ListPrice int64       `json:"list_price"`
	Discount  int64       `json:"discount"`
	Period    *civil.Date `json:"period"`
	DueDate   civil.Date  `json:"due_date"`
}

type enrollResp struct {
	Enrollment enrollmentResp `json:"enrollment"`
	Charge     *chargeResp    `json:"charge"`
}

type studentListResp struct {
	Items []struct {
		ID       uuid.UUID   `json:"id"`
		FullName string      `json:"full_name"`
		Status   string      `json:"status"`
		Parent   *parentResp `json:"parent"`
		Groups   []struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"groups"`
		Balance int64 `json:"balance"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// catalog — центр с владельцем, месячным тарифом и группой (тариф по умолчанию — месячный).
type catalog struct {
	api     *testAPI
	owner   *client
	center  uuid.UUID
	monthly tariffResp
	pkg     tariffResp
	group   groupResp
	today   civil.Date // «сегодня» в Бишкеке по часам теста
}

func newCatalog(t *testing.T, phone string) *catalog {
	t.Helper()
	return newCatalogOn(t, newTestAPI(t), phone)
}

// newCatalogOn — то же в уже созданном API (несколько центров в одной БД).
func newCatalogOn(t *testing.T, api *testAPI, phone string) *catalog {
	t.Helper()
	owner := api.client(t, "10.0.0.1")
	center := owner.register(phone, "Центр "+phone).Centers[0].ID
	c := &catalog{api: api, owner: owner, center: center}
	c.monthly = createTariff(t, owner, center, monthly("Месяц", 300_000))
	c.pkg = createTariff(t, owner, center, map[string]any{"name": "Абонемент 8", "kind": "package", "price": 400_000, "lessons_count": 8})
	c.group = createGroup(t, owner, center, map[string]any{"name": "Английский A1", "default_tariff_id": c.monthly.ID})
	today, err := civil.Today(api.clock.Now(), "Asia/Bishkek")
	if err != nil {
		t.Fatal(err)
	}
	c.today = today
	return c
}

func (c *catalog) student(t *testing.T, name, parentPhone string, discount *discountJSON) studentResp {
	t.Helper()
	body := map[string]any{"full_name": name, "parent": map[string]string{"name": "Родитель " + name, "phone": parentPhone}}
	if discount != nil {
		body["discount"] = discount
	}
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create student: %d %s", rec.Code, rec.Body)
	}
	return decode[studentResp](t, rec)
}

func (c *catalog) enroll(t *testing.T, sid uuid.UUID, body map[string]any) enrollResp {
	t.Helper()
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+sid.String()+"/enrollments"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %v: %d %s", body, rec.Code, rec.Body)
	}
	return decode[enrollResp](t, rec)
}

func (c *catalog) card(t *testing.T, sid uuid.UUID) studentResp {
	t.Helper()
	rec := c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+sid.String()), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("card: %d %s", rec.Code, rec.Body)
	}
	return decode[studentResp](t, rec)
}

type ledgerRow struct {
	Kind      string
	Amount    int64
	ListPrice *int64
	Discount  *int64
	Period    *civil.Date
	DueDate   *civil.Date
	Comment   *string
}

func ledgerOf(t *testing.T, api *testAPI, sid uuid.UUID) []ledgerRow {
	t.Helper()
	rows, err := api.pool.Query(context.Background(),
		`SELECT kind, amount, list_price, discount, period, due_date, comment FROM ledger WHERE student_id = $1 ORDER BY created_at, id`, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		if err := rows.Scan(&r.Kind, &r.Amount, &r.ListPrice, &r.Discount, &r.Period, &r.DueDate, &r.Comment); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func dbBalance(t *testing.T, api *testAPI, sid uuid.UUID) int64 {
	t.Helper()
	var b int64
	if err := api.pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount), 0) FROM ledger WHERE student_id = $1 AND cancelled_at IS NULL`, sid).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStudentCreateAndParentReuse(t *testing.T) {
	c := newCatalog(t, "+996702000001")

	a := c.student(t, "Асанов Бакыт", "0700 111 222", nil)
	if a.ParentExisting || a.Parent == nil || a.Parent.Phone != "+996700111222" || a.Parent.Name != "Родитель Асанов Бакыт" ||
		a.Parent.TgStatus != "none" || a.Status != "active" || a.Balance != 0 || a.Enrollments == nil || len(a.Enrollments) != 0 {
		t.Fatalf("student A = %+v", a)
	}

	// Тот же родитель (телефон в другом формате) — переиспользуется, имя не перезаписывается.
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students"), map[string]any{
		"full_name": "Асанова Айпери", "birth_date": "2015-03-08",
		"parent":   map[string]string{"name": "Мама", "phone": "+996 700 111 222"},
		"discount": discountJSON{"percent", 10},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("student B: %d %s", rec.Code, rec.Body)
	}
	b := decode[studentResp](t, rec)
	if !b.ParentExisting || b.Parent.ID != a.Parent.ID || b.Parent.Name != "Родитель Асанов Бакыт" ||
		b.BirthDate == nil || *b.BirthDate != civil.New(2015, 3, 8) || b.Discount == nil || b.Discount.Value != 10 {
		t.Fatalf("student B = %+v", b)
	}
	var parents int
	_ = c.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM parents WHERE center_id = $1`, c.center).Scan(&parents)
	if parents != 1 {
		t.Errorf("родителей %d, want 1", parents)
	}

	ok := map[string]any{"full_name": "x", "parent": map[string]string{"name": "p", "phone": "0700999999"}}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	tomorrow := c.today.AddDays(1)
	invalid := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"без ФИО", with("full_name", " "), "full_name"},
		{"телефон родителя", with("parent", map[string]string{"name": "p", "phone": "12345"}), "parent.phone"},
		{"имя родителя", with("parent", map[string]string{"name": "", "phone": "0700999999"}), "parent.name"},
		{"без родителя", map[string]any{"full_name": "x"}, "parent.name"},
		{"скидка 150%", with("discount", discountJSON{"percent", 150}), "discount"},
		{"скидка отрицательная", with("discount", discountJSON{"fixed", -1}), "discount"},
		{"вид скидки", with("discount", discountJSON{"sibling", 1}), "discount"},
		{"дата рождения в будущем", with("birth_date", tomorrow.String()), "birth_date"},
		{"дата рождения до 1900", with("birth_date", "1899-12-31"), "birth_date"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			assertField(t, c.owner.do(http.MethodPost, centerPath(c.center, "/students"), tt.body), http.StatusBadRequest, tt.field)
		})
	}
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/students"), with("birth_date", "08.03.2015")),
		http.StatusBadRequest, "bad_request")

	if got := auditActions(filterAudit(auditLog(t, c.api, c.center), "student.")); strings.Join(got, ",") != "student.create,student.create" {
		t.Errorf("audit = %v", got)
	}
}

func TestStudentPatchAndParent(t *testing.T) {
	c := newCatalog(t, "+996702100001")
	a := c.student(t, "Брат", "0700 222 001", nil)
	b := c.student(t, "Сестра", "0700 222 001", nil) // тот же родитель
	other := c.student(t, "Чужой", "0700 222 002", nil)
	path := centerPath(c.center, "/students/"+a.ID.String())

	rec := c.owner.do(http.MethodPatch, path, map[string]any{
		"full_name": "Брат Старший", "birth_date": "2014-01-02", "discount": discountJSON{"fixed", 50_000},
	})
	got := decode[studentResp](t, rec)
	if rec.Code != http.StatusOK || got.FullName != "Брат Старший" || *got.BirthDate != civil.New(2014, 1, 2) ||
		got.Discount == nil || got.Discount.Kind != "fixed" {
		t.Fatalf("patch: %d %+v", rec.Code, got)
	}
	// null снимает скидку и дату рождения.
	got = decode[studentResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"discount": nil, "birth_date": nil}))
	if got.Discount != nil || got.BirthDate != nil {
		t.Fatalf("patch nulls: %+v", got)
	}

	// Родитель общий: новое имя и телефон видны у обоих детей.
	rec = c.owner.do(http.MethodPatch, path, map[string]any{"parent": map[string]any{"name": "Гульнара", "phone": "0555 333 444"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch parent: %d %s", rec.Code, rec.Body)
	}
	if sib := c.card(t, b.ID); sib.Parent.Name != "Гульнара" || sib.Parent.Phone != "+996555333444" {
		t.Errorf("у сестры родитель = %+v", sib.Parent)
	}

	// Телефон другого родителя центра — 409, ничего не меняется.
	assertError(t, c.owner.do(http.MethodPatch, path, map[string]any{"parent": map[string]any{"phone": other.Parent.Phone}}),
		http.StatusConflict, "parent_phone_taken")
	if p := c.card(t, a.ID).Parent; p.Phone != "+996555333444" {
		t.Errorf("телефон изменился после 409: %+v", p)
	}

	assertField(t, c.owner.do(http.MethodPatch, path, map[string]any{"status": "graduated"}), http.StatusBadRequest, "status")
	assertField(t, c.owner.do(http.MethodPatch, path, map[string]any{"full_name": nil}), http.StatusBadRequest, "full_name")
	assertField(t, c.owner.do(http.MethodPatch, path, map[string]any{"parent": map[string]any{"phone": "abc"}}), http.StatusBadRequest, "parent.phone")
	assertError(t, c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+uuid.NewString()), map[string]any{"full_name": "x"}),
		http.StatusNotFound, "not_found")

	log := auditLog(t, c.api, c.center)
	if got := strings.Join(auditActions(filterAudit(log, "student.update", "parent.update")), ","); got !=
		"student.update,student.update,parent.update" {
		t.Errorf("audit = %s", got)
	}
}

// R5: запись на месячный тариф в текущем месяце — выбор за первый месяц.
func TestEnrollMonthlyFirstMonthR5(t *testing.T) {
	c := newCatalog(t, "+996702200001")
	period := c.today.FirstOfMonth()
	due := c.today.AddDays(5) // due_days по умолчанию — 5 (R4)
	nextMonth := c.today.FirstOfMonth().AddMonths(1)

	tests := []struct {
		name       string
		discount   *discountJSON
		body       map[string]any
		wantCharge *chargeResp // nil — начисления нет
		comment    bool
	}{
		{"full", nil, map[string]any{"first_month": "full"},
			&chargeResp{Amount: -300_000, ListPrice: 300_000, Discount: 0}, false},
		{"full со скидкой fixed", &discountJSON{"fixed", 50_000}, map[string]any{"first_month": "full"},
			&chargeResp{Amount: -250_000, ListPrice: 300_000, Discount: 50_000}, false},
		{"full со скидкой percent", &discountJSON{"percent", 15}, map[string]any{"first_month": "full"},
			&chargeResp{Amount: -255_000, ListPrice: 300_000, Discount: 45_000}, false},
		{"manual", &discountJSON{"percent", 50}, map[string]any{"first_month": "manual", "manual_amount": 150_000},
			&chargeResp{Amount: -150_000, ListPrice: 150_000, Discount: 0}, true}, // скидка к ручной сумме не применяется
		{"manual 0", nil, map[string]any{"first_month": "manual", "manual_amount": 0},
			&chargeResp{Amount: 0, ListPrice: 0, Discount: 0}, true},
		{"none", nil, map[string]any{"first_month": "none"}, nil, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := c.student(t, "Ученик "+tt.name, "+99670220010"+string(rune('0'+i)), tt.discount)
			body := map[string]any{"group_id": c.group.ID, "start_date": c.today.String()}
			for k, v := range tt.body {
				body[k] = v
			}
			res := c.enroll(t, s.ID, body)
			if res.Enrollment.Tariff.ID != c.monthly.ID || !res.Enrollment.Active || res.Enrollment.LessonsLeft != nil ||
				res.Enrollment.StartDate != c.today || res.Enrollment.BilledThrough == nil || *res.Enrollment.BilledThrough != period {
				t.Errorf("enrollment = %+v", res.Enrollment)
			}
			ledger := ledgerOf(t, c.api, s.ID)
			if tt.wantCharge == nil {
				if res.Charge != nil || len(ledger) != 0 {
					t.Fatalf("начисление при none: %+v %+v", res.Charge, ledger)
				}
				return
			}
			ch := res.Charge
			if ch == nil || ch.Amount != tt.wantCharge.Amount || ch.ListPrice != tt.wantCharge.ListPrice ||
				ch.Discount != tt.wantCharge.Discount || ch.Period == nil || *ch.Period != period || ch.DueDate != due {
				t.Fatalf("charge = %+v, want %+v period=%s due=%s", ch, tt.wantCharge, period, due)
			}
			if len(ledger) != 1 || ledger[0].Kind != "charge" || ledger[0].Amount != ch.Amount ||
				*ledger[0].ListPrice != ch.ListPrice || *ledger[0].Discount != ch.Discount ||
				*ledger[0].Period != period || *ledger[0].DueDate != due || (ledger[0].Comment != nil) != tt.comment {
				t.Fatalf("ledger = %+v", ledger)
			}
			if card := c.card(t, s.ID); card.Balance != ch.Amount {
				t.Errorf("balance = %d, want %d", card.Balance, ch.Amount)
			}
		})
	}

	t.Run("будущий месяц — без начисления", func(t *testing.T) {
		s := c.student(t, "Со следующего месяца", "+996702200201", nil)
		res := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": nextMonth.String(), "first_month": "full"})
		if res.Charge != nil || len(ledgerOf(t, c.api, s.ID)) != 0 || res.Enrollment.BilledThrough != nil {
			t.Fatalf("начисление за будущий месяц: %+v", res.Charge)
		}
		// first_month для будущего месяца не обязателен.
		s2 := c.student(t, "Без выбора", "+996702200202", nil)
		if res := c.enroll(t, s2.ID, map[string]any{"group_id": c.group.ID, "start_date": nextMonth.AddDays(3).String()}); res.Charge != nil {
			t.Fatal("начисление за будущий месяц")
		}
	})

	t.Run("ошибки выбора", func(t *testing.T) {
		s := c.student(t, "Ошибки", "+996702200301", nil)
		p := centerPath(c.center, "/students/"+s.ID.String()+"/enrollments")
		start := c.today.String()
		cases := []struct {
			body  map[string]any
			field string
		}{
			{map[string]any{"group_id": c.group.ID, "start_date": start}, "first_month"},
			{map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "half"}, "first_month"},
			{map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "manual"}, "manual_amount"},
			{map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "manual", "manual_amount": -1}, "manual_amount"},
			{map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "full", "manual_amount": 100}, "manual_amount"},
			{map[string]any{"group_id": c.group.ID, "first_month": "full"}, "start_date"},
			{map[string]any{"start_date": start, "first_month": "full"}, "group_id"},
		}
		for _, cs := range cases {
			assertField(t, c.owner.do(http.MethodPost, p, cs.body), http.StatusBadRequest, cs.field)
		}
		if card := c.card(t, s.ID); len(card.Enrollments) != 0 || len(ledgerOf(t, c.api, s.ID)) != 0 {
			t.Errorf("после ошибок остались записи: %+v", card.Enrollments)
		}
	})

	t.Run("fixed-скидка больше цены — начисление на 0 с пометкой discount_capped", func(t *testing.T) {
		s := c.student(t, "Большая скидка", "+996702200401", &discountJSON{"fixed", 300_001})
		res := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
		if ch := res.Charge; ch == nil || ch.Amount != 0 || ch.ListPrice != 300_000 || ch.Discount != 300_000 {
			t.Fatalf("charge = %+v, want 0 (list 300000, discount 300000)", ch)
		}
		if card := c.card(t, s.ID); len(card.Enrollments) != 1 || card.Balance != 0 ||
			card.Enrollments[0].BilledThrough == nil || *card.Enrollments[0].BilledThrough != period {
			t.Errorf("card = %+v", card)
		}
		capped := 0
		for _, a := range filterAudit(auditLog(t, c.api, c.center), "ledger.charge") {
			if a.After["discount_capped"] == true {
				capped++
				if *a.EntityID != res.Charge.ID {
					t.Errorf("discount_capped у чужого начисления: %+v", a)
				}
			}
		}
		if capped != 1 {
			t.Errorf("пометок discount_capped: %d, want 1", capped)
		}
	})

	log := auditLog(t, c.api, c.center)
	if n := len(filterAudit(log, "ledger.charge")); n != 6 {
		t.Errorf("ledger.charge в журнале: %d, want 6", n)
	}
	if n := len(filterAudit(log, "enrollment.create")); n != 9 {
		t.Errorf("enrollment.create в журнале: %d, want 9", n)
	}
}

// Перенос из Excel: ученик учится давно. Прошлые месяцы не начисляются никогда, текущий — по first_month;
// billed_through = текущий период при любом выборе. Срок оплаты — как у всех: billing_day + due_days (R4).
func TestEnrollPastStart(t *testing.T) {
	c := newCatalog(t, "+996703000001")
	ctx := context.Background()
	if _, err := c.api.pool.Exec(ctx, `UPDATE centers SET billing_day = 10 WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	period := c.today.FirstOfMonth()
	dueR4 := period.AddDays(10 - 1 + 5) // 15-е число текущего месяца
	longAgo := c.today.AddMonths(-26)

	tests := []struct {
		name        string
		start       civil.Date
		firstMonth  map[string]any
		wantAmount  *int64
		wantComment bool
	}{
		{"2 года назад, full", longAgo, map[string]any{"first_month": "full"}, ptrInt(-300_000), false},
		{"2 года назад, manual", longAgo, map[string]any{"first_month": "manual", "manual_amount": 120_000}, ptrInt(-120_000), true},
		{"2 года назад, none", longAgo, map[string]any{"first_month": "none"}, nil, false},
		{"прошлый месяц, full", period.AddDays(-1), map[string]any{"first_month": "full"}, ptrInt(-300_000), false},
		{"ровно 5 лет назад", c.today.AddMonths(-60), map[string]any{"first_month": "none"}, nil, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := c.student(t, "Давний "+tt.name, "+99670300010"+string(rune('0'+i)), nil)
			body := map[string]any{"group_id": c.group.ID, "start_date": tt.start.String()}
			for k, v := range tt.firstMonth {
				body[k] = v
			}
			res := c.enroll(t, s.ID, body)
			if res.Enrollment.StartDate != tt.start || res.Enrollment.BilledThrough == nil || *res.Enrollment.BilledThrough != period {
				t.Fatalf("enrollment = %+v, want billed_through %s", res.Enrollment, period)
			}
			ledger := ledgerOf(t, c.api, s.ID)
			if tt.wantAmount == nil {
				if res.Charge != nil || len(ledger) != 0 {
					t.Fatalf("начисление при none: %+v", ledger)
				}
				return
			}
			// Ровно одно начисление — за текущий период; прошлые месяцы не начисляются.
			if len(ledger) != 1 || ledger[0].Amount != *tt.wantAmount || *ledger[0].Period != period ||
				*ledger[0].DueDate != dueR4 || (ledger[0].Comment != nil) != tt.wantComment {
				t.Fatalf("ledger = %+v, want %d за %s со сроком %s", ledger, *tt.wantAmount, period, dueR4)
			}
		})
	}

	s := c.student(t, "Опечатка", "+996703000201", nil)
	p := centerPath(c.center, "/students/"+s.ID.String()+"/enrollments")
	assertField(t, c.owner.do(http.MethodPost, p, map[string]any{"group_id": c.group.ID,
		"start_date": c.today.AddMonths(-60).AddDays(-1).String(), "first_month": "none"}), http.StatusUnprocessableEntity, "start_date")
	assertField(t, c.owner.do(http.MethodPost, p, map[string]any{"group_id": c.group.ID, "start_date": "1990-09-01", "first_month": "none"}),
		http.StatusUnprocessableEntity, "start_date")
	// Давно учится — first_month обязателен, как и в текущем месяце.
	assertField(t, c.owner.do(http.MethodPost, p, map[string]any{"group_id": c.group.ID, "start_date": longAgo.String()}),
		http.StatusBadRequest, "first_month")

	// В БД то же, что в ответе.
	var n int
	if err := c.api.pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE center_id = $1 AND billed_through = $2`, c.center, period).Scan(&n); err != nil || n != len(tests) {
		t.Errorf("billed_through в БД у %d записей, want %d (%v)", n, len(tests), err)
	}
}

func ptrInt(v int64) *int64 { return &v }

// R8: абонемент начисляется сразу; продление прибавляет занятия, минус вычитается (R10).
func TestEnrollPackageAndRenew(t *testing.T) {
	c := newCatalog(t, "+996702300001")
	s := c.student(t, "Абонементщик", "+996702300101", &discountJSON{"percent", 10})

	res := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": c.today.AddMonths(1).String(),
		"first_month": "none"}) // first_month игнорируется, будущая дата — тоже
	if res.Enrollment.Tariff.Kind != "package" || res.Enrollment.LessonsLeft == nil || *res.Enrollment.LessonsLeft != 8 ||
		res.Enrollment.BilledThrough != nil {
		t.Fatalf("enrollment = %+v", res.Enrollment)
	}
	if ch := res.Charge; ch == nil || ch.Amount != -360_000 || ch.ListPrice != 400_000 || ch.Discount != 40_000 ||
		ch.Period != nil || ch.DueDate != c.today.AddDays(5) {
		t.Fatalf("charge = %+v", ch)
	}
	eid := res.Enrollment.ID
	renew := centerPath(c.center, "/students/"+s.ID.String()+"/enrollments/"+eid.String()+"/renew")

	// Отметки посещаемости (неделя 2) увели остаток в минус: -2.
	if _, err := c.api.pool.Exec(context.Background(), `UPDATE enrollments SET lessons_left = -2 WHERE id = $1`, eid); err != nil {
		t.Fatal(err)
	}
	// Цена изменилась — продление уже по новой (R6).
	c.owner.do(http.MethodPatch, centerPath(c.center, "/tariffs/"+c.pkg.ID.String()), map[string]any{"price": 500_000})

	rec := c.owner.do(http.MethodPost, renew, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("renew: %d %s", rec.Code, rec.Body)
	}
	r := decode[enrollResp](t, rec)
	if r.Enrollment.LessonsLeft == nil || *r.Enrollment.LessonsLeft != 6 {
		t.Errorf("lessons_left после продления = %v, want 6 (-2 + 8)", r.Enrollment.LessonsLeft)
	}
	if ch := r.Charge; ch == nil || ch.Amount != -450_000 || ch.ListPrice != 500_000 || ch.Discount != 50_000 || ch.DueDate != c.today.AddDays(5) {
		t.Fatalf("renew charge = %+v", ch)
	}
	if card := c.card(t, s.ID); card.Balance != -810_000 || card.Balance != dbBalance(t, c.api, s.ID) {
		t.Errorf("balance = %d, want -810000 (= ledger %d)", card.Balance, dbBalance(t, c.api, s.ID))
	}

	// Месячную запись продлить нельзя.
	m := c.enroll(t, s.ID, map[string]any{"group_id": createGroup(t, c.owner, c.center, map[string]any{"name": "Вторая"}).ID,
		"tariff_id": c.monthly.ID, "start_date": c.today.String(), "first_month": "none"})
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+s.ID.String()+"/enrollments/"+m.Enrollment.ID.String()+"/renew"), nil),
		http.StatusConflict, "not_package")

	// Завершённую — тоже.
	// Запись с будущим началом завершается (отменяется) датой начала.
	if rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+s.ID.String()+"/enrollments/"+eid.String()+"/end"), nil); rec.Code != http.StatusOK {
		t.Fatalf("end: %d %s", rec.Code, rec.Body)
	}
	assertError(t, c.owner.do(http.MethodPost, renew, nil), http.StatusConflict, "enrollment_ended")

	renews := filterAudit(auditLog(t, c.api, c.center), "enrollment.renew")
	if len(renews) != 1 || renews[0].Before["lessons_left"] != float64(-2) || renews[0].After["lessons_left"] != float64(6) {
		t.Errorf("enrollment.renew = %+v", renews)
	}
}

func TestEnrollReferences(t *testing.T) {
	c := newCatalog(t, "+996702400001")
	s := c.student(t, "Ученик", "+996702400101", nil)
	path := centerPath(c.center, "/students/"+s.ID.String()+"/enrollments")
	start := c.today.String()

	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "none"})
	assertError(t, c.owner.do(http.MethodPost, path, map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "none"}),
		http.StatusConflict, "already_enrolled")

	noDefault := createGroup(t, c.owner, c.center, map[string]any{"name": "Без тарифа"})
	archivedGroup := createGroup(t, c.owner, c.center, map[string]any{"name": "Архивная", "default_tariff_id": c.monthly.ID})
	c.owner.do(http.MethodDelete, centerPath(c.center, "/groups/"+archivedGroup.ID.String()), nil)
	archivedTariff := createTariff(t, c.owner, c.center, monthly("Старый", 100_000))
	withArchivedDefault := createGroup(t, c.owner, c.center, map[string]any{"name": "Тариф ушёл в архив", "default_tariff_id": archivedTariff.ID})
	c.owner.do(http.MethodDelete, centerPath(c.center, "/tariffs/"+archivedTariff.ID.String()), nil)

	other := newCatalog(t, "+996702400002")

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"архивная группа", map[string]any{"group_id": archivedGroup.ID}, "group_id"},
		{"чужая группа", map[string]any{"group_id": other.group.ID}, "group_id"},
		{"несуществующая группа", map[string]any{"group_id": uuid.New()}, "group_id"},
		{"нет тарифа ни у группы, ни в запросе", map[string]any{"group_id": noDefault.ID}, "tariff_id"},
		{"архивный тариф", map[string]any{"group_id": noDefault.ID, "tariff_id": archivedTariff.ID}, "tariff_id"},
		{"архивный тариф по умолчанию", map[string]any{"group_id": withArchivedDefault.ID}, "tariff_id"},
		{"чужой тариф", map[string]any{"group_id": noDefault.ID, "tariff_id": other.monthly.ID}, "tariff_id"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.body["start_date"], tt.body["first_month"] = start, "full"
			assertField(t, c.owner.do(http.MethodPost, path, tt.body), http.StatusUnprocessableEntity, tt.field)
		})
	}

	// Явный тариф важнее тарифа группы.
	res := c.enroll(t, s.ID, map[string]any{"group_id": noDefault.ID, "tariff_id": c.pkg.ID, "start_date": start})
	if res.Enrollment.Tariff.ID != c.pkg.ID {
		t.Errorf("tariff = %v, want package", res.Enrollment.Tariff.ID)
	}

	// Ушедшего ученика не записываем.
	gone := c.student(t, "Ушёл", "+996702400102", nil)
	c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+gone.ID.String()), map[string]any{"status": "left"})
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+gone.ID.String()+"/enrollments"),
		map[string]any{"group_id": c.group.ID, "start_date": start, "first_month": "full"}), http.StatusConflict, "student_left")

	if card := c.card(t, s.ID); len(card.Enrollments) != 2 {
		t.Errorf("записей %d, want 2", len(card.Enrollments))
	}
}

// R7: «ушёл» закрывает все активные записи сегодняшним днём; «пауза» — не трогает.
func TestStatusLeftClosesEnrollments(t *testing.T) {
	c := newCatalog(t, "+996702500001")
	s := c.student(t, "Уходит", "+996702500101", nil)
	g2 := createGroup(t, c.owner, c.center, map[string]any{"name": "Вторая", "default_tariff_id": c.monthly.ID})
	g3 := createGroup(t, c.owner, c.center, map[string]any{"name": "Третья", "default_tariff_id": c.monthly.ID})
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	future := c.today.FirstOfMonth().AddMonths(1).AddDays(9)
	c.enroll(t, s.ID, map[string]any{"group_id": g2.ID, "start_date": future.String()})
	old := c.enroll(t, s.ID, map[string]any{"group_id": g3.ID, "start_date": c.today.FirstOfMonth().String(), "first_month": "none"})
	path := centerPath(c.center, "/students/"+s.ID.String())

	// Завершение: не раньше начала, не позже сегодня; по умолчанию — сегодня; повторно — 409.
	endPath := path + "/enrollments/" + old.Enrollment.ID.String() + "/end"
	assertField(t, c.owner.do(http.MethodPost, endPath, map[string]any{"end_date": c.today.AddDays(1).String()}), http.StatusBadRequest, "end_date")
	assertField(t, c.owner.do(http.MethodPost, endPath, map[string]any{"end_date": c.today.FirstOfMonth().AddDays(-1).String()}), http.StatusBadRequest, "end_date")
	rec := c.owner.do(http.MethodPost, endPath, nil)
	if e := decode[enrollmentResp](t, rec); rec.Code != http.StatusOK || e.Active || e.EndDate == nil || *e.EndDate != c.today {
		t.Fatalf("end: %d %+v", rec.Code, e)
	}
	assertError(t, c.owner.do(http.MethodPost, endPath, nil), http.StatusConflict, "enrollment_ended")

	// Пауза — записи живы.
	card := decode[studentResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"status": "paused"}))
	if card.Status != "paused" || !card.Enrollments[0].Active || !card.Enrollments[1].Active {
		t.Fatalf("после паузы: %+v", card.Enrollments)
	}

	// Ушёл — обе активные закрыты; запись с будущим началом закрыта датой начала.
	card = decode[studentResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"status": "left"}))
	if card.Status != "left" {
		t.Fatalf("status = %s", card.Status)
	}
	ends := map[uuid.UUID]civil.Date{}
	for _, e := range card.Enrollments {
		if e.Active || e.EndDate == nil {
			t.Fatalf("запись не закрыта: %+v", e)
		}
		ends[e.Group.ID] = *e.EndDate
	}
	if ends[c.group.ID] != c.today || ends[g2.ID] != future || ends[g3.ID] != c.today {
		t.Errorf("end_date = %v", ends)
	}
	// Долг остаётся (R7): начисление первого месяца не трогаем.
	if card.Balance != -300_000 {
		t.Errorf("balance = %d", card.Balance)
	}

	ended := filterAudit(auditLog(t, c.api, c.center), "enrollment.end")
	if len(ended) != 3 || ended[1].After["reason"] != "student.left" || ended[2].After["reason"] != "student.left" {
		t.Errorf("enrollment.end = %+v", ended)
	}
}

func TestStudentListFiltersBalanceAndPaging(t *testing.T) {
	c := newCatalog(t, "+996702600001")
	ctx := context.Background()
	g2 := createGroup(t, c.owner, c.center, map[string]any{"name": "Шахматы", "default_tariff_id": c.monthly.ID})

	ayana := c.student(t, "Айгерим Токтосунова", "0700 333 111", nil)
	c.enroll(t, ayana.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	c.enroll(t, ayana.ID, map[string]any{"group_id": g2.ID, "start_date": c.today.String(), "first_month": "manual", "manual_amount": 100_000})
	bek := c.student(t, "Бекзат 50% скидка", "0555 444 222", &discountJSON{"percent", 50})
	c.enroll(t, bek.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	c.student(t, "Бектур", "0777 555 333", nil)
	c.student(t, "Данияр", "0700 333 999", nil)
	c.student(t, "Эрмек", "0222 000 111", nil)

	// Отменённое начисление в баланс не входит (R15).
	if _, err := c.api.pool.Exec(ctx, `UPDATE ledger SET cancelled_at = now(), cancel_reason = 'тест'
		WHERE student_id = $1 AND list_price = 100000`, ayana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.pool.Exec(ctx, `UPDATE parents SET tg_status = 'active' WHERE id = $1`, bek.Parent.ID); err != nil {
		t.Fatal(err)
	}

	list := func(t *testing.T, query string) studentListResp {
		t.Helper()
		rec := c.owner.do(http.MethodGet, centerPath(c.center, "/students?"+query), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list ?%s: %d %s", query, rec.Code, rec.Body)
		}
		return decode[studentListResp](t, rec)
	}
	names := func(r studentListResp) string {
		var n []string
		for _, it := range r.Items {
			n = append(n, it.FullName)
		}
		return strings.Join(n, "|")
	}

	all := list(t, "")
	if names(all) != "Айгерим Токтосунова|Бектур|Бекзат 50% скидка|Данияр|Эрмек" && names(all) != "Айгерим Токтосунова|Бекзат 50% скидка|Бектур|Данияр|Эрмек" {
		t.Fatalf("порядок = %s", names(all))
	}
	for _, it := range all.Items {
		if card := c.card(t, it.ID); it.Balance != card.Balance || it.Balance != dbBalance(t, c.api, it.ID) {
			t.Errorf("%s: balance в списке %d, в карточке %d, в ledger %d", it.FullName, it.Balance, card.Balance, dbBalance(t, c.api, it.ID))
		}
		if it.FullName == "Айгерим Токтосунова" && (it.Balance != -300_000 || len(it.Groups) != 2 || it.Groups[0].Name != "Английский A1") {
			t.Errorf("Айгерим: %+v", it)
		}
		if it.FullName == "Бекзат 50% скидка" && it.Balance != -150_000 {
			t.Errorf("Бекзат: %+v", it)
		}
	}

	filters := []struct {
		query, want string
	}{
		{"q=" + url.QueryEscape("бек"), "Бекзат 50% скидка|Бектур"},               // регистр не важен
		{"q=" + url.QueryEscape("ТОКТОСУН"), "Айгерим Токтосунова"},               //
		{"q=" + url.QueryEscape("0700 333"), "Айгерим Токтосунова|Данияр"},        // телефон родителя
		{"q=" + url.QueryEscape("+996 555 444"), "Бекзат 50% скидка"},             //
		{"q=" + url.QueryEscape("50%"), "Бекзат 50% скидка"},                      // % — буквально
		{"q=" + url.QueryEscape("_"), ""},                                         // _ — буквально
		{"group=" + c.group.ID.String(), "Айгерим Токтосунова|Бекзат 50% скидка"}, //
		{"group=" + g2.ID.String(), "Айгерим Токтосунова"},                        //
		{"tg=active", "Бекзат 50% скидка"},                                        //
		{"tg=none&q=" + url.QueryEscape("бек"), "Бектур"},                         //
		{"status=left", ""}, //
	}
	for _, f := range filters {
		t.Run(f.query, func(t *testing.T) {
			got := names(list(t, f.query))
			if f.want == "Бекзат 50% скидка|Бектур" && got == "Бектур|Бекзат 50% скидка" {
				return // порядок «Бекзат»/«Бектур» зависит от правил сортировки БД
			}
			if got != f.want {
				t.Errorf("?%s = %q, want %q", f.query, got, f.want)
			}
		})
	}

	// Пагинация: страницы по 2 складываются в полный список, на последней next_cursor = null.
	var paged []string
	cursor := ""
	for page := 0; ; page++ {
		q := "limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		r := list(t, q)
		if len(r.Items) > 2 || page > 3 {
			t.Fatalf("страница %d: %d строк", page, len(r.Items))
		}
		for _, it := range r.Items {
			paged = append(paged, it.FullName)
		}
		if r.NextCursor == nil {
			break
		}
		cursor = *r.NextCursor
	}
	if strings.Join(paged, "|") != names(all) {
		t.Errorf("постранично %v != %s", paged, names(all))
	}

	for _, bad := range []struct{ query, field string }{
		{"limit=0", "limit"}, {"limit=201", "limit"}, {"limit=abc", "limit"},
		{"cursor=garbage", "cursor"}, {"status=gone", "status"}, {"tg=yes", "tg"}, {"group=xyz", "group"},
	} {
		assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/students?"+bad.query), nil), http.StatusBadRequest, bad.field)
	}
}

// «Сегодня» — по часам центра: в 20:00 UTC в Бишкеке уже следующий день.
func TestTodayInCenterTimezone(t *testing.T) {
	c := newCatalog(t, "+996702700001")
	now := c.api.clock.Now().UTC()
	evening := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, time.UTC)
	c.api.clock.Advance(evening.Sub(c.api.clock.Now()))
	utcDate := civil.DateOf(evening)
	bishkek := utcDate.AddDays(1)

	s := c.student(t, "Вечерний", "+996702700101", nil)
	res := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": bishkek.String()})
	if res.Charge == nil || res.Charge.DueDate != bishkek.AddDays(5) {
		t.Fatalf("due_date = %v, want %s (сегодня по Бишкеку + 5)", res.Charge, bishkek.AddDays(5))
	}
	// Завершение «сегодня» — это бишкекская дата, а не UTC (иначе было бы раньше начала).
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+s.ID.String()+"/enrollments/"+res.Enrollment.ID.String()+"/end"), nil)
	if e := decode[enrollmentResp](t, rec); rec.Code != http.StatusOK || *e.EndDate != bishkek {
		t.Fatalf("end: %d %s", rec.Code, rec.Body)
	}
}

func TestStudentsTeacherForbidden(t *testing.T) {
	c := newCatalog(t, "+996702800001")
	s := c.student(t, "Ученик", "+996702800101", nil)
	e := c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": c.today.String()})
	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996702800002")

	for _, ep := range studentEndpoints(s.ID, e.Enrollment.ID, c.group.ID, c.today) {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			assertError(t, teacher.do(ep.method, centerPath(c.center, ep.path), ep.body), http.StatusForbidden, "forbidden")
		})
	}
	if card := c.card(t, s.ID); card.Status != "active" || len(card.Enrollments) != 1 || !card.Enrollments[0].Active {
		t.Errorf("учитель что-то изменил: %+v", card)
	}
}

// Тест изоляции: каждый эндпоинт /students… для чужого центра — 404; id из А через путь Б — 404;
// группы и тарифы А нельзя использовать при записи в Б.
func TestStudentsIsolation(t *testing.T) {
	a := newCatalog(t, "+996702900001")
	s := a.student(t, "Ученик А", "+996702900101", nil)
	e := a.enroll(t, s.ID, map[string]any{"group_id": a.group.ID, "tariff_id": a.pkg.ID, "start_date": a.today.String()})

	// Центр Б в той же БД: владелец Б регистрируется через API того же сервера.
	ownerB := a.api.client(t, "10.0.0.2")
	centerB := ownerB.register("+996702900002", "Центр Б").Centers[0].ID
	sB := decode[studentResp](t, ownerB.do(http.MethodPost, centerPath(centerB, "/students"),
		map[string]any{"full_name": "Ученик Б", "parent": map[string]string{"name": "Р", "phone": "+996702900102"}}))

	for _, ep := range studentEndpoints(s.ID, e.Enrollment.ID, a.group.ID, a.today) {
		t.Run("чужой центр "+ep.method+" "+ep.path, func(t *testing.T) {
			assertError(t, ownerB.do(ep.method, centerPath(a.center, ep.path), ep.body), http.StatusNotFound, "not_found")
		})
		if ep.withID {
			t.Run("id из А через Б "+ep.method+" "+ep.path, func(t *testing.T) {
				assertError(t, ownerB.do(ep.method, centerPath(centerB, ep.path), ep.body), http.StatusNotFound, "not_found")
			})
		}
	}
	// Запись своего ученика Б в группу/на тариф А — 422.
	assertField(t, ownerB.do(http.MethodPost, centerPath(centerB, "/students/"+sB.ID.String()+"/enrollments"),
		map[string]any{"group_id": a.group.ID, "start_date": a.today.String(), "first_month": "full"}), http.StatusUnprocessableEntity, "group_id")
	// Запись ученика Б на чужую запись А — продление по id записи А через ученика Б.
	assertError(t, ownerB.do(http.MethodPost, centerPath(centerB, "/students/"+sB.ID.String()+"/enrollments/"+e.Enrollment.ID.String()+"/renew"), nil),
		http.StatusNotFound, "not_found")

	// Поиск в Б не находит учеников А ни по имени, ни по телефону родителя.
	if r := decode[studentListResp](t, ownerB.do(http.MethodGet, centerPath(centerB, "/students?q="+url.QueryEscape("Ученик")), nil)); len(r.Items) != 1 || r.Items[0].ID != sB.ID {
		t.Errorf("поиск в Б: %+v", r.Items)
	}
	if r := decode[studentListResp](t, ownerB.do(http.MethodGet, centerPath(centerB, "/students?q=702900101"), nil)); len(r.Items) != 0 {
		t.Errorf("поиск в Б по телефону родителя А: %+v", r.Items)
	}

	// В А ничего не изменилось.
	card := a.card(t, s.ID)
	if card.FullName != "Ученик А" || card.Status != "active" || len(card.Enrollments) != 1 || !card.Enrollments[0].Active ||
		*card.Enrollments[0].LessonsLeft != 8 || card.Balance != -400_000 {
		t.Errorf("А после атаки: %+v", card)
	}
}

type endpoint struct {
	method, path string
	body         any
	withID       bool
}

func studentEndpoints(sid, eid, groupID uuid.UUID, today civil.Date) []endpoint {
	sp := "/students/" + sid.String()
	return []endpoint{
		{http.MethodGet, "/students", nil, false},
		{http.MethodPost, "/students", map[string]any{"full_name": "Взлом", "parent": map[string]string{"name": "x", "phone": "0700000000"}}, false},
		{http.MethodGet, sp, nil, true},
		{http.MethodPatch, sp, map[string]any{"status": "left"}, true},
		{http.MethodPost, sp + "/enrollments", map[string]any{"group_id": groupID, "start_date": today.String(), "first_month": "full"}, true},
		{http.MethodPost, sp + "/enrollments/" + eid.String() + "/end", nil, true},
		{http.MethodPost, sp + "/enrollments/" + eid.String() + "/renew", nil, true},
	}
}

func filterAudit(rows []auditRow, prefixes ...string) []auditRow {
	var out []auditRow
	for _, r := range rows {
		for _, p := range prefixes {
			if strings.HasPrefix(r.Action, p) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}
