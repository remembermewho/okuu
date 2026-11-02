package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
	"okuu/internal/httpapi"
	"okuu/internal/notify"
	"okuu/internal/parents"
)

func ddmmOf(d civil.Date) string { return fmt.Sprintf("%02d.%02d", d.Day(), int(d.Month())) }

// Напоминание показывает все долги детей родителя в центре — и просроченные, и ожидающие, — сначала
// просроченные. «Напомнить» по одному ученику — тоже все дети; лимит «раз в час» по-прежнему по ученику.
func TestRemindAllParentDebts(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708300001")
	c.patchSettings(t, map[string]any{"due_days": 1})
	sofia := c.debtor(t, "София Токтосунова", "0708 300 101") // срок завтра — завтра «срок сегодня»
	c.patchSettings(t, map[string]any{"due_days": 0})
	aidana := c.debtor(t, "Айдана Токтосунова", "0708 300 101") // срок сегодня — завтра просрочено на 1 день
	c.mustOp(t, aidana.ID, pay(150_000))
	const chat = 8300101
	linkDirect(t, env.api, aidana.Parent.ID, chat)
	env.api.clock.Advance(24 * time.Hour)

	want := "• Айдана Токтосунова — 1 500 сом, просрочено на 1 день\n• София Токтосунова — 3 000 сом, срок сегодня\nИтого: 4 500 сом"
	r := mustRemind(t, c, map[string]any{"student_ids": []uuid.UUID{aidana.ID}})
	if r.Sent != 1 || r.Messages != 1 || len(r.Skipped) != 0 {
		t.Fatalf("Айдана: %+v", r)
	}
	if m := env.tg.Last(chat); !strings.Contains(m.Text, want) {
		t.Errorf("напоминание по Айдане — оба ребёнка, итого:\n%s", m.Text)
	}
	// В student_ids — только тот, о ком напомнили: София не попала под лимит «раз в час».
	if rows := notificationsOf(t, env.api, aidana.Parent.ID); len(rows) != 1 || fmt.Sprint(rows[0].Students) != fmt.Sprint([]string{aidana.ID.String()}) {
		t.Errorf("notifications: %+v", rows)
	}

	// По Софии — тоже оба, просроченная Айдана первой.
	r = mustRemind(t, c, map[string]any{"student_ids": []uuid.UUID{sofia.ID}})
	if r.Sent != 1 {
		t.Fatalf("София: %+v", r)
	}
	if m := env.tg.Last(chat); !strings.Contains(m.Text, want) {
		t.Errorf("напоминание по Софии:\n%s", m.Text)
	}
	// Правила отправки не изменились: Айдане снова — только через час.
	r = mustRemind(t, c, map[string]any{"student_ids": []uuid.UUID{aidana.ID}})
	if r.Sent != 0 || fmt.Sprint(r.reasons()) != fmt.Sprint(map[uuid.UUID]string{aidana.ID: "rate_limited"}) {
		t.Errorf("повтор по Айдане: %+v", r)
	}
	if n := len(env.tg.Messages(chat)); n != 2 {
		t.Errorf("сообщений %d, want 2", n)
	}
}

// Автонапоминание: сработал срок одного ребёнка — в сообщении и второй, у которого срок ещё не наступил;
// просроченный — первым, хотя по алфавиту он второй.
func TestRemindersAutoAllDebtsOverdueFirst(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708400001")
	c.patchSettings(t, map[string]any{"due_days": 0})
	timur := c.debtor(t, "Тимур Жумабеков", "0708 400 101")
	c.patchSettings(t, map[string]any{"due_days": 5})
	c.debtor(t, "Арсен Жумабеков", "0708 400 101")
	const chat = 8400101
	linkDirect(t, env.api, timur.Parent.ID, chat)
	env.api.clock.Advance(3 * 24 * time.Hour) // Тимур: просрочено 3 дня (overdue_3); Арсен: срок через 2 дня

	if res := env.runAuto(t); res.Sent != 1 {
		t.Fatalf("авто: %+v", res)
	}
	m := one(t, env.tg.Messages(chat))
	want := "оплата просрочена.\n\n• Тимур Жумабеков — 3 000 сом, просрочено на 3 дня\n• Арсен Жумабеков — 3 000 сом, срок до " +
		ddmmOf(c.today.AddDays(5)) + "\nИтого: 6 000 сом"
	if !strings.Contains(m.Text, want) {
		t.Errorf("автонапоминание:\n%s\nwant:\n%s", m.Text, want)
	}
}

// «Оплата получена» за одного ребёнка при долге другого: «Ещё к оплате», итог; «долгов нет» — только
// когда не должен никто из детей родителя в центре.
func TestNotifyPaidOtherChildrenDebts(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708500001")
	aidana := c.debtor(t, "Айдана Токтосунова", "0708 500 101")
	sofia := c.debtor(t, "София Токтосунова", "0708 500 101")
	bekzhan := c.debtor(t, "Бекжан Токтосунов", "0708 500 101")
	c.debtor(t, "Чужой Должник", "0708 500 999") // другой родитель — не упоминается
	const chat = 8500101
	linkDirect(t, env.api, aidana.Parent.ID, chat)
	head := "🏫 Центр «" + c.centerName(t) + "»\n"

	steps := []struct {
		student uuid.UUID
		amount  int64
		want    string
	}{
		{aidana.ID, 300_000, "Оплата получена: 3 000 сом — Айдана Токтосунова. Спасибо!\nБаланс: 0 сом\n" +
			"Ещё к оплате:\n• Бекжан Токтосунов — 3 000 сом\n• София Токтосунова — 3 000 сом\nИтого к оплате: 6 000 сом"},
		{sofia.ID, 100_000, "Оплата получена: 1 000 сом — София Токтосунова. Спасибо!\nБаланс: долг 2 000 сом\n" +
			"Ещё к оплате: Бекжан Токтосунов — 3 000 сом\nИтого к оплате: 5 000 сом"},
		{bekzhan.ID, 300_000, "Оплата получена: 3 000 сом — Бекжан Токтосунов. Спасибо!\nБаланс: 0 сом\n" +
			"Ещё к оплате: София Токтосунова — 2 000 сом"},
		{sofia.ID, 200_000, "Оплата получена: 2 000 сом — София Токтосунова. Спасибо!\nБаланс: 0 сом, долгов нет"},
	}
	for i, st := range steps {
		c.mustOp(t, st.student, pay(st.amount))
		if n := env.dispatch(t); n != 1 {
			t.Fatalf("шаг %d: отправлено %d", i, n)
		}
		if m := env.tg.Last(chat); m.Text != head+st.want {
			t.Errorf("шаг %d:\n%s\nwant:\n%s", i, m.Text, head+st.want)
		}
	}
}

// «Посещения»: под счётчиками — даты месяца с отметкой; прошлый месяц и отменённые занятия не попадают.
func TestBotAttendanceDates(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708600001")
	s := c.student(t, "Айдана Токтосунова", "0708 600 101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.AddDays(-40).String(), "first_month": "none"})
	lesson := func(date civil.Date, start, status string) lessonResp {
		l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": date.String(), "start_time": start})
		mustMark(t, c, c.owner, l.ID, s.ID, status)
		return l
	}
	lesson(c.today.FirstOfMonth().AddDays(-1), "07:00", "present") // прошлый месяц
	lesson(c.today, "08:00", "present")
	lesson(c.today, "07:00", "excused")
	cancelled := lesson(c.today, "09:00", "absent")
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/lessons/"+cancelled.ID.String()), map[string]any{"cancelled": true}); rec.Code != http.StatusOK {
		t.Fatalf("отмена занятия: %d %s", rec.Code, rec.Body)
	}
	u := env.user(t, 8600101)
	linkDirect(t, env.api, s.Parent.ID, u.id)
	tx := notify.For("ru")

	m := one(t, u.send("/attendance"))
	d := ddmmOf(c.today)
	want := tx.AttendanceHeader(int(c.today.Month())) + "\n\n" + tx.AttendanceChild("Айдана Токтосунова", c.centerName(t), 1, 0, 1) +
		"\n" + d + " — по уважительной\n" + d + " — был"
	if m.Text != want {
		t.Errorf("посещения:\n%s\nwant:\n%s", m.Text, want)
	}
}

// Поиск родителя по телефону в форме ученика: найден — имя и дети (без ушедших); нет — parent: null;
// телефон в любом виде; кривой — 400; чужой центр не виден; преподавателю — нельзя.
func TestParentLookup(t *testing.T) {
	api := newTestAPI(t)
	c := newCatalogOn(t, api, "+996708800001")
	aidana := c.student(t, "Айдана Токтосунова", "0708 800 101", nil)
	left := c.student(t, "Ушедший Токтосунов", "0708 800 101", nil)
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+left.ID.String()), map[string]any{"status": "left"}); rec.Code != http.StatusOK {
		t.Fatalf("left: %d %s", rec.Code, rec.Body)
	}
	type lookupResp struct {
		Parent *struct {
			ID       uuid.UUID `json:"id"`
			Name     string    `json:"name"`
			Children []string  `json:"children"`
		} `json:"parent"`
	}
	lookup := func(as *client, center uuid.UUID, phone string) *httptest.ResponseRecorder {
		return as.do(http.MethodGet, centerPath(center, "/parents/lookup?phone="+url.QueryEscape(phone)), nil)
	}
	for _, phone := range []string{"0708 800 101", "+996708800101", "996 708 800 101"} {
		rec := lookup(c.owner, c.center, phone)
		r := decode[lookupResp](t, rec)
		if rec.Code != http.StatusOK || r.Parent == nil || r.Parent.ID != aidana.Parent.ID || r.Parent.Name != "Родитель Айдана Токтосунова" ||
			fmt.Sprint(r.Parent.Children) != "[Айдана Токтосунова]" {
			t.Errorf("%s: %d %s", phone, rec.Code, rec.Body)
		}
	}
	if rec := lookup(c.owner, c.center, "0708 800 199"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"parent":null}` {
		t.Errorf("новый номер: %d %s", rec.Code, rec.Body)
	}
	assertField(t, lookup(c.owner, c.center, "12"), http.StatusBadRequest, "phone")

	// Другой центр с тем же телефоном родителя не видит.
	other := newCatalogOn(t, api, "+996708800002")
	if rec := lookup(other.owner, other.center, "0708 800 101"); strings.TrimSpace(rec.Body.String()) != `{"parent":null}` {
		t.Errorf("чужой центр: %d %s", rec.Code, rec.Body)
	}
	admin, _ := addStaff(t, api, c.owner, c.center, "admin", "+996708800003")
	if rec := lookup(admin, c.center, "0708 800 101"); rec.Code != http.StatusOK {
		t.Errorf("админ: %d %s", rec.Code, rec.Body)
	}
	teacher, _ := addStaff(t, api, c.owner, c.center, "teacher", "+996708800004")
	assertError(t, lookup(teacher, c.center, "0708 800 101"), http.StatusForbidden, "forbidden")
}

// «Ушёл» с cancel_future_charges: начисления за месяцы после месяца последнего дня отменяются с причиной
// «ученик ушёл» (ledger.cancel в журнале), за месяц ухода — остаются; без флага — ничего не отменяется.
func TestLeftCancelsFutureCharges(t *testing.T) {
	c := newCatalog(t, "+996708900001")
	next := c.today.FirstOfMonth().AddMonths(1)
	leaver := func(name, phone string) uuid.UUID {
		s := c.student(t, name, phone, nil)
		c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.FirstOfMonth().String(), "first_month": "full"})
		return s.ID
	}
	a, b := leaver("Бекжан Асанов", "0708 900 101"), leaver("Элина Сыдыкова", "0708 900 102")
	if res := runMonthly(t, c, next); res.Charged != 2 {
		t.Fatalf("billing run на %s: %+v", next, res)
	}
	type charge struct {
		Period civil.Date
		Reason *string
	}
	charges := func(sid uuid.UUID) []charge {
		t.Helper()
		rows, err := c.api.pool.Query(t.Context(), `SELECT period, cancel_reason FROM ledger WHERE student_id = $1 AND kind = 'charge' ORDER BY period`, sid)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []charge
		for rows.Next() {
			var ch charge
			if err := rows.Scan(&ch.Period, &ch.Reason); err != nil {
				t.Fatal(err)
			}
			out = append(out, ch)
		}
		return out
	}
	patch := func(sid uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
		return c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+sid.String()), body)
	}

	// Флаг без ухода — ошибка.
	assertField(t, patch(a, map[string]any{"cancel_future_charges": true}), http.StatusBadRequest, "cancel_future_charges")

	if rec := patch(a, map[string]any{"status": "left", "left_date": c.today.String(), "cancel_future_charges": true}); rec.Code != http.StatusOK {
		t.Fatalf("ушёл: %d %s", rec.Code, rec.Body)
	}
	got := charges(a)
	if len(got) != 2 || got[0].Period != c.today.FirstOfMonth() || got[0].Reason != nil || got[1].Period != next || got[1].Reason == nil || *got[1].Reason != "ученик ушёл" {
		t.Errorf("начисления ушедшего: %+v", got)
	}
	var balance int64
	if err := c.api.pool.QueryRow(t.Context(), `SELECT COALESCE(sum(amount), 0) FROM ledger WHERE student_id = $1 AND cancelled_at IS NULL`, a).Scan(&balance); err != nil || balance != -300_000 {
		t.Errorf("баланс: %d %v", balance, err)
	}
	cancels := filterAudit(auditLog(t, c.api, c.center), "ledger.cancel")
	if len(cancels) != 1 || cancels[0].UserID == nil || cancels[0].After["reason"] != "ученик ушёл" {
		t.Errorf("журнал: %+v", cancels)
	}

	// Без флага — начисление за следующий месяц остаётся долгом.
	if rec := patch(b, map[string]any{"status": "left", "left_date": c.today.String()}); rec.Code != http.StatusOK {
		t.Fatalf("ушёл без отмены: %d %s", rec.Code, rec.Body)
	}
	if got := charges(b); len(got) != 2 || got[1].Reason != nil {
		t.Errorf("без флага: %+v", got)
	}
}

// setDue — срок неотменённых начислений ученика (сценарии «просрочено / ожидает оплаты»).
func setDue(t *testing.T, c *catalog, sid uuid.UUID, due civil.Date) {
	t.Helper()
	if _, err := c.api.pool.Exec(t.Context(), `UPDATE ledger SET due_date = $2 WHERE student_id = $1 AND kind = 'charge' AND cancelled_at IS NULL`, sid, due); err != nil {
		t.Fatal(err)
	}
}

// «Сегодня» у владельца: бейдж «долг» (has_debt) — только у просроченных; срок сегодня или позже — без бейджа.
func TestTodayDebtBadgeOnlyOverdue(t *testing.T) {
	c := newCatalog(t, "+996709000001")
	student := func(name, phone string, due civil.Date) uuid.UUID {
		s := c.student(t, name, phone, nil)
		c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
		setDue(t, c, s.ID, due)
		return s.ID
	}
	overdue := student("Айдана Просрочено", "0709 000 101", c.today.AddDays(-1))
	today := student("София Срок Сегодня", "0709 000 102", c.today)
	later := student("Нурислам Срок Позже", "0709 000 103", c.today.AddDays(3))
	paid := c.student(t, "Бекжан Без Долга", "0709 000 104", nil)
	c.enroll(t, paid.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})

	l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "07:00"})
	got := decode[lessonResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/lessons/"+l.ID.String()+"/attendance"), nil))
	for id, want := range map[uuid.UUID]bool{overdue: true, today: false, later: false, paid.ID: false} {
		if st := studentOn(t, got, id); st.HasDebt == nil || *st.HasDebt != want {
			t.Errorf("%s: has_debt = %v, want %v", id, st.HasDebt, want)
		}
	}
}

// Текст приглашения — только на языках бота: BOT_LANGUAGES=ru — только русский, и WhatsApp — по-русски,
// даже если родитель раньше выбрал кыргызский; ru,ky — оба, WhatsApp — на языке родителя.
func TestParentInviteLangs(t *testing.T) {
	for _, tt := range []struct {
		langs     string
		wantTexts int
		wantLang  string
	}{
		{"ru", 1, "ru"},
		{"ru,ky", 2, "ky"},
	} {
		t.Run(tt.langs, func(t *testing.T) {
			api := newTestAPIWith(t, func(api *testAPI, d *httpapi.Deps) {
				d.Parents = parents.NewService(api.pool, testBotUsername).WithLangs(notify.MustLangs(tt.langs))
			})
			c := newCatalogOn(t, api, "+996708700001")
			s := c.student(t, "Нурланова Дана", "0708 700 111", nil)
			if _, err := api.pool.Exec(t.Context(), `UPDATE parents SET lang = 'ky' WHERE id = $1`, s.Parent.ID); err != nil {
				t.Fatal(err)
			}
			inv, _ := inviteParent(t, c, s.Parent.ID)
			if len(inv.Texts) != tt.wantTexts || inv.Texts["ru"] == "" || inv.Lang != tt.wantLang {
				t.Errorf("texts=%v lang=%s", inv.Texts, inv.Lang)
			}
			wa, err := url.Parse(inv.WhatsAppURL)
			if err != nil || wa.Query().Get("text") != inv.Texts[tt.wantLang] {
				t.Errorf("whatsapp_url = %s", inv.WhatsAppURL)
			}
		})
	}
}
