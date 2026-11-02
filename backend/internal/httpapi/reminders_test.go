package httpapi_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
	"okuu/internal/jobs"
	"okuu/internal/notify"
	"okuu/internal/reminders"
	"okuu/internal/telegram/telegramtest"
)

// Напоминания (R17–R19) на фейковом Telegram. Время — часы теста: «сегодня» и окно 9–20
// считаются по Бишкеку, дни листаются Advance(24h).

var bishkek = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Bishkek")
	if err != nil {
		panic(err)
	}
	return loc
}()

// atBishkek ставит часы теста на hh:mm того же дня по Бишкеку.
func atBishkek(api *testAPI, hour, minute int) {
	now := api.clock.Now().In(bishkek)
	api.clock.Set(time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, bishkek))
}

// linkDirect — родитель подключён к боту (сам сценарий привязки проверяет telegram_test.go).
func linkDirect(t *testing.T, api *testAPI, parentID uuid.UUID, chatID int64) {
	t.Helper()
	if _, err := api.pool.Exec(context.Background(),
		`UPDATE parents SET telegram_chat_id = $2, tg_status = 'active', consent_at = now() WHERE id = $1`, parentID, chatID); err != nil {
		t.Fatal(err)
	}
}

func setTgStatus(t *testing.T, api *testAPI, parentID uuid.UUID, status string) {
	t.Helper()
	if _, err := api.pool.Exec(context.Background(), `UPDATE parents SET tg_status = $2 WHERE id = $1`, parentID, status); err != nil {
		t.Fatal(err)
	}
}

func (c *catalog) patchSettings(t *testing.T, body map[string]any) {
	t.Helper()
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/settings"), body); rec.Code != http.StatusOK {
		t.Fatalf("settings %v: %d %s", body, rec.Code, rec.Body)
	}
}

// debtor — ученик с месячным тарифом, начисление за этот месяц сегодня (срок = сегодня + due_days).
func (c *catalog) debtor(t *testing.T, name, phone string) studentResp {
	t.Helper()
	s := c.student(t, name, phone, nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	return s
}

type notifRow struct {
	Kind      string
	Status    string
	Key       *string
	Students  []string
	Error     *string
	CreatedBy *uuid.UUID
}

func notificationsOf(t *testing.T, api *testAPI, parentID uuid.UUID) []notifRow {
	t.Helper()
	rows, err := api.pool.Query(context.Background(), `
		SELECT kind, status, dedup_key, student_ids::text[], error, created_by
		FROM notifications WHERE parent_id = $1 ORDER BY sent_at, id`, parentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []notifRow
	for rows.Next() {
		var r notifRow
		if err := rows.Scan(&r.Kind, &r.Status, &r.Key, &r.Students, &r.Error, &r.CreatedBy); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func outboxCount(t *testing.T, api *testAPI) int {
	t.Helper()
	var n int
	if err := api.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *botEnv) runAuto(t *testing.T) reminders.AutoResult {
	t.Helper()
	res, err := e.rem.RunAuto(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *botEnv) dispatch(t *testing.T) int {
	t.Helper()
	n, err := e.rem.Dispatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Каждый вид автонапоминания — в свой день; повтор в тот же день ничего не шлёт.
func TestRemindersAutoKindsAndTiming(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996707100001")
	c.patchSettings(t, map[string]any{"pay_details": "MBank: 0700 000 000 (Айгуль А.)"})
	s := c.debtor(t, "Асанов Бакыт", "0707 100 101")
	due := c.today.AddDays(5)
	const chat = 7100101
	linkDirect(t, env.api, s.Parent.ID, chat)

	want := map[int]string{2: notify.KindDueSoon, 5: notify.KindDueToday, 8: notify.KindOverdue3, 12: notify.KindOverdue7}
	sent := 0
	for day := 0; day <= 16; day++ {
		if day > 0 {
			env.api.clock.Advance(24 * time.Hour)
		}
		res := env.runAuto(t)
		again := env.runAuto(t)
		msgs := env.tg.Messages(chat)
		kind, expected := want[day]
		if !expected {
			if len(msgs) != sent || res.Sent != 0 {
				t.Fatalf("день %d: лишнее напоминание: %+v", day, msgs[len(msgs)-1].Text)
			}
			continue
		}
		if len(msgs) != sent+1 || res.Sent != 1 {
			t.Fatalf("день %d (%s): сообщений %d, want %d", day, kind, len(msgs), sent+1)
		}
		sent++
		if again.Sent != 0 || again.Limited != 1 {
			t.Errorf("день %d: повторный запуск %+v, want limited", day, again)
		}
		m := msgs[len(msgs)-1]
		overdue := max(0, day-5)
		wantText := notify.DebtReminder(notify.For("ru"), kind, c.centerName(t), due,
			[]notify.ChildDebt{{Name: "Асанов Бакыт", Debt: 300_000, Due: &due, DaysOverdue: overdue, DueToday: day == 5}},
			"MBank: 0700 000 000 (Айгуль А.)")
		if m.Text != wantText {
			t.Errorf("день %d:\n%s\nwant:\n%s", day, m.Text, wantText)
		}
		// Реквизиты — под «Как оплатить:» как есть; кнопок оплаты нет, только «Баланс и посещения».
		if !strings.Contains(m.Text, "Как оплатить:\nMBank: 0700 000 000 (Айгуль А.)\n\nЕсли вы уже оплатили") || strings.Contains(m.Text, "Реквизиты") {
			t.Errorf("день %d: «Как оплатить»:\n%s", day, m.Text)
		}
		if b := m.Buttons(); fmt.Sprint(b) != "[Баланс и посещения|balance]" {
			t.Errorf("кнопки: %v", b)
		}
		rows := notificationsOf(t, env.api, s.Parent.ID)
		last := rows[len(rows)-1]
		if last.Kind != kind || last.Status != "sent" || last.Key == nil ||
			*last.Key != fmt.Sprintf("%s:%s:%s", kind, s.Parent.ID, due) || fmt.Sprint(last.Students) != fmt.Sprint([]string{s.ID.String()}) {
			t.Errorf("день %d: notifications %+v", day, last)
		}
	}
	for _, text := range []string{"просрочено на 3 дня", "просрочено на 7 дней"} {
		found := false
		for _, m := range env.tg.Messages(chat) {
			found = found || strings.Contains(m.Text, text)
		}
		if !found {
			t.Errorf("нет сообщения с %q", text)
		}
	}
	card := decode[studentResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+s.ID.String()), nil))
	var lastReminder *time.Time
	if err := env.api.pool.QueryRow(context.Background(), `SELECT last_reminder_at FROM parents WHERE id = $1`, s.Parent.ID).Scan(&lastReminder); err != nil {
		t.Fatal(err)
	}
	if lastReminder == nil || card.Parent == nil {
		t.Error("last_reminder_at не заполнен")
	}
}

// Повтор после 7-го дня просрочки (overdue_repeat = weekly, по умолчанию): 14-й и 21-й день, ключ с номером недели,
// повторный запуск в тот же день не дублирует; оплата останавливает повтор; off — после 7-го дня тишина.
func TestRemindersOverdueWeekly(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	weekly := newCatalogOn(t, env.api, "+996707150001")
	off := newCatalogOn(t, env.api, "+996707150002")
	off.patchSettings(t, map[string]any{"overdue_repeat": "off"})
	if got := decode[map[string]any](t, weekly.owner.do(http.MethodGet, centerPath(weekly.center, "/settings"), nil))["overdue_repeat"]; got != "weekly" {
		t.Errorf("по умолчанию overdue_repeat = %v, want weekly", got)
	}
	assertField(t, off.owner.do(http.MethodPatch, centerPath(off.center, "/settings"), map[string]any{"overdue_repeat": "daily"}), http.StatusBadRequest, "overdue_repeat")

	sw := weekly.debtor(t, "Асанов Бакыт", "0707 150 101") // срок — сегодня + 5 (due_days по умолчанию)
	so := off.debtor(t, "Бекова Айпери", "0707 150 102")
	due := weekly.today.AddDays(5)
	const chatW, chatO = 7150101, 7150102
	linkDirect(t, env.api, sw.Parent.ID, chatW)
	linkDirect(t, env.api, so.Parent.ID, chatO)

	// Перематываем на день, когда просрочка = overdue, и запускаем задачу дважды.
	day := 0
	runOn := func(overdue int) (first, again reminders.AutoResult) {
		t.Helper()
		target := 5 + overdue
		env.api.clock.Advance(time.Duration(target-day) * 24 * time.Hour)
		day = target
		return env.runAuto(t), env.runAuto(t)
	}
	lastKind := func(parent uuid.UUID) notifRow {
		rows := notificationsOf(t, env.api, parent)
		if len(rows) == 0 {
			return notifRow{}
		}
		return rows[len(rows)-1]
	}

	_, _ = runOn(7) // overdue_7 — у обоих центров
	if lastKind(sw.Parent.ID).Kind != notify.KindOverdue7 || lastKind(so.Parent.ID).Kind != notify.KindOverdue7 {
		t.Fatalf("7-й день: %+v / %+v", lastKind(sw.Parent.ID), lastKind(so.Parent.ID))
	}
	if res, _ := runOn(13); res.Sent != 0 {
		t.Errorf("13-й день: %+v", res)
	}

	res, again := runOn(14)
	if res.Sent != 1 || again.Sent != 0 || again.Limited != 1 {
		t.Fatalf("14-й день: %+v, повтор %+v", res, again)
	}
	w2 := lastKind(sw.Parent.ID)
	if w2.Kind != notify.KindOverdueWeekly || w2.Key == nil || *w2.Key != fmt.Sprintf("overdue_weekly:%s:%s:w2", sw.Parent.ID, due) {
		t.Errorf("14-й день: %+v", w2)
	}
	if m := env.tg.Last(chatW); !strings.Contains(m.Text, "оплата просрочена") || !strings.Contains(m.Text, "просрочено на 14 дней") {
		t.Errorf("текст 14-го дня: %q", m.Text)
	}
	if lastKind(so.Parent.ID).Kind != notify.KindOverdue7 {
		t.Error("off: на 14-й день напоминания быть не должно")
	}

	// Между неделями — тишина.
	if res, _ := runOn(15); res.Sent != 0 {
		t.Errorf("15-й день: %+v", res)
	}
	res, _ = runOn(21)
	if res.Sent != 1 {
		t.Fatalf("21-й день: %+v", res)
	}
	if w3 := lastKind(sw.Parent.ID); w3.Key == nil || !strings.HasSuffix(*w3.Key, ":w3") {
		t.Errorf("21-й день: %+v", w3)
	}

	// Оплатили — на 28-й день тишина.
	weekly.mustOp(t, sw.ID, pay(300_000))
	if res, _ := runOn(28); res.Sent != 0 {
		t.Errorf("28-й день после оплаты: %+v", res)
	}
	weeklyCount := 0
	for _, r := range notificationsOf(t, env.api, sw.Parent.ID) {
		if r.Kind == notify.KindOverdueWeekly {
			weeklyCount++
		}
	}
	if weeklyCount != 2 {
		t.Errorf("повторов %d, want 2 (14 и 21)", weeklyCount)
	}
	for _, r := range notificationsOf(t, env.api, so.Parent.ID) {
		if r.Kind == notify.KindOverdueWeekly {
			t.Errorf("off: повтор %+v", r)
		}
	}
}

func TestAutoKind(t *testing.T) {
	due := civil.New(2026, 10, 1)
	for _, tt := range []struct {
		days   int
		weekly bool
		want   string
	}{
		{-3, true, notify.KindDueSoon}, {0, true, notify.KindDueToday}, {3, true, notify.KindOverdue3},
		{7, true, notify.KindOverdue7}, {7, false, notify.KindOverdue7}, {8, true, ""}, {13, true, ""},
		{14, true, notify.KindOverdueWeekly}, {14, false, ""}, {21, true, notify.KindOverdueWeekly},
		{28, true, notify.KindOverdueWeekly}, {29, true, ""}, {70, true, notify.KindOverdueWeekly}, {-1, true, ""},
	} {
		if got := reminders.AutoKind(due, due.AddDays(tt.days), tt.weekly); got != tt.want {
			t.Errorf("день %d, weekly=%v: %q, want %q", tt.days, tt.weekly, got, tt.want)
		}
	}
}

// Окно 9:00–20:00 по поясу центра.
func TestRemindersWindow(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 12, 0)
	c := newCatalogOn(t, env.api, "+996707200001")
	c.patchSettings(t, map[string]any{"due_days": 3}) // срок через 3 дня — due_soon сегодня
	s := c.debtor(t, "Бекова Айпери", "0707 200 101")
	const chat = 7200101
	linkDirect(t, env.api, s.Parent.ID, chat)

	for _, tt := range []struct {
		h, m int
		sent bool
	}{{8, 59, false}, {20, 0, false}, {23, 30, false}, {0, 0, false}, {19, 59, true}} {
		atBishkek(env.api, tt.h, tt.m)
		res := env.runAuto(t)
		if (res.Sent == 1) != tt.sent {
			t.Errorf("%02d:%02d: %+v, want sent=%v", tt.h, tt.m, res, tt.sent)
		}
	}
	if ok, _ := reminders.InWindow(time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC), "Asia/Bishkek"); !ok {
		t.Error("03:00 UTC = 09:00 в Бишкеке — в окне")
	}
}

// R18: дети одного родителя в центре — одним сообщением, с итогом; другой центр — своим сообщением.
func TestRemindersGroupChildren(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	a := newCatalogOn(t, env.api, "+996707300001")
	b := newCatalogOn(t, env.api, "+996707300002")
	a.patchSettings(t, map[string]any{"due_days": 3})
	s1 := a.debtor(t, "Касымов Тимур", "0707 300 101") // due_soon
	a.patchSettings(t, map[string]any{"due_days": 0})
	s2 := a.debtor(t, "Касымова Дана", "0707 300 101")   // due_today — важнее, он в заголовке
	other := a.debtor(t, "Чужой Ученик", "0707 300 999") // другой родитель, не подключён
	b.patchSettings(t, map[string]any{"due_days": 3})
	sb := b.debtor(t, "Касымов Арсен", "0707 300 101")
	const chat = 7300101
	linkDirect(t, env.api, s1.Parent.ID, chat)
	linkDirect(t, env.api, sb.Parent.ID, chat)
	_ = other

	if res := env.runAuto(t); res.Sent != 2 {
		t.Fatalf("отправлено %+v, want 2 (по сообщению на центр)", res)
	}
	var msgA, msgB string
	for _, m := range env.tg.Messages(chat) {
		switch {
		case strings.Contains(m.Text, a.centerName(t)):
			msgA = m.Text
		case strings.Contains(m.Text, b.centerName(t)):
			msgB = m.Text
		}
	}
	for _, want := range []string{"сегодня срок оплаты", "Касымов Тимур — 3 000 сом", "Касымова Дана — 3 000 сом", "Итого: 6 000 сом"} {
		if !strings.Contains(msgA, want) {
			t.Errorf("сообщение центра А без %q:\n%s", want, msgA)
		}
	}
	if strings.Contains(msgA, "Чужой") || strings.Contains(msgA, "Арсен") {
		t.Errorf("в сообщении чужие дети:\n%s", msgA)
	}
	if !strings.Contains(msgB, "через 3 дня") || !strings.Contains(msgB, "Касымов Арсен") || strings.Contains(msgB, "Итого") {
		t.Errorf("сообщение центра Б:\n%s", msgB)
	}
	rows := notificationsOf(t, env.api, s1.Parent.ID)
	if len(rows) != 1 || rows[0].Kind != notify.KindDueToday || len(rows[0].Students) != 2 {
		t.Errorf("notifications центра А: %+v", rows)
	}
	if s2.Parent.ID != s1.Parent.ID {
		t.Fatal("родитель не общий")
	}
}

// R17: не больше одного напоминания о долге в день — ручное утром закрывает автоматическое.
func TestRemindersDailyLimit(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 9, 0)
	c := newCatalogOn(t, env.api, "+996707400001")
	c.patchSettings(t, map[string]any{"due_days": 3})
	s := c.debtor(t, "Давлетов Эрлан", "0707 400 101")
	const chat = 7400101
	linkDirect(t, env.api, s.Parent.ID, chat)

	r := decode[remindResp](t, remind(c, map[string]any{"student_ids": []uuid.UUID{s.ID}}))
	if r.Sent != 1 {
		t.Fatalf("ручное: %+v", r)
	}
	atBishkek(env.api, 10, 0)
	if res := env.runAuto(t); res.Sent != 0 || res.Limited != 1 {
		t.Errorf("авто после ручного: %+v", res)
	}
	if n := len(env.tg.Messages(chat)); n != 1 {
		t.Errorf("сообщений %d, want 1", n)
	}
}

// reminders_on=false: автонапоминаний и package_low нет; «оплата получена» уходит.
func TestRemindersOff(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996707500001")
	c.patchSettings(t, map[string]any{"due_days": 3, "reminders_on": false})
	s := c.debtor(t, "Жумабаев Нурс", "0707 500 101")
	const chat = 7500101
	linkDirect(t, env.api, s.Parent.ID, chat)

	if res := env.runAuto(t); res.Sent != 0 {
		t.Errorf("reminders_on=false: %+v", res)
	}
	c.mustOp(t, s.ID, pay(100_000))
	if n := env.dispatch(t); n != 1 {
		t.Errorf("paid при reminders_on=false: %d", n)
	}
}

// Повторный запуск не шлёт повторно; неудачная отправка повторяется; 403 — blocked (R19).
func TestRemindersFailedAndBlocked(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996707600001")
	c.patchSettings(t, map[string]any{"due_days": 3})
	s1 := c.debtor(t, "Исаков Нурбек", "0707 600 101")
	s2 := c.debtor(t, "Осмонова Мээрим", "0707 600 102")
	const chat1, chat2 = 7600101, 7600102
	linkDirect(t, env.api, s1.Parent.ID, chat1)
	linkDirect(t, env.api, s2.Parent.ID, chat2)
	env.tg.Fail("sendMessage", chat1, http.StatusBadRequest, "Bad Request: chat not found", 0, 1)
	env.tg.Block(chat2)

	if res := env.runAuto(t); res.Sent != 0 || res.Failed != 1 || res.Blocked != 1 {
		t.Fatalf("первый запуск: %+v", res)
	}
	r1 := notificationsOf(t, env.api, s1.Parent.ID)
	if len(r1) != 1 || r1[0].Status != "failed" || r1[0].Key != nil || r1[0].Error == nil ||
		!strings.Contains(*r1[0].Error, "chat not found") || strings.Contains(*r1[0].Error, telegramtest.Token) {
		t.Errorf("failed: %+v", r1)
	}
	r2 := notificationsOf(t, env.api, s2.Parent.ID)
	if len(r2) != 1 || r2[0].Status != "blocked" {
		t.Errorf("blocked: %+v", r2)
	}
	if p := parentTGOf(t, env.api, s2.Parent.ID); p.Status != "blocked" {
		t.Errorf("родитель после 403: %+v", p)
	}

	// Через час: неудачное повторяется, заблокировавшему не пишем.
	env.api.clock.Advance(time.Hour)
	if res := env.runAuto(t); res.Sent != 1 || res.Blocked != 0 {
		t.Errorf("повтор: %+v", res)
	}
	env.api.clock.Advance(time.Hour)
	if res := env.runAuto(t); res.Sent != 0 {
		t.Errorf("третий запуск: %+v", res)
	}
	if n := len(env.tg.Messages(chat1)); n != 1 {
		t.Errorf("сообщений родителю 1: %d", n)
	}
	// Ключ дедупликации защищает и без дневного лимита: удалим отметку времени — запуск всё равно ничего не шлёт.
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE notifications SET sent_at = sent_at - interval '2 days' WHERE parent_id = $1`, s1.Parent.ID); err != nil {
		t.Fatal(err)
	}
	if res := env.runAuto(t); res.Sent != 0 || res.Duplicate != 1 {
		t.Errorf("после сдвига времени: %+v", res)
	}
}

type remindResp struct {
	Sent     int `json:"sent"`
	Messages int `json:"messages"`
	Skipped  []struct {
		StudentID uuid.UUID `json:"student_id"`
		Reason    string    `json:"reason"`
	} `json:"skipped"`
}

func (r remindResp) reasons() map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, s := range r.Skipped {
		out[s.StudentID] = s.Reason
	}
	return out
}

func remind(c *catalog, body any) *httptest.ResponseRecorder {
	return c.owner.do(http.MethodPost, centerPath(c.center, "/reminders"), body)
}

func mustRemind(t *testing.T, c *catalog, body any) remindResp {
	t.Helper()
	rec := remind(c, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("reminders: %d %s", rec.Code, rec.Body)
	}
	return decode[remindResp](t, rec)
}

// Ручные «Напомнить»: причины пропуска, группировка, лимит раз в час, all.
func TestRemindersManual(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 21, 0) // ручные — и вне окна: их отправляет сотрудник
	c := newCatalogOn(t, env.api, "+996707700001")
	s1 := c.debtor(t, "Алиев Азамат", "0707 700 101")
	s6 := c.debtor(t, "Алиева Айжан", "0707 700 101") // тот же родитель
	s2 := c.debtor(t, "Бобров Без Телеграма", "0707 700 102")
	s3 := c.debtor(t, "Васильев Отключил", "0707 700 103")
	s4 := c.debtor(t, "Гусев Заблокировал", "0707 700 104")
	s5 := c.student(t, "Дмитриев Без Долга", "0707 700 105", nil)
	const chat = 7700101
	linkDirect(t, env.api, s1.Parent.ID, chat)
	linkDirect(t, env.api, s3.Parent.ID, 7700103)
	setTgStatus(t, env.api, s3.Parent.ID, "stopped")
	linkDirect(t, env.api, s4.Parent.ID, 7700104)
	setTgStatus(t, env.api, s4.Parent.ID, "blocked")
	linkDirect(t, env.api, s5.Parent.ID, 7700105)

	all := []uuid.UUID{s1.ID, s2.ID, s3.ID, s4.ID, s5.ID, s6.ID, s1.ID} // дубль — не страшно
	r := mustRemind(t, c, map[string]any{"student_ids": all})
	wantSkipped := map[uuid.UUID]string{s2.ID: "no_telegram", s3.ID: "stopped", s4.ID: "blocked", s5.ID: "no_debt"}
	if r.Sent != 2 || r.Messages != 1 || fmt.Sprint(r.reasons()) != fmt.Sprint(wantSkipped) {
		t.Fatalf("первый раз: %+v", r)
	}
	m := one(t, env.tg.Messages(chat))
	// Реквизиты в центре не заданы — «Как оплатить: уточните у администратора»; кнопок оплаты нет.
	for _, want := range []string{"напоминание об оплате", "Алиев Азамат — 3 000 сом", "Алиева Айжан — 3 000 сом", "Итого: 6 000 сом", "срок до",
		"Как оплатить:\nУточните способ оплаты у администратора центра.", "Если вы уже оплатили"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("сообщение без %q:\n%s", want, m.Text)
		}
	}
	if b := m.Buttons(); fmt.Sprint(b) != "[Баланс и посещения|balance]" {
		t.Errorf("кнопки ручного напоминания: %v", b)
	}
	rows := notificationsOf(t, env.api, s1.Parent.ID)
	if len(rows) != 1 || rows[0].Kind != "manual" || rows[0].CreatedBy == nil || rows[0].Key != nil || len(rows[0].Students) != 2 {
		t.Errorf("notifications: %+v", rows)
	}

	// Меньше часа — rate_limited.
	env.api.clock.Advance(59 * time.Minute)
	r = mustRemind(t, c, map[string]any{"student_ids": []uuid.UUID{s1.ID, s6.ID}})
	if r.Sent != 0 || fmt.Sprint(r.reasons()) != fmt.Sprint(map[uuid.UUID]string{s1.ID: "rate_limited", s6.ID: "rate_limited"}) {
		t.Errorf("через 59 минут: %+v", r)
	}
	// Через час — снова можно; all — только должники, s5 без долга в ответе не упоминается.
	env.api.clock.Advance(2 * time.Minute)
	r = mustRemind(t, c, map[string]any{"all": true})
	wantAll := map[uuid.UUID]string{s2.ID: "no_telegram", s3.ID: "stopped", s4.ID: "blocked"}
	if r.Sent != 2 || r.Messages != 1 || fmt.Sprint(r.reasons()) != fmt.Sprint(wantAll) {
		t.Errorf("all: %+v", r)
	}
	if got := filterAudit(auditLog(t, env.api, c.center), "reminder."); len(got) != 3 || got[0].UserID == nil {
		t.Errorf("журнал: %+v", got)
	}

	// Ошибки запроса.
	assertField(t, remind(c, map[string]any{}), http.StatusBadRequest, "student_ids")
	assertField(t, remind(c, map[string]any{"all": true, "student_ids": []uuid.UUID{s1.ID}}), http.StatusBadRequest, "student_ids")
	assertError(t, remind(c, map[string]any{"student_ids": []string{"x"}}), http.StatusBadRequest, "bad_request")
	assertError(t, remind(c, map[string]any{"student_ids": []uuid.UUID{s1.ID, uuid.New()}}), http.StatusNotFound, "not_found")
	other := newCatalogOn(t, env.api, "+996707700002")
	foreign := other.debtor(t, "Чужой Должник", "0707 700 201")
	linkDirect(t, env.api, foreign.Parent.ID, 7700201)
	assertError(t, remind(c, map[string]any{"student_ids": []uuid.UUID{foreign.ID}}), http.StatusNotFound, "not_found")
	if len(env.tg.Messages(7700201)) != 0 {
		t.Error("ушло напоминание ученику чужого центра")
	}
	teacher, _ := addStaff(t, env.api, c.owner, c.center, "teacher", "+996707700003")
	assertError(t, teacher.do(http.MethodPost, centerPath(c.center, "/reminders"), map[string]any{"all": true}), http.StatusForbidden, "forbidden")
}

func TestRemindersManualTelegramOff(t *testing.T) {
	c := newCatalog(t, "+996707800001")
	s := c.debtor(t, "Без Бота", "0707 800 101")
	assertError(t, remind(c, map[string]any{"student_ids": []uuid.UUID{s.ID}}), http.StatusConflict, "telegram_not_configured")
}

// paid: сразу после оплаты; отменённая до отправки — не уходит; ночью — утром; /stop — не уходит.
func TestNotifyPaid(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996707900001")
	s := c.debtor(t, "Сыдыков Арген", "0707 900 101")
	const chat = 7900101
	linkDirect(t, env.api, s.Parent.ID, chat)

	op := c.mustOp(t, s.ID, pay(100_000))
	if n := env.dispatch(t); n != 1 {
		t.Fatalf("отправлено %d", n)
	}
	want := "🏫 Центр «" + c.centerName(t) + "»\nОплата получена: 1 000 сом — Сыдыков Арген. Спасибо!\nБаланс: долг 2 000 сом"
	if m := one(t, env.tg.Messages(chat)); m.Text != want {
		t.Errorf("текст: %q", m.Text)
	}
	if rows := notificationsOf(t, env.api, s.Parent.ID); len(rows) != 1 || rows[0].Kind != "paid" || *rows[0].Key != "paid:"+op.ID.String() {
		t.Errorf("notifications: %+v", rows)
	}
	if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 0 {
		t.Errorf("повторная отправка: %d, в очереди %d", n, outboxCount(t, env.api))
	}

	// Отменили до отправки — уведомления нет.
	op2 := c.mustOp(t, s.ID, pay(50_000))
	if rec := c.cancel(t, c.owner, op2.ID, "ошибочная оплата"); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 0 {
		t.Errorf("отменённая оплата: %d", n)
	}

	// Ночью — ждёт утра.
	atBishkek(env.api, 22, 0)
	c.mustOp(t, s.ID, pay(30_000))
	if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 1 {
		t.Errorf("ночью: отправлено %d, в очереди %d", n, outboxCount(t, env.api))
	}
	env.api.clock.Advance(10*time.Hour + 59*time.Minute) // 08:59
	if n := env.dispatch(t); n != 0 {
		t.Errorf("08:59: %d", n)
	}
	env.api.clock.Advance(time.Minute) // 09:00
	if n := env.dispatch(t); n != 1 {
		t.Errorf("09:00: %d", n)
	}

	// После /stop — не шлём, событие снимается.
	setTgStatus(t, env.api, s.Parent.ID, "stopped")
	c.mustOp(t, s.ID, pay(10_000))
	if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 0 {
		t.Errorf("stopped: %d, в очереди %d", n, outboxCount(t, env.api))
	}
	if n := len(env.tg.Messages(chat)); n != 2 {
		t.Errorf("всего сообщений %d, want 2", n)
	}
}

// package_low (R10): списание оставило 1 занятие; дальше 0 — не повторяем; reminders_on=false — нет.
func TestNotifyPackageLow(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708000001")
	sid := packageStudent(t, c, "Токтогулов Арсен", "0708 000 101")
	card := c.card(t, sid)
	const chat = 8000101
	linkDirect(t, env.api, card.Parent.ID, chat)
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE enrollments SET lessons_left = 2 WHERE student_id = $1`, sid); err != nil {
		t.Fatal(err)
	}

	l1 := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "07:00"})
	mustMark(t, c, c.owner, l1.ID, sid, "present") // 2 → 1
	if n := env.dispatch(t); n != 1 {
		t.Fatalf("package_low: %d", n)
	}
	if m := one(t, env.tg.Messages(chat)); m.Text != notify.For("ru").PackageLow(c.centerName(t), "Токтогулов Арсен", "Английский A1") {
		t.Errorf("текст: %q", m.Text)
	}
	l2 := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "08:00"})
	mustMark(t, c, c.owner, l2.ID, sid, "absent") // 1 → 0: не «осталось 1»
	if n := env.dispatch(t); n != 0 {
		t.Errorf("на нуле: %d", n)
	}

	// Снова до 1, но напоминания центра выключены.
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE enrollments SET lessons_left = 2 WHERE student_id = $1`, sid); err != nil {
		t.Fatal(err)
	}
	c.patchSettings(t, map[string]any{"reminders_on": false})
	env.api.clock.Advance(24 * time.Hour)
	l3 := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "09:00"})
	mustMark(t, c, c.owner, l3.ID, sid, "present")
	if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 0 {
		t.Errorf("reminders_on=false: %d", n)
	}
}

// absent: только при notify_absent; исправленная отметка не уходит; старое занятие — не новость.
func TestNotifyAbsent(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708100001")
	s := c.student(t, "Нурланова Дана", "0708 100 101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.AddDays(-7).String(), "first_month": "none"})
	const chat = 8100101
	linkDirect(t, env.api, s.Parent.ID, chat)
	lesson := func(date civil.Date, start string) lessonResp {
		return createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": date.String(), "start_time": start})
	}

	l1 := lesson(c.today, "07:00")
	mustMark(t, c, c.owner, l1.ID, s.ID, "absent")
	if outboxCount(t, env.api) != 0 {
		t.Fatal("notify_absent выключен по умолчанию, а событие записано")
	}

	c.patchSettings(t, map[string]any{"notify_absent": true})
	l2 := lesson(c.today, "08:00")
	mustMark(t, c, c.owner, l2.ID, s.ID, "absent")
	if n := env.dispatch(t); n != 1 {
		t.Fatalf("absent: %d", n)
	}
	if m := one(t, env.tg.Messages(chat)); m.Text != notify.For("ru").Absent(c.centerName(t), "Нурланова Дана", "Английский A1", c.today, "08:00") {
		t.Errorf("текст: %q", m.Text)
	}
	// Исправили на «был», потом снова «не был» — второго сообщения нет (ключ занят).
	mustMark(t, c, c.owner, l2.ID, s.ID, "present")
	mustMark(t, c, c.owner, l2.ID, s.ID, "absent")
	if n := env.dispatch(t); n != 0 {
		t.Errorf("повторная отметка: %d", n)
	}

	// Поставили «не был» и сразу исправили до отправки.
	l3 := lesson(c.today, "09:00")
	mustMark(t, c, c.owner, l3.ID, s.ID, "absent")
	mustMark(t, c, c.owner, l3.ID, s.ID, "present")
	if n := env.dispatch(t); n != 0 {
		t.Errorf("исправленная отметка: %d", n)
	}
	// Занятие трёхдневной давности — не новость.
	l4 := lesson(c.today.AddDays(-3), "07:00")
	mustMark(t, c, c.owner, l4.ID, s.ID, "absent")
	if outboxCount(t, env.api) != 0 {
		t.Error("старое занятие попало в очередь")
	}
	if n := len(env.tg.Messages(chat)); n != 1 {
		t.Errorf("всего сообщений %d", n)
	}
}

// Диспетчер просыпается по NOTIFY сразу после оплаты, не дожидаясь интервала.
func TestDispatcherWakesOnNotify(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708200001")
	s := c.debtor(t, "Мамытов Эмир", "0708 200 101")
	const chat = 8200101
	linkDirect(t, env.api, s.Parent.ID, chat)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		jobs.RunDispatcher(ctx, ctx, env.api.pool, slog.New(slog.DiscardHandler), nil, env.rem, time.Hour)
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(100 * time.Millisecond) // диспетчер подписался и разобрал пустую очередь
	c.mustOp(t, s.ID, pay(100_000))
	if m := waitMessage(t, env.tg, chat); !strings.Contains(m.Text, "Оплата получена") {
		t.Errorf("текст: %q", m.Text)
	}
}
