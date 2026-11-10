package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type waResp struct {
	ParentID uuid.UUID `json:"parent_id"`
	Phone    *string   `json:"phone"`
	Students []struct {
		ID       uuid.UUID `json:"id"`
		FullName string    `json:"full_name"`
		Debt     int64     `json:"debt"`
	} `json:"students"`
	Total       int64   `json:"total"`
	Text        string  `json:"text"`
	URL         *string `json:"url"`
	OpenedToday bool    `json:"opened_today"`
}

func waPath(c *catalog, id uuid.UUID) string {
	return centerPath(c.center, "/students/"+id.String()+"/whatsapp-reminder")
}

func mustWA(t *testing.T, c *catalog, method string, id uuid.UUID) waResp {
	t.Helper()
	rec := c.owner.do(method, waPath(c, id), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s whatsapp: %d %s", method, rec.Code, rec.Body)
	}
	return decode[waResp](t, rec)
}

func waNotifications(t *testing.T, api *testAPI, parent uuid.UUID) (n int, kind string) {
	t.Helper()
	err := api.pool.QueryRow(context.Background(), `
		SELECT count(*), COALESCE(max(kind), '') FROM notifications
		WHERE parent_id = $1 AND channel = 'whatsapp_link' AND status = 'link_opened'`, parent).Scan(&n, &kind)
	if err != nil {
		t.Fatal(err)
	}
	return n, kind
}

func auditCount(t *testing.T, api *testAPI, center uuid.UUID, action string) (n int) {
	t.Helper()
	if err := api.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE center_id = $1 AND action = $2`, center, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type debtorWA struct {
	StudentID uuid.UUID `json:"student_id"`
	Opened    bool      `json:"whatsapp_opened_today"`
	Reminded  bool      `json:"reminded_today"`
	Parent    *struct {
		WhatsAppPhone *string `json:"whatsapp_phone"`
	} `json:"parent"`
}

func debtorsWA(t *testing.T, c *catalog) map[uuid.UUID]debtorWA {
	t.Helper()
	out := map[uuid.UUID]debtorWA{}
	for _, status := range []string{"overdue", "pending"} {
		rec := c.owner.do(http.MethodGet, centerPath(c.center, "/debtors?status="+status), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("debtors: %d %s", rec.Code, rec.Body)
		}
		for _, d := range decodeItems[debtorWA](t, rec) {
			out[d.StudentID] = d
		}
	}
	return out
}

// Кнопка WhatsApp: GET ничего не пишет, POST отмечает; в тексте все дети родителя с долгом; переплата брата не вычитается.
func TestWhatsAppReminder(t *testing.T) {
	c := newCatalog(t, "+996708100001")
	c.patchSettings(t, map[string]any{"pay_details": "MBank 0708 100 001"})
	a := c.debtor(t, "Алиев Азамат", "0708 100 101")
	b := c.debtor(t, "Алиева Айжан", "0708 100 101")
	paidUp := c.student(t, "Алиев Переплатил", "0708 100 101", nil)
	c.mustOp(t, paidUp.ID, pay(50_000))

	got := mustWA(t, c, http.MethodGet, a.ID)
	if n, _ := waNotifications(t, c.api, a.Parent.ID); n != 0 {
		t.Fatalf("GET записал %d уведомлений", n)
	}
	if got.Phone == nil || *got.Phone != "996708100101" {
		t.Fatalf("телефон: %v", got.Phone)
	}
	if len(got.Students) != 2 || got.Total != 600_000 {
		t.Fatalf("дети и итог: %+v", got)
	}
	for _, want := range []string{"Алиев Азамат", "Алиева Айжан", "*Итого: 6 000 сом*", "*Как оплатить:*", "MBank 0708 100 001"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("в тексте нет %q:\n%s", want, got.Text)
		}
	}
	if strings.Contains(got.Text, "Переплатил") {
		t.Errorf("ребёнок с переплатой попал в напоминание:\n%s", got.Text)
	}
	u, err := url.Parse(*got.URL)
	if err != nil || u.Host != "wa.me" || u.Path != "/996708100101" || u.Query().Get("text") != got.Text {
		t.Fatalf("ссылка: %s (%v)", *got.URL, err)
	}

	opened := mustWA(t, c, http.MethodPost, a.ID)
	if !opened.OpenedToday || *opened.URL != *got.URL {
		t.Fatalf("POST: %+v", opened)
	}
	if n, kind := waNotifications(t, c.api, a.Parent.ID); n != 1 || kind != "manual" {
		t.Fatalf("notifications: %d %s", n, kind)
	}
	if n := auditCount(t, c.api, c.center, "reminder.whatsapp"); n != 1 {
		t.Fatalf("журнал: %d", n)
	}
	// Отметка — у обоих детей этого родителя; «напоминали» (Telegram) — нет: доставку мы не знаем.
	d := debtorsWA(t, c)
	if !d[a.ID].Opened || !d[b.ID].Opened || d[a.ID].Reminded {
		t.Fatalf("должники: %+v / %+v", d[a.ID], d[b.ID])
	}
	if p := d[a.ID].Parent; p == nil || p.WhatsAppPhone == nil || *p.WhatsAppPhone != "996708100101" {
		t.Fatalf("whatsapp_phone: %+v", p)
	}
	// Завтра отметки нет.
	c.api.clock.Advance(24 * time.Hour)
	if d := debtorsWA(t, c); d[a.ID].Opened {
		t.Error("«открыто» осталось на следующий день")
	}
}

func TestWhatsAppReminderErrors(t *testing.T) {
	c := newCatalog(t, "+996708200001")
	bad := c.debtor(t, "С Плохим Номером", "0708 200 101")
	if _, err := c.api.pool.Exec(context.Background(), `UPDATE parents SET phone = '12345' WHERE id = $1`, bad.Parent.ID); err != nil {
		t.Fatal(err)
	}
	// Текст есть (его можно скопировать), ссылки нет; кнопка — 409.
	if got := mustWA(t, c, http.MethodGet, bad.ID); got.URL != nil || got.Phone != nil || got.Text == "" {
		t.Fatalf("плохой номер: %+v", got)
	}
	assertError(t, c.owner.do(http.MethodPost, waPath(c, bad.ID), nil), http.StatusConflict, "parent_phone_invalid")
	if d := debtorsWA(t, c)[bad.ID]; d.Parent == nil || d.Parent.WhatsAppPhone != nil {
		t.Fatalf("должник с плохим номером: %+v", d)
	}

	noDebt := c.student(t, "Без Долга", "0708 200 102", nil)
	assertError(t, c.owner.do(http.MethodPost, waPath(c, noDebt.ID), nil), http.StatusConflict, "no_debt")
	assertError(t, c.owner.do(http.MethodGet, waPath(c, uuid.New()), nil), http.StatusNotFound, "not_found")

	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996708200003")
	assertError(t, teacher.do(http.MethodPost, waPath(c, bad.ID), nil), http.StatusForbidden, "forbidden")
}

func TestWhatsAppDemoCenter(t *testing.T) {
	c := newCatalog(t, "+996708300001")
	s := c.debtor(t, "Демо Должник", "0708 300 101")
	if _, err := c.api.pool.Exec(context.Background(), `UPDATE centers SET is_demo = true WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	if got := mustWA(t, c, http.MethodGet, s.ID); got.Text == "" {
		t.Fatal("демо: нет текста")
	}
	assertError(t, c.owner.do(http.MethodPost, waPath(c, s.ID), nil), http.StatusConflict, "demo_center")
	if n, _ := waNotifications(t, c.api, s.Parent.ID); n != 0 {
		t.Fatalf("демо: записано %d", n)
	}
	if me := decode[struct {
		Centers []struct {
			IsDemo bool `json:"is_demo"`
		} `json:"centers"`
	}](t, c.owner.do(http.MethodGet, "/api/v1/me", nil)); !me.Centers[0].IsDemo {
		t.Fatalf("/me: %+v", me)
	}
	// Рассылка: предпросмотр работает, отправка — нет.
	body := map[string]any{"audience": "center", "text": "Объявление"}
	if rec := c.owner.do(http.MethodPost, centerPath(c.center, "/broadcasts/preview"), body); rec.Code != http.StatusOK {
		t.Fatalf("демо, предпросмотр: %d %s", rec.Code, rec.Body)
	}
	assertError(t, sendBroadcast(c, "demo-1", body), http.StatusConflict, "demo_center")
}

type remindWAResp struct {
	Sent     int      `json:"sent"`
	Messages int      `json:"messages"`
	WhatsApp []waResp `json:"whatsapp"`
	Skipped  []struct {
		StudentID uuid.UUID `json:"student_id"`
		Reason    string    `json:"reason"`
	} `json:"skipped"`
}

// «Напомнить всем»: Telegram-подключённым — бот; остальным — ссылки; заблокировавшие посреди отправки — тоже в список.
func TestRemindAllWhatsAppQueue(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 12, 0)
	c := newCatalogOn(t, env.api, "+996708400001")
	tg := c.debtor(t, "Бот Подключён", "0708 400 101")
	const chat, chatBlocked = 8400101, 8400102
	linkDirect(t, env.api, tg.Parent.ID, chat)
	none := c.debtor(t, "Без Телеграма", "0708 400 102")
	stopped := c.debtor(t, "Отключил", "0708 400 103")
	linkDirect(t, env.api, stopped.Parent.ID, 8400103)
	setTgStatus(t, env.api, stopped.Parent.ID, "stopped")
	blocked := c.debtor(t, "Заблокировал", "0708 400 104")
	linkDirect(t, env.api, blocked.Parent.ID, chatBlocked)
	env.tg.Block(chatBlocked)
	badPhone := c.debtor(t, "Без Номера", "0708 400 105")
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE parents SET phone = 'нет' WHERE id = $1`, badPhone.Parent.ID); err != nil {
		t.Fatal(err)
	}

	rec := remind(c, map[string]any{"all": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("reminders: %d %s", rec.Code, rec.Body)
	}
	res := decode[remindWAResp](t, rec)
	if res.Messages != 1 || len(env.tg.Messages(chat)) != 1 {
		t.Fatalf("Telegram: %+v", res)
	}
	byName := map[string]waResp{}
	for _, m := range res.WhatsApp {
		byName[m.Students[0].FullName] = m
	}
	if len(byName) != 4 {
		t.Fatalf("очередь WhatsApp: %+v", res.WhatsApp)
	}
	for _, name := range []string{none.FullName, stopped.FullName, blocked.FullName} {
		if m := byName[name]; m.URL == nil || !strings.Contains(m.Text, name) {
			t.Errorf("%s: нет ссылки: %+v", name, m)
		}
	}
	if m := byName[badPhone.FullName]; m.URL != nil || m.Phone != nil {
		t.Errorf("некорректный номер получил ссылку: %+v", m)
	}
	if _, ok := byName[tg.FullName]; ok {
		t.Error("подключённый к Telegram попал в список WhatsApp")
	}

	// Клик из списка — тот же POST по первому ученику; на лимит Telegram не влияет.
	mustWA(t, c, http.MethodPost, none.ID)
	if n, _ := waNotifications(t, env.api, none.Parent.ID); n != 1 {
		t.Fatalf("открытие не записано: %d", n)
	}
	limited := mustRemind(t, c, map[string]any{"student_ids": []uuid.UUID{tg.ID}})
	if limited.reasons()[tg.ID] != "rate_limited" {
		t.Fatalf("лимит Telegram сломан: %+v", limited)
	}
}

type broadcastResp struct {
	ID     uuid.UUID `json:"id"`
	Author string    `json:"author"`
	Text   string    `json:"text"`
	Counts struct {
		TelegramSent   int `json:"telegram_sent"`
		TelegramQueued int `json:"telegram_queued"`
		WhatsAppTotal  int `json:"whatsapp_total"`
		WhatsAppOpened int `json:"whatsapp_opened"`
		NoContact      int `json:"no_contact"`
	} `json:"counts"`
	GroupNames []string `json:"group_names"`
	Links      []struct {
		ParentID   uuid.UUID `json:"parent_id"`
		ParentName string    `json:"parent_name"`
		Students   []string  `json:"students"`
		Text       string    `json:"text"`
		URL        *string   `json:"url"`
		Opened     bool      `json:"opened"`
	} `json:"links"`
}

func sendBroadcast(c *catalog, key string, body map[string]any) *httptest.ResponseRecorder {
	saved := c.owner.headers
	c.owner.headers = map[string]string{"Idempotency-Key": key}
	defer func() { c.owner.headers = saved }()
	return c.owner.do(http.MethodPost, centerPath(c.center, "/broadcasts"), body)
}

func mustBroadcast(t *testing.T, c *catalog, key string, body map[string]any) broadcastResp {
	t.Helper()
	rec := sendBroadcast(c, key, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("broadcast: %d %s", rec.Code, rec.Body)
	}
	return decode[broadcastResp](t, rec)
}

func getBroadcast(t *testing.T, c *catalog, id uuid.UUID) broadcastResp {
	t.Helper()
	rec := c.owner.do(http.MethodGet, centerPath(c.center, "/broadcasts/"+id.String()), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get broadcast: %d %s", rec.Code, rec.Body)
	}
	return decode[broadcastResp](t, rec)
}

// Рассылка: каналы, дети одного родителя — одно сообщение, /stop и блокировка, итоги, история, журнал.
func TestBroadcastChannels(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 21, 0) // рассылку запускает сотрудник — и вне окна 9–20
	c := newCatalogOn(t, env.api, "+996708500001")
	tg := c.debtor(t, "Бот Первый", "0708 500 101")
	c.debtor(t, "Бот Второй", "0708 500 101") // тот же родитель
	const chat, chatBlocked, chatStopLate = 8500101, 8500102, 8500106
	linkDirect(t, env.api, tg.Parent.ID, chat)
	wa := c.student(t, "Без Телеграма", "0708 500 103", nil)
	stopped := c.student(t, "Отключил", "0708 500 104", nil)
	linkDirect(t, env.api, stopped.Parent.ID, 8500104)
	setTgStatus(t, env.api, stopped.Parent.ID, "stopped")
	blocked := c.student(t, "Заблокировал Сейчас", "0708 500 102", nil)
	linkDirect(t, env.api, blocked.Parent.ID, chatBlocked)
	env.tg.Block(chatBlocked)
	late := c.student(t, "Отключил Позже", "0708 500 106", nil)
	linkDirect(t, env.api, late.Parent.ID, chatStopLate)
	noPhone := c.student(t, "Без Контакта", "0708 500 105", nil)
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE parents SET phone = '' WHERE id = $1`, noPhone.Parent.ID); err != nil {
		t.Fatal(err)
	}
	gone := c.student(t, "Ушедший", "0708 500 107", nil)
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+gone.ID.String()), map[string]any{"status": "left"}); rec.Code != http.StatusOK {
		t.Fatalf("left: %d %s", rec.Code, rec.Body)
	}

	body := map[string]any{"audience": "center", "text": "Завтра занятий нет"}
	prev := decode[struct {
		Telegram     int    `json:"telegram"`
		WhatsApp     int    `json:"whatsapp"`
		NoContact    int    `json:"no_contact"`
		TelegramText string `json:"telegram_text"`
		WhatsAppText string `json:"whatsapp_text"`
	}](t, c.owner.do(http.MethodPost, centerPath(c.center, "/broadcasts/preview"), body))
	if prev.Telegram != 3 || prev.WhatsApp != 2 || prev.NoContact != 1 ||
		!strings.HasPrefix(prev.TelegramText, "🏫 Центр «") || !strings.HasPrefix(prev.WhatsAppText, "*🏫 Центр «") {
		t.Fatalf("предпросмотр: %+v", prev)
	}

	b := mustBroadcast(t, c, "b-1", body)
	if b.Counts.TelegramQueued != 3 || b.Counts.WhatsAppTotal != 2 || b.Counts.NoContact != 1 {
		t.Fatalf("создана: %+v", b.Counts)
	}
	setTgStatus(t, env.api, late.Parent.ID, "stopped") // отключился, пока рассылка ждала очереди
	env.dispatch(t)

	msgs := env.tg.Messages(chat)
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Text, "🏫 Центр «") || !strings.HasSuffix(msgs[0].Text, "\n\nЗавтра занятий нет") {
		t.Fatalf("Telegram: %+v", msgs)
	}
	if len(env.tg.Messages(8500104)) != 0 || len(env.tg.Messages(chatStopLate)) != 0 {
		t.Error("написали отключившему /stop")
	}
	d := getBroadcast(t, c, b.ID)
	if d.Counts.TelegramSent != 1 || d.Counts.TelegramQueued != 0 || d.Counts.WhatsAppTotal != 4 || d.Counts.NoContact != 1 {
		t.Fatalf("итоги: %+v", d.Counts)
	}

	// Повторный проход второй раз не шлёт.
	env.dispatch(t)
	if len(env.tg.Messages(chat)) != 1 {
		t.Fatal("сообщение ушло дважды")
	}

	// Ссылка: открытие отмечается один раз.
	var parent uuid.UUID
	for _, l := range d.Links {
		if l.ParentID == wa.Parent.ID {
			parent = l.ParentID
			if l.URL == nil || !strings.Contains(*l.URL, "996708500103") || !strings.HasPrefix(l.Text, "*🏫 Центр «") {
				t.Fatalf("ссылка: %+v", l)
			}
		}
		if l.ParentID == noPhone.Parent.ID && l.URL != nil {
			t.Fatalf("без контакта со ссылкой: %+v", l)
		}
	}
	open := centerPath(c.center, "/broadcasts/"+b.ID.String()+"/recipients/"+parent.String()+"/whatsapp-opened")
	for range 2 {
		if rec := c.owner.do(http.MethodPost, open, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("открыта: %d %s", rec.Code, rec.Body)
		}
	}
	if n, kind := waNotifications(t, env.api, parent); n != 1 || kind != "broadcast" {
		t.Fatalf("notifications: %d %s", n, kind)
	}
	if d := getBroadcast(t, c, b.ID); d.Counts.WhatsAppOpened != 1 {
		t.Fatalf("открыто: %+v", d.Counts)
	}
	if n := auditCount(t, env.api, c.center, "broadcast.send"); n != 1 {
		t.Fatalf("журнал send: %d", n)
	}
	if n := auditCount(t, env.api, c.center, "broadcast.whatsapp_open"); n != 1 {
		t.Fatalf("журнал open: %d", n)
	}
	// Без контакта открыть нельзя.
	assertError(t, c.owner.do(http.MethodPost,
		centerPath(c.center, "/broadcasts/"+b.ID.String()+"/recipients/"+noPhone.Parent.ID.String()+"/whatsapp-opened"), nil), http.StatusNotFound, "not_found")

	hist := decodeItems[broadcastResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/broadcasts"), nil))
	if len(hist) != 1 || hist[0].Text != "Завтра занятий нет" || hist[0].Author == "" || hist[0].Counts.TelegramSent != 1 {
		t.Fatalf("история: %+v", hist)
	}
}

// Двойное нажатие — та же рассылка; вторая за 5 минут — отказ; другой центр лимит не делит.
func TestBroadcastIdempotencyAndRateLimit(t *testing.T) {
	c := newCatalog(t, "+996708600001")
	c.student(t, "Ученик", "0708 600 101", nil)
	body := map[string]any{"audience": "center", "text": "Объявление"}
	b := mustBroadcast(t, c, "key-1", body)

	again := sendBroadcast(c, "key-1", body)
	if again.Code != http.StatusOK || decode[broadcastResp](t, again).ID != b.ID {
		t.Fatalf("повтор: %d %s", again.Code, again.Body)
	}
	assertError(t, sendBroadcast(c, "key-1", map[string]any{"audience": "center", "text": "Другой текст"}), http.StatusUnprocessableEntity, "idempotency_mismatch")
	assertError(t, sendBroadcast(c, "key-2", body), http.StatusTooManyRequests, "broadcast_too_soon")
	if rec := sendBroadcast(c, "", body); rec.Code != http.StatusBadRequest {
		t.Fatalf("без ключа: %d %s", rec.Code, rec.Body)
	}

	other := newCatalogOn(t, c.api, "+996708600002")
	other.student(t, "Ученик Другого", "0708 600 201", nil)
	mustBroadcast(t, other, "key-1", body)

	c.api.clock.Advance(5*time.Minute + time.Second)
	mustBroadcast(t, c, "key-3", body)
	var n int
	if err := c.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM broadcasts WHERE center_id = $1`, c.center).Scan(&n); err != nil || n != 2 {
		t.Fatalf("рассылок: %d (%v)", n, err)
	}
}

func TestBroadcastGroupsAndAccess(t *testing.T) {
	c := newCatalog(t, "+996708700001")
	in := c.debtor(t, "В Группе", "0708 700 101") // debtor записывает в c.group
	c.student(t, "Вне Группы", "0708 700 102", nil)

	b := mustBroadcast(t, c, "g-1", map[string]any{"audience": "groups", "group_ids": []uuid.UUID{c.group.ID}, "text": "Группе"})
	if b.Counts.WhatsAppTotal != 1 || len(b.Links) != 1 || b.Links[0].ParentID != in.Parent.ID || len(b.GroupNames) != 1 {
		t.Fatalf("группа: %+v", b)
	}

	other := newCatalogOn(t, c.api, "+996708700002")
	preview := centerPath(c.center, "/broadcasts/preview")
	assertError(t, c.owner.do(http.MethodPost, preview, map[string]any{"audience": "groups", "group_ids": []uuid.UUID{other.group.ID}, "text": "x"}), http.StatusNotFound, "not_found")
	if rec := c.owner.do(http.MethodPost, preview, map[string]any{"audience": "center", "text": strings.Repeat("я", 1001)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("длинный текст: %d %s", rec.Code, rec.Body)
	}

	empty := newCatalogOn(t, c.api, "+996708700003")
	assertError(t, sendBroadcast(empty, "e-1", map[string]any{"audience": "center", "text": "x"}), http.StatusUnprocessableEntity, "validation")

	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996708700004")
	assertError(t, teacher.do(http.MethodGet, centerPath(c.center, "/broadcasts"), nil), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodPost, preview, map[string]any{"audience": "center", "text": "x"}), http.StatusForbidden, "forbidden")
}

// Бот выключен: писать некому — понятная ошибка; должник с телефоном — в список WhatsApp, рассылка — тоже.
func TestWhatsAppWhenTelegramOff(t *testing.T) {
	c := newCatalog(t, "+996708800001")
	assertError(t, remind(c, map[string]any{"all": true}), http.StatusConflict, "telegram_not_configured")

	s := c.debtor(t, "Без Бота", "0708 800 101")
	linkDirect(t, c.api, s.Parent.ID, 8800101) // подключён, но бот сейчас выключен
	res := decode[remindWAResp](t, remind(c, map[string]any{"all": true}))
	if res.Messages != 0 || len(res.WhatsApp) != 1 || res.WhatsApp[0].URL == nil {
		t.Fatalf("напоминание: %+v", res)
	}
	b := mustBroadcast(t, c, "off-1", map[string]any{"audience": "center", "text": "Объявление"})
	if b.Counts.TelegramQueued != 0 || b.Counts.WhatsAppTotal != 1 {
		t.Fatalf("рассылка: %+v", b.Counts)
	}
}
