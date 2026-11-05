package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/billing"
	"okuu/internal/civil"
)

type settingsResp struct {
	Name               string      `json:"name"`
	Phone              *string     `json:"phone"`
	Timezone           string      `json:"timezone"`
	BillingDay         int         `json:"billing_day"`
	BillingDayNext     *int        `json:"billing_day_next"`
	BillingDayNextFrom *civil.Date `json:"billing_day_next_from"`
	DueDays            int         `json:"due_days"`
	ExcusedDeducts     bool        `json:"excused_deducts"`
	RemindersOn        bool        `json:"reminders_on"`
	PayDetails         *string     `json:"pay_details"`
	Plan               string      `json:"plan"`
	FinikConnected     bool        `json:"finik_connected"`
}

func TestSettings(t *testing.T) {
	c := newCatalog(t, "+996708200001")
	admin, _ := addStaff(t, c.api, c.owner, c.center, "admin", "+996708200002")
	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996708200003")
	path := centerPath(c.center, "/settings")
	ctx := context.Background()

	// Ключи Finik наружу не отдаются — только флаг.
	if _, err := c.api.pool.Exec(ctx, `UPDATE centers SET finik_creds = '\xdeadbeef' WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	rec := admin.do(http.MethodGet, path, nil)
	s := decode[settingsResp](t, rec)
	if rec.Code != http.StatusOK || s.BillingDay != 1 || s.DueDays != 5 || s.Timezone != "Asia/Bishkek" || s.ExcusedDeducts ||
		!s.RemindersOn || s.Phone == nil || *s.Phone != "+996708200001" || s.Plan != "free" || !s.FinikConnected || s.BillingDayNext != nil {
		t.Fatalf("настройки по умолчанию: %d %+v", rec.Code, s)
	}
	if strings.Contains(rec.Body.String(), "finik_creds") || strings.Contains(rec.Body.String(), "deadbeef") {
		t.Errorf("ключи Finik в ответе: %s", rec.Body)
	}

	// Права: admin читает, но не меняет; teacher — ни то ни другое.
	assertError(t, admin.do(http.MethodPatch, path, map[string]any{"name": "x"}), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodGet, path, nil), http.StatusForbidden, "forbidden")

	rec = c.owner.do(http.MethodPatch, path, map[string]any{
		"name": "  Учебный центр «Билим»  ", "phone": "0555 111 222", "due_days": 10, "excused_deducts": true,
		"reminders_on": false, "pay_details": "MBank: 0700 123 456 (Асанова А.)", "timezone": "Asia/Almaty",
	})
	s = decode[settingsResp](t, rec)
	if rec.Code != http.StatusOK || s.Name != "Учебный центр «Билим»" || *s.Phone != "+996555111222" || s.DueDays != 10 ||
		!s.ExcusedDeducts || s.RemindersOn || *s.PayDetails != "MBank: 0700 123 456 (Асанова А.)" || s.Timezone != "Asia/Almaty" {
		t.Fatalf("patch: %d %+v", rec.Code, s)
	}
	// null очищает телефон и реквизиты; повтор тех же значений — без записи в журнал.
	s = decode[settingsResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"phone": nil, "pay_details": nil, "timezone": "Asia/Bishkek"}))
	if s.Phone != nil || s.PayDetails != nil {
		t.Errorf("null не очистил: %+v", s)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"due_days": 10})

	invalid := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"name": " "}, "name"},
		{map[string]any{"name": nil}, "name"},
		{map[string]any{"phone": "123"}, "phone"},
		{map[string]any{"timezone": "Mars/Olympus"}, "timezone"},
		{map[string]any{"timezone": "Local"}, "timezone"},
		{map[string]any{"timezone": ""}, "timezone"},
		{map[string]any{"billing_day": 0}, "billing_day"},
		{map[string]any{"billing_day": 29}, "billing_day"},
		{map[string]any{"due_days": -1}, "due_days"},
		{map[string]any{"due_days": 61}, "due_days"},
		{map[string]any{"excused_deducts": nil}, "excused_deducts"},
		{map[string]any{"pay_details": strings.Repeat("я", 1001)}, "pay_details"},
	}
	for i, tt := range invalid {
		t.Run(strconv.Itoa(i)+" "+tt.field, func(t *testing.T) {
			assertField(t, c.owner.do(http.MethodPatch, path, tt.body), http.StatusBadRequest, tt.field)
		})
	}
	assertError(t, c.owner.do(http.MethodPatch, path, map[string]any{"finik_creds": "x"}), http.StatusBadRequest, "bad_request")

	updates := filterAudit(auditLog(t, c.api, c.center), "center.update")
	if len(updates) != 2 || updates[0].Before["due_days"] != float64(5) || updates[0].After["due_days"] != float64(10) ||
		updates[1].After["phone"] != nil {
		t.Errorf("center.update = %+v (повтор тех же значений не должен попадать в журнал)", updates)
	}
}

// Новый billing_day действует со следующего периода: текущий месяц — по старому дню.
func TestBillingDayChangesFromNextPeriod(t *testing.T) {
	c := newCatalog(t, "+996708300001")
	path := centerPath(c.center, "/settings")
	s := c.student(t, "Ученик", "+996708300101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})
	p1 := c.today.FirstOfMonth().AddMonths(1)

	got := decode[settingsResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"billing_day": 10}))
	if got.BillingDay != 1 || got.BillingDayNext == nil || *got.BillingDayNext != 10 || *got.BillingDayNextFrom != p1 {
		t.Fatalf("после смены: %+v, want действует 1, со %s — 10", got, p1)
	}
	// Вернули текущий — отложенное отменяется.
	if got := decode[settingsResp](t, c.owner.do(http.MethodPatch, path, map[string]any{"billing_day": 1})); got.BillingDayNext != nil {
		t.Fatalf("отложенное не отменилось: %+v", got)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"billing_day": 10})

	// Следующий месяц: до 10-го не начисляем, с 10-го — по новому дню, срок 10 + due_days − 1.
	for _, day := range []int{1, 9} {
		if res, _ := billing.RunMonthly(context.Background(), c.api.pool, nil, c.center, p1.AddDays(day-1)); res.Charged != 0 {
			t.Fatalf("%d-е число нового периода: %+v", day, res)
		}
	}
	if res, _ := billing.RunMonthly(context.Background(), c.api.pool, nil, c.center, p1.AddDays(9)); res.Charged != 1 {
		t.Fatalf("10-е число: %+v", res)
	}
	if ch := chargesFor(t, c, s.ID, p1); len(ch) != 1 || *ch[0].DueDate != p1.AddDays(9+5) {
		t.Errorf("начисление = %+v, want срок %s", ch, p1.AddDays(14))
	}

	// Когда период наступил, настройки показывают новый день действующим, а запись в БД обновляется при следующем PATCH.
	c.api.clock.Advance(time.Duration(c.today.DaysSince(p1)*-1+1) * 24 * time.Hour)
	got = decode[settingsResp](t, c.owner.do(http.MethodGet, path, nil))
	if got.BillingDay != 10 || got.BillingDayNext != nil {
		t.Errorf("в новом периоде: %+v, want billing_day 10 без отложенного", got)
	}
	c.owner.do(http.MethodPatch, path, map[string]any{"due_days": 7})
	var day int
	var next *int
	_ = c.api.pool.QueryRow(context.Background(), `SELECT billing_day, billing_day_next FROM centers WHERE id = $1`, c.center).Scan(&day, &next)
	if day != 10 || next != nil {
		t.Errorf("в БД billing_day=%d next=%v, want 10 и NULL", day, next)
	}
}

func TestEffectiveBillingDay(t *testing.T) {
	p := civil.New(2026, 11, 1)
	ten := int16(10)
	from := civil.New(2026, 11, 1)
	tests := []struct {
		period civil.Date
		next   *int16
		want   int
	}{
		{p.AddMonths(-1), &ten, 5}, // до from — старый
		{p, &ten, 10},              // с from — новый
		{p.AddMonths(3), &ten, 10},
		{p, nil, 5},
	}
	for _, tt := range tests {
		f := &from
		if tt.next == nil {
			f = nil
		}
		if got := billing.EffectiveBillingDay(5, tt.next, f, tt.period); got != tt.want {
			t.Errorf("EffectiveBillingDay(%s) = %d, want %d", tt.period, got, tt.want)
		}
	}
}

type auditItemResp struct {
	ID   int64     `json:"id"`
	At   time.Time `json:"at"`
	User *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"user"`
	UserName string         `json:"user_name"`
	Action   string         `json:"action"`
	Entity   string         `json:"entity"`
	EntityID *uuid.UUID     `json:"entity_id"`
	Before   map[string]any `json:"before"`
	After    map[string]any `json:"after"`
}

func TestAuditLogEndpoint(t *testing.T) {
	c := newCatalog(t, "+996708400001") // владелец уже создал 2 тарифа и группу
	admin, adminID := addStaff(t, c.api, c.owner, c.center, "admin", "+996708400002")
	createGroup(t, admin, c.center, map[string]any{"name": "От админа"})
	s := c.student(t, "Ученик", "+996708400101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})
	if _, err := billing.RunMonthly(context.Background(), c.api.pool, nil, c.center, c.today.FirstOfMonth().AddMonths(1)); err != nil {
		t.Fatal(err)
	}
	path := centerPath(c.center, "/audit")
	list := func(t *testing.T, query string) itemsResp[auditItemResp] {
		t.Helper()
		rec := c.owner.do(http.MethodGet, path+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("audit%s: %d %s", query, rec.Code, rec.Body)
		}
		return decode[itemsResp[auditItemResp]](t, rec)
	}

	all := *list(t, "?limit=200").Items
	if len(all) < 8 {
		t.Fatalf("записей %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID >= all[i-1].ID {
			t.Fatalf("не по убыванию: %d после %d", all[i].ID, all[i-1].ID)
		}
	}
	// Новее всех — системное начисление billing.monthly.
	if top := all[0]; top.Action != "ledger.charge" || top.User != nil || top.UserName != "Система" || top.After["amount"] != float64(-300_000) {
		t.Errorf("верхняя запись = %+v", top)
	}

	byAdmin := *list(t, "?user="+adminID.String()).Items
	// Админ: принял приглашение (invite.accept записан от его имени) и создал группу.
	if len(byAdmin) != 2 || byAdmin[0].Action != "group.create" || byAdmin[1].Action != "invite.accept" ||
		byAdmin[0].User.Name != "Сотрудник +996708400002" {
		t.Errorf("?user=admin = %+v", byAdmin)
	}
	if exact := *list(t, "?action=tariff.create").Items; len(exact) != 2 {
		t.Errorf("?action=tariff.create: %d, want 2", len(exact))
	}
	if prefix := *list(t, "?action=ledger.").Items; len(prefix) != 1 || prefix[0].Action != "ledger.charge" {
		t.Errorf("?action=ledger.: %+v", prefix)
	}
	if none := *list(t, "?action=tariff").Items; len(none) != 0 {
		t.Errorf("?action=tariff без точки — точное совпадение: %d", len(none))
	}
	today := c.today.String()
	if got := *list(t, "?from="+today+"&to="+today).Items; len(got) != len(all) {
		t.Errorf("сегодня: %d из %d", len(got), len(all))
	}
	if got := *list(t, "?from="+c.today.AddDays(1).String()).Items; len(got) != 0 {
		t.Errorf("с завтра: %d", len(got))
	}

	// Постранично по 3 — те же записи.
	var paged []int64
	cursor := ""
	for range 20 {
		q := "?limit=3"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		page := list(t, q)
		for _, it := range *page.Items {
			paged = append(paged, it.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(paged) != len(all) || paged[0] != all[0].ID || paged[len(paged)-1] != all[len(all)-1].ID {
		t.Errorf("постранично %d записей, want %d", len(paged), len(all))
	}

	for _, bad := range []struct{ q, field string }{
		{"?user=xyz", "user"}, {"?from=20.10.2026", "from"}, {"?to=x", "to"},
		{"?from=" + today + "&to=" + c.today.AddDays(-1).String(), "to"}, {"?limit=0", "limit"}, {"?cursor=abc", "cursor"},
	} {
		assertField(t, c.owner.do(http.MethodGet, path+bad.q, nil), http.StatusBadRequest, bad.field)
	}
	assertError(t, admin.do(http.MethodGet, path, nil), http.StatusForbidden, "forbidden")
}

type auditDetailResp struct {
	Action   string `json:"action"`
	Students []struct {
		ID       uuid.UUID `json:"id"`
		FullName string    `json:"full_name"`
	} `json:"students"`
	Amount *int64 `json:"amount"`
	Lesson *struct {
		Date      civil.Date `json:"date"`
		GroupName string     `json:"group_name"`
	} `json:"lesson"`
}

// Строка журнала: у ledger.* — ученик и сумма операции (со знаком), у attendance.* — занятие и отмеченные
// ученики по имени; у остальных действий — пустой список и null.
func TestAuditLogStudentAndAmount(t *testing.T) {
	c := newCatalog(t, "+996708410001")
	aidana := c.debtor(t, "Токтосунова Айдана", "+996708410101") // начисление −3 000
	sofia := c.debtor(t, "Асанова София", "+996708410102")
	paid := c.mustOp(t, aidana.ID, pay(150_000))
	if rec := c.cancel(t, c.owner, paid.ID, "ошибка кассира"); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "12:00"})
	mustMark(t, c, c.owner, l.ID, aidana.ID, "present", sofia.ID, "absent")

	rec := c.owner.do(http.MethodGet, centerPath(c.center, "/audit?limit=200"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body)
	}
	got := map[string][]auditDetailResp{}
	for _, it := range *decode[itemsResp[auditDetailResp]](t, rec).Items {
		got[it.Action] = append(got[it.Action], it)
	}
	one := func(action string) auditDetailResp {
		t.Helper()
		if len(got[action]) == 0 {
			t.Fatalf("нет записи %s", action)
		}
		return got[action][0] // новее всех
	}
	type want struct {
		names  string
		amount int64
	}
	for action, w := range map[string]want{
		"ledger.payment": {"Токтосунова Айдана", 150_000},
		"ledger.cancel":  {"Токтосунова Айдана", 150_000}, // сумма отменённой операции
	} {
		it := one(action)
		if len(it.Students) != 1 || it.Students[0].ID != aidana.ID || it.Students[0].FullName != w.names ||
			it.Amount == nil || *it.Amount != w.amount || it.Lesson != nil {
			t.Errorf("%s = %+v", action, it)
		}
	}
	charges := got["ledger.charge"]
	if len(charges) != 2 || charges[0].Students[0].ID != sofia.ID || charges[0].Amount == nil || *charges[0].Amount != -300_000 {
		t.Errorf("ledger.charge = %+v", charges)
	}
	m := one("attendance.mark")
	if len(m.Students) != 2 || m.Students[0].FullName != "Асанова София" || m.Students[1].FullName != "Токтосунова Айдана" ||
		m.Amount != nil || m.Lesson == nil || m.Lesson.Date != c.today || m.Lesson.GroupName != c.group.Name {
		t.Errorf("attendance.mark = %+v", m)
	}
	if s := one("student.create"); len(s.Students) != 0 || s.Amount != nil || s.Lesson != nil {
		t.Errorf("student.create = %+v", s)
	}
	if s := one("lesson.create"); s.Lesson != nil {
		t.Errorf("lesson.create — без занятия в строке: %+v", s)
	}
}
