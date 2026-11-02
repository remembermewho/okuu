package httpapi_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/httpapi"
	"okuu/internal/notify"
	"okuu/internal/parents"
	"okuu/internal/payments"
	"okuu/internal/reminders"
	"okuu/internal/telegram"
	"okuu/internal/telegram/bot"
	"okuu/internal/telegram/telegramtest"
	"okuu/internal/token"
)

// Telegram в тестах — только фейковый (telegramtest, httptest). Очередь отправки идёт по часам
// теста: ожидание лимита «1 сообщение в секунду в чат» сдвигает часы, а не ждёт.

const testWebhookSecret = "test_webhook_secret_0123456789"

type botEnv struct {
	api *testAPI
	tg  *telegramtest.Server
	bot *bot.Bot
	rem *reminders.Service // напоминания через ту же очередь отправки
	pay *payments.Service  // оплата: онлайн-провайдер — фейковый (для центров с finik_creds)

	provider *fakeProvider
	langs    notify.Langs // BOT_LANGUAGES этого окружения

	mu         sync.Mutex
	nextUpdate int64
}

// testLogWriter — ошибки бота в лог валят тест: иначе сбой обработки выглядел бы как «бот промолчал».
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Errorf("лог бота: %s", p)
	return len(p), nil
}

// newBotEnv — бот на фейковом Telegram. langs — BOT_LANGUAGES (по умолчанию — только русский, как в prod).
func newBotEnv(t *testing.T, langs ...notify.Langs) *botEnv {
	t.Helper()
	env := &botEnv{}
	var l notify.Langs
	if len(langs) > 0 {
		l = langs[0]
	}
	env.langs = l
	env.api = newTestAPIWith(t, func(api *testAPI, d *httpapi.Deps) {
		env.tg = telegramtest.New(t)
		env.tg.SetNow(api.clock.Now)
		logger := slog.New(slog.NewTextHandler(testLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelError}))
		sender := telegram.NewSender(env.tg.Client(), telegram.SenderOptions{
			Now:       api.clock.Now,
			Sleep:     func(ctx context.Context, d time.Duration) error { api.clock.Advance(d); return ctx.Err() },
			OnBlocked: bot.MarkBlocked(api.pool, logger),
			Logger:    logger,
		})
		env.provider = newFakeProvider()
		env.pay = payments.NewService(payments.Options{Pool: api.pool, Online: env.provider, AppOrigin: testOrigin, Now: api.clock.Now, Logger: logger})
		d.Payments = env.pay
		env.bot = bot.New(bot.Options{Pool: api.pool, Sender: sender, Now: api.clock.Now, Logger: logger, Langs: l})
		env.rem = reminders.NewService(api.pool, sender, api.clock.Now, logger).WithLangs(l)
		d.Reminders = env.rem
		d.Bot = env.bot
		d.TelegramWebhookSecret = testWebhookSecret
	})
	return env
}

// tgUser — родитель в Telegram; в личном чате chat.id == user.id.
type tgUser struct {
	env  *botEnv
	t    *testing.T
	id   int64
	lang string // language_code клиента Telegram
}

func (e *botEnv) user(t *testing.T, id int64) *tgUser { return &tgUser{env: e, t: t, id: id} }

func (u *tgUser) from() *telegram.User {
	return &telegram.User{ID: u.id, FirstName: "Родитель", LanguageCode: u.lang}
}

func (u *tgUser) chat() telegram.Chat { return telegram.Chat{ID: u.id, Type: "private"} }

// deliver обрабатывает апдейт синхронно и возвращает новые сообщения бота в этот чат.
func (u *tgUser) deliver(up telegram.Update) []telegramtest.Call {
	u.t.Helper()
	before := len(u.env.tg.Messages(u.id))
	u.env.mu.Lock()
	u.env.nextUpdate++
	up.UpdateID = u.env.nextUpdate
	u.env.mu.Unlock()
	u.env.bot.Handle(context.Background(), up)
	return u.env.tg.Messages(u.id)[before:]
}

func (u *tgUser) send(text string) []telegramtest.Call {
	u.t.Helper()
	return u.deliver(telegram.Update{Message: &telegram.Message{MessageID: 1, From: u.from(), Chat: u.chat(), Text: text}})
}

// shareContact — контакт с номером phone, принадлежащий пользователю owner (0 — не пользователь Telegram).
func (u *tgUser) shareContact(phone string, owner int64) []telegramtest.Call {
	u.t.Helper()
	return u.deliver(telegram.Update{Message: &telegram.Message{MessageID: 2, From: u.from(), Chat: u.chat(),
		Contact: &telegram.Contact{PhoneNumber: phone, FirstName: "Родитель", UserID: owner}}})
}

// press — нажатие кнопки с данными data под сообщением msg.
func (u *tgUser) press(msg telegramtest.Call, data string) []telegramtest.Call {
	u.t.Helper()
	return u.deliver(telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID: fmt.Sprint("cb", u.id), From: *u.from(), Data: data,
		Message: &telegram.Message{MessageID: msg.MessageID, Chat: u.chat(), Text: msg.Text},
	}})
}

// one — ровно одно новое сообщение.
func one(t *testing.T, msgs []telegramtest.Call) telegramtest.Call {
	t.Helper()
	if len(msgs) != 1 {
		var texts []string
		for _, m := range msgs {
			texts = append(texts, m.Method+": "+m.Text)
		}
		t.Fatalf("ожидалось одно сообщение, получено %d: %q", len(msgs), texts)
	}
	return msgs[0]
}

// callbackData — данные кнопки с подписью label.
func callbackData(t *testing.T, msg telegramtest.Call, label string) string {
	t.Helper()
	for _, b := range msg.Buttons() {
		if text, data, _ := strings.Cut(b, "|"); text == label {
			return data
		}
	}
	t.Fatalf("нет кнопки %q в %v", label, msg.Buttons())
	return ""
}

type parentInviteResp struct {
	ID          uuid.UUID         `json:"id"`
	BotURL      string            `json:"bot_url"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Texts       map[string]string `json:"texts"`
	Lang        string            `json:"lang"`
	WhatsAppURL string            `json:"whatsapp_url"`
	TgStatus    string            `json:"tg_status"`
}

// inviteParent — приглашение от владельца; возвращает ответ и токен из ссылки.
func inviteParent(t *testing.T, c *catalog, parentID uuid.UUID) (parentInviteResp, string) {
	t.Helper()
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/parents/"+parentID.String()+"/invite"), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite parent: %d %s", rec.Code, rec.Body)
	}
	inv := decode[parentInviteResp](t, rec)
	tok, ok := strings.CutPrefix(inv.BotURL, "https://t.me/"+testBotUsername+"?start=")
	if !ok {
		t.Fatalf("bot_url = %q", inv.BotURL)
	}
	return inv, tok
}

type parentTG struct {
	ChatID    *int64
	Status    string
	Lang      string
	ConsentAt *time.Time
}

func parentTGOf(t *testing.T, api *testAPI, parentID uuid.UUID) parentTG {
	t.Helper()
	var p parentTG
	if err := api.pool.QueryRow(context.Background(),
		`SELECT telegram_chat_id, tg_status, lang, consent_at FROM parents WHERE id = $1`, parentID,
	).Scan(&p.ChatID, &p.Status, &p.Lang, &p.ConsentAt); err != nil {
		t.Fatal(err)
	}
	return p
}

// link — полный сценарий привязки: /start <токен> → «Согласен» → свой номер.
func (u *tgUser) link(c *catalog, parentID uuid.UUID, phone string) telegramtest.Call {
	u.t.Helper()
	_, tok := inviteParent(u.t, c, parentID)
	prompt := one(u.t, u.send("/start "+tok))
	// Тексты на языке клиента Telegram, если этот язык включён (BOT_LANGUAGES).
	tx := u.env.langs.For(u.env.langs.Resolve(u.lang))
	afterConsent := u.press(prompt, callbackData(u.t, prompt, tx.ConsentButton))
	if len(afterConsent) != 2 || afterConsent[0].Method != "editMessageText" || afterConsent[1].Text != tx.ContactPrompt {
		u.t.Fatalf("после согласия: %+v", afterConsent)
	}
	return one(u.t, u.shareContact(phone, u.id))
}

// Полный сценарий привязки и всё, что видит администратор.
func TestBotLinkFullScenario(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705100001")
	s1 := c.student(t, "Асанов Бакыт", "0705 111 222", nil)
	s2 := c.student(t, "Асанова Айпери", "+996705111222", nil) // тот же родитель
	pid := s1.Parent.ID
	if s2.Parent.ID != pid {
		t.Fatal("родитель не переиспользован")
	}

	inv, tok := inviteParent(t, c, pid)
	u := env.user(t, 5100001)

	// /start <токен>: центр, дети и текст согласия с кнопкой.
	prompt := one(t, u.send("/start "+tok))
	for _, want := range []string{c.centerName(t), "Асанов Бакыт", "Асанова Айпери", "Согласен", "/stop"} {
		if !strings.Contains(prompt.Text, want) {
			t.Errorf("приглашение без %q: %s", want, prompt.Text)
		}
	}
	consent := callbackData(t, prompt, "Согласен")
	if consent != "consent:"+inv.ID.String() {
		t.Errorf("кнопка согласия = %q", consent)
	}

	// Номер до согласия — сначала согласие.
	if m := one(t, u.shareContact("996705111222", u.id)); m.Text != notify.For("ru").ContactBeforeConsent {
		t.Errorf("номер до согласия: %q", m.Text)
	}

	// «Согласен»: кнопка убрана (правка сообщения), просьба поделиться номером.
	msgs := u.press(prompt, consent)
	if len(msgs) != 2 {
		t.Fatalf("после согласия %d сообщений", len(msgs))
	}
	if msgs[0].Method != "editMessageText" || msgs[0].MessageID != prompt.MessageID || !strings.Contains(msgs[0].Text, "✅") ||
		len(msgs[0].Buttons()) != 0 {
		t.Errorf("правка приглашения: %+v", msgs[0])
	}
	if b := msgs[1].Buttons(); len(b) != 1 || !strings.HasSuffix(b[0], "|contact=true") {
		t.Errorf("кнопка номера: %v", b)
	}
	if p := parentTGOf(t, env.api, pid); p.Status != "none" || p.ChatID != nil {
		t.Errorf("до номера родитель уже привязан: %+v", p)
	}

	// Свой номер, совпадает (Telegram присылает без «+»).
	linked := one(t, u.shareContact("996705111222", u.id))
	for _, want := range []string{"Готово", c.centerName(t), "Асанов Бакыт", "Асанова Айпери", "/balance"} {
		if !strings.Contains(linked.Text, want) {
			t.Errorf("подтверждение без %q: %s", want, linked.Text)
		}
	}
	// Кнопку «Поделиться номером» сменяет постоянная клавиатура-меню.
	if got := fmt.Sprint(linked.Buttons()); got != "[💰 Баланс|contact=false 💳 Как оплатить|contact=false 📅 Посещения|contact=false ❓ Помощь|contact=false]" ||
		!strings.Contains(string(linked.Markup), `"is_persistent":true`) {
		t.Errorf("клавиатура после привязки: %s", linked.Markup)
	}

	p := parentTGOf(t, env.api, pid)
	if p.ChatID == nil || *p.ChatID != u.id || p.Status != "active" || p.ConsentAt == nil || p.Lang != "ru" {
		t.Fatalf("родитель после привязки: %+v", p)
	}
	var usedAt *time.Time
	var states int
	if err := env.api.pool.QueryRow(context.Background(),
		`SELECT (SELECT used_at FROM invites WHERE id = $1), (SELECT count(*) FROM telegram_link_states)`, inv.ID,
	).Scan(&usedAt, &states); err != nil {
		t.Fatal(err)
	}
	if usedAt == nil || states != 0 {
		t.Errorf("приглашение used_at=%v, состояний привязки %d", usedAt, states)
	}
	link := filterAudit(auditLog(t, env.api, c.center), "parent.telegram")
	if len(link) != 1 || link[0].Action != "parent.telegram_link" || link[0].UserID != nil || link[0].After["consent_at"] == nil {
		t.Errorf("журнал привязки: %+v", link)
	}

	// Администратор видит «в Telegram ✓» в карточке и списке; last_reminder_at пока пуст.
	card := c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+s1.ID.String()), nil)
	if !strings.Contains(card.Body.String(), `"tg_status":"active","last_reminder_at":null`) {
		t.Errorf("карточка: %s", card.Body)
	}
	list := decode[studentListResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/students?tg=active"), nil))
	if len(list.Items) != 2 {
		t.Errorf("фильтр tg=active: %d", len(list.Items))
	}

	// Приглашение одноразовое: второй раз (и другим человеком) — недействительно.
	other := env.user(t, 5100002)
	if m := one(t, other.send("/start "+tok)); m.Text != notify.For("ru").InviteInvalid {
		t.Errorf("повторное приглашение: %q", m.Text)
	}
}

// Чужой контакт (переслан, из записной книжки, без user_id) — не подтверждение.
func TestBotForeignContact(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705200001")
	s := c.student(t, "Бекова Айпери", "0705 222 333", nil)
	_, tok := inviteParent(t, c, s.Parent.ID)
	u := env.user(t, 5200001)
	prompt := one(t, u.send("/start "+tok))
	u.press(prompt, callbackData(t, prompt, "Согласен"))

	for name, owner := range map[string]int64{"чужой пользователь": 5200999, "не пользователь Telegram": 0} {
		m := one(t, u.shareContact("996705222333", owner)) // номер даже совпадает
		if m.Text != notify.For("ru").ContactNotOwn {
			t.Errorf("%s: %q", name, m.Text)
		}
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Status != "none" || p.ChatID != nil {
		t.Errorf("привязан по чужому контакту: %+v", p)
	}
	// Свой — проходит.
	if m := one(t, u.shareContact("+996705222333", u.id)); !strings.HasPrefix(m.Text, "Готово") {
		t.Errorf("свой контакт: %q", m.Text)
	}
}

// Неверный номер: вежливый отказ, приглашение остаётся; после исправления номера в центре — проходит.
func TestBotWrongPhone(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705300001")
	s := c.student(t, "Давлетов Эрлан", "0705 333 444", nil)
	inv, tok := inviteParent(t, c, s.Parent.ID)
	u := env.user(t, 5300001)
	prompt := one(t, u.send("/start "+tok))
	u.press(prompt, callbackData(t, prompt, "Согласен"))

	for _, phone := range []string{"996705333999", "+79991234567"} {
		m := one(t, u.shareContact(phone, u.id))
		if m.Text != notify.For("ru").PhoneMismatch(c.centerName(t)) {
			t.Errorf("%s: %q", phone, m.Text)
		}
		if b := m.Buttons(); len(b) != 1 || !strings.HasSuffix(b[0], "|contact=true") {
			t.Errorf("после отказа нет кнопки номера: %v", b)
		}
	}
	var used *time.Time
	if err := env.api.pool.QueryRow(context.Background(), `SELECT used_at FROM invites WHERE id = $1`, inv.ID).Scan(&used); err != nil || used != nil {
		t.Fatalf("приглашение после отказа: used_at=%v err=%v", used, err)
	}

	// Пишет текстом вместо кнопки — напоминаем про кнопку.
	if m := one(t, u.send("мой номер 0705333999")); m.Text != notify.For("ru").ContactPrompt {
		t.Errorf("текст вместо номера: %q", m.Text)
	}

	// Администратор исправил номер — та же ссылка, тот же шаг.
	rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+s.ID.String()), map[string]any{"parent": map[string]string{"phone": "0705 333 999"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if m := one(t, u.shareContact("996705333999", u.id)); !strings.HasPrefix(m.Text, "Готово") {
		t.Errorf("после исправления: %q", m.Text)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Status != "active" {
		t.Errorf("статус = %s", p.Status)
	}

	// Смена номера родителя отвязывает Telegram: подтверждали старым номером.
	rec = c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+s.ID.String()), map[string]any{"parent": map[string]string{"phone": "0705 333 000"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Status != "none" || p.ChatID != nil || p.ConsentAt != nil {
		t.Errorf("после смены номера: %+v", p)
	}
	// Смена только имени — связь остаётся.
	u2 := env.user(t, 5300002)
	u2.link(c, s.Parent.ID, "996705333000")
	rec = c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+s.ID.String()), map[string]any{"parent": map[string]string{"name": "Давлетова Динара"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Status != "active" {
		t.Errorf("после смены имени: %+v", p)
	}
}

// Один чат — родитель в двух центрах: /balance по обоим с названиями, /stop и /start — для обоих.
func TestBotTwoCenters(t *testing.T) {
	env := newBotEnv(t)
	a := newCatalogOn(t, env.api, "+996705400001")
	b := newCatalogOn(t, env.api, "+996705400002")
	sa := a.student(t, "Касымов Тимур", "0705 444 555", nil)
	sb := b.student(t, "Касымова Дана", "0705 444 555", nil)
	b.mustOp(t, sb.ID, map[string]any{"kind": "correction", "amount": -250_000, "comment": "долг"})

	u := env.user(t, 5400001)
	u.link(a, sa.Parent.ID, "996705444555")
	linkedB := u.link(b, sb.Parent.ID, "996705444555")
	if !strings.Contains(linkedB.Text, b.centerName(t)) {
		t.Errorf("подтверждение второго центра: %s", linkedB.Text)
	}
	for _, pid := range []uuid.UUID{sa.Parent.ID, sb.Parent.ID} {
		if p := parentTGOf(t, env.api, pid); p.Status != "active" || *p.ChatID != u.id {
			t.Errorf("родитель %s: %+v", pid, p)
		}
	}

	// Двое детей — сначала выбор; «Все дети» — по обоим центрам с названиями.
	choose := one(t, u.send("/balance"))
	if choose.Text != env.langs.For("ru").ChooseChild || len(choose.Buttons()) != 4 ||
		!strings.Contains(fmt.Sprint(choose.Buttons()), "Касымов Тимур · "+a.centerName(t)) {
		t.Fatalf("выбор ребёнка: %q %v", choose.Text, choose.Buttons())
	}
	m := one(t, u.press(choose, "bal:all"))
	for _, want := range []string{"«" + a.centerName(t) + "»", "Касымов Тимур", "Баланс: 0 сом, долгов нет",
		"«" + b.centerName(t) + "»", "Касымова Дана", "Баланс: долг 2 500 сом"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("/balance без %q:\n%s", want, m.Text)
		}
	}

	stop := one(t, u.send("/stop"))
	if !strings.Contains(stop.Text, a.centerName(t)) || !strings.Contains(stop.Text, b.centerName(t)) {
		t.Errorf("/stop: %s", stop.Text)
	}
	for _, c := range []*catalog{a, b} {
		got := filterAudit(auditLog(t, env.api, c.center), "parent.telegram_stop")
		if len(got) != 1 {
			t.Errorf("журнал /stop в %s: %+v", c.centerName(t), got)
		}
	}
	resumed := one(t, u.send("/start"))
	if resumed.Text != notify.For("ru").Resumed([]string{a.centerName(t), b.centerName(t)}) &&
		resumed.Text != notify.For("ru").Resumed([]string{b.centerName(t), a.centerName(t)}) {
		t.Errorf("/start после /stop: %s", resumed.Text)
	}
	for _, pid := range []uuid.UUID{sa.Parent.ID, sb.Parent.ID} {
		if p := parentTGOf(t, env.api, pid); p.Status != "active" {
			t.Errorf("после /start: %+v", p)
		}
	}
	// Повторный /start — просто приветствие со списком центров.
	if m := one(t, u.send("/start")); !strings.HasPrefix(m.Text, "Вы подключены") {
		t.Errorf("приветствие: %s", m.Text)
	}
}

// /balance: долг, абонемент и посещения за месяц; кнопка «Баланс и посещения» делает то же.
func TestBotBalance(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705500001")
	sid := packageStudent(t, c, "Осмонова Мээрим", "0705 555 666") // абонемент 8 занятий: начисление 4 000 сом
	lesson := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "07:00"})
	mustMark(t, c, c.owner, lesson.ID, sid, "present")
	c.mustOp(t, sid, pay(100_000))
	left := c.student(t, "Ушедший Ученик", "0705 555 666", nil)
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/students/"+left.ID.String()), map[string]any{"status": "left"}); rec.Code != http.StatusOK {
		t.Fatalf("left: %d %s", rec.Code, rec.Body)
	}

	u := env.user(t, 5500001)
	if m := one(t, u.send("/balance")); m.Text != notify.For("ru").NotLinked {
		t.Errorf("/balance до привязки: %q", m.Text)
	}
	var parentID uuid.UUID
	if err := env.api.pool.QueryRow(context.Background(), `SELECT parent_id FROM students WHERE id = $1`, sid).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	u.link(c, parentID, "996705555666")

	m := one(t, u.send("/balance"))
	want := strings.Join([]string{
		"🏫 Центр «" + c.centerName(t) + "»",
		"",
		"👤 Осмонова Мээрим",
		"Баланс: долг 3 000 сом",
		"Абонемент (Английский A1): осталось 7 занятий",
		notify.For("ru").Attendance(int(c.today.Month()), 1, 0, 0),
	}, "\n")
	if m.Text != want {
		t.Errorf("/balance:\n%s\nwant:\n%s", m.Text, want)
	}
	if strings.Contains(m.Text, "Ушедший") {
		t.Error("ушедший без долга показан")
	}
	// Кнопок оплаты нет: при долге — «Как оплатить» (реквизиты центра).
	if fmt.Sprint(m.Buttons()) != "[💳 Как оплатить|howtopay « Меню|menu]" {
		t.Errorf("кнопки баланса: %v", m.Buttons())
	}
	// Кнопка «Баланс и посещения» из напоминаний делает то же.
	if again := one(t, u.press(m, "balance")); again.Text != want {
		t.Errorf("кнопка баланса: %s", again.Text)
	}
	// Кнопка «💰 Баланс» постоянной клавиатуры — тоже.
	if again := one(t, u.send("💰 Баланс")); again.Text != want {
		t.Errorf("кнопка меню «Баланс»: %s", again.Text)
	}
}

// /lang (BOT_LANGUAGES=ru,ky): язык меняется во всех центрах чата; /help — на выбранном языке.
func TestBotLang(t *testing.T) {
	env := newBotEnv(t, notify.MustLangs("ru,ky"))
	c := newCatalogOn(t, env.api, "+996705600001")
	s := c.student(t, "Токтогулов Арсен", "0705 666 777", nil)
	u := env.user(t, 5600001)
	u.link(c, s.Parent.ID, "996705666777")

	prompt := one(t, u.send("/lang"))
	if prompt.Text != notify.LangPrompt || fmt.Sprint(prompt.Buttons()) != "[Русский|lang:ru Кыргызча|lang:ky]" {
		t.Fatalf("/lang: %q %v", prompt.Text, prompt.Buttons())
	}
	changed := one(t, u.press(prompt, "lang:ky"))
	if changed.Method != "editMessageText" || changed.Text != notify.For("ky").LangChanged {
		t.Errorf("смена языка: %+v", changed)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Lang != "ky" {
		t.Errorf("parents.lang = %s", p.Lang)
	}
	if m := one(t, u.send("/help")); m.Text != notify.For("ky").Help {
		t.Errorf("/help после ky: %q", m.Text)
	}
	if m := one(t, u.send("/balance")); !strings.Contains(m.Text, "Баланс: 0 сом, карыз жок") {
		t.Errorf("/balance на ky: %s", m.Text)
	}
	// Непривязанный клиент с кыргызским Telegram получает кыргызские тексты.
	ky := env.user(t, 5600002)
	ky.lang = "ky"
	if m := one(t, ky.send("/start")); m.Text != notify.For("ky").StartHint {
		t.Errorf("/start ky: %q", m.Text)
	}
}

// Навигация: постоянная клавиатура, «Как оплатить», /attendance, выбор ребёнка, «« Меню», подсказка на непонятный текст.
func TestBotNavigation(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705660001")
	debt := c.debtor(t, "Асанов Бакыт", "0705 666 001") // месяц: долг 3 000
	calm := c.student(t, "Асанова Айпери", "0705 666 001", nil)
	c.enroll(t, calm.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})
	lesson := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "07:00"})
	mustMark(t, c, c.owner, lesson.ID, debt.ID, "present", calm.ID, "absent")
	u := env.user(t, 5660001)
	linkDirect(t, env.api, debt.Parent.ID, u.id)
	tx := env.langs.For("ru")

	// «💳 Как оплатить» — реквизиты центра (не заданы — «уточните у администратора») и «« Меню».
	howTo := "🏫 Центр «" + c.centerName(t) + "»\n" + tx.HowToPayHeader + "\n" + tx.PayDetailsMissing
	payMsg := one(t, u.send("💳 Как оплатить"))
	if payMsg.Text != howTo || fmt.Sprint(payMsg.Buttons()) != "[« Меню|menu]" {
		t.Errorf("«Как оплатить»: %q %v", payMsg.Text, payMsg.Buttons())
	}
	// Старая кнопка «💳 Оплатить» постоянной клавиатуры и набранная /pay — то же.
	for _, text := range []string{"💳 Оплатить", "/pay"} {
		if m := one(t, u.send(text)); m.Text != howTo {
			t.Errorf("%s: %q", text, m.Text)
		}
	}

	// /attendance — двое детей: выбор (каждый, «Все дети», «« Меню»).
	choose := one(t, u.send("/attendance"))
	if choose.Text != tx.ChooseChild || len(choose.Buttons()) != 4 {
		t.Fatalf("выбор ребёнка: %q %v", choose.Text, choose.Buttons())
	}
	att := one(t, u.press(choose, callbackData(t, choose, "Асанова Айпери")))
	month := int(c.today.Month())
	wantOne := tx.AttendanceHeader(month) + "\n\n" + tx.AttendanceChild("Асанова Айпери", c.centerName(t), 0, 1, 0) +
		"\n" + tx.AttendanceMark(c.today, "absent", "")
	if att.Text != wantOne || fmt.Sprint(att.Buttons()) != "[« Меню|menu]" {
		t.Errorf("посещения ребёнка:\n%s\n%v", att.Text, att.Buttons())
	}
	all := one(t, u.press(choose, "att:all"))
	if !strings.Contains(all.Text, tx.AttendanceChild("Асанов Бакыт", c.centerName(t), 1, 0, 0)) || !strings.Contains(all.Text, "Асанова Айпери") {
		t.Errorf("посещения всех:\n%s", all.Text)
	}
	// Чужой ребёнок в данных кнопки — снова выбор, без чужих данных.
	other := c.student(t, "Чужой Ребёнок", "0705 666 999", nil)
	if m := one(t, u.press(choose, "att:"+other.ID.String())); m.Text != tx.ChooseChild || strings.Contains(fmt.Sprint(m.Buttons()), "Чужой") {
		t.Errorf("чужой ребёнок: %q %v", m.Text, m.Buttons())
	}

	// /balance — выбор, затем один ребёнок.
	balChoose := one(t, u.send("/balance"))
	bal := one(t, u.press(balChoose, "bal:"+calm.ID.String()))
	if !strings.Contains(bal.Text, "👤 Асанова Айпери") || strings.Contains(bal.Text, "Асанов Бакыт") || fmt.Sprint(bal.Buttons()) != "[« Меню|menu]" {
		t.Errorf("баланс ребёнка:\n%s\n%v", bal.Text, bal.Buttons())
	}

	// «« Меню» — разделы inline-кнопками; каждая ведёт в свой раздел.
	menu := one(t, u.press(bal, "menu"))
	if menu.Text != tx.MenuPrompt || fmt.Sprint(menu.Buttons()) != "[💰 Баланс|bal: 💳 Как оплатить|howtopay 📅 Посещения|att: ❓ Помощь|help]" {
		t.Fatalf("меню: %q %v", menu.Text, menu.Buttons())
	}
	if m := one(t, u.press(menu, "bal:")); m.Text != tx.ChooseChild {
		t.Errorf("меню → баланс: %q", m.Text)
	}
	if m := one(t, u.press(menu, "howtopay")); m.Text != howTo {
		t.Errorf("меню → как оплатить: %q", m.Text)
	}
	if m := one(t, u.press(menu, "help")); m.Text != tx.Help || !strings.Contains(string(m.Markup), `"is_persistent":true`) {
		t.Errorf("меню → помощь: %q %s", m.Text, m.Markup)
	}

	// Непонятный текст — короткая подсказка и постоянная клавиатура.
	unk := one(t, u.send("а когда занятие?"))
	if unk.Text != tx.Unknown || len(unk.Buttons()) != 4 {
		t.Errorf("непонятный текст: %q %v", unk.Text, unk.Buttons())
	}

	// Оплатили — в балансе кнопки «Как оплатить» больше нет.
	c.mustOp(t, debt.ID, pay(300_000))
	if m := one(t, u.press(one(t, u.send("/balance")), "bal:all")); strings.Contains(fmt.Sprint(m.Buttons()), "howtopay") {
		t.Errorf("баланс без долга: %v", m.Buttons())
	}

	// Не привязан: «Как оплатить» и /attendance — как получить ссылку у центра.
	stranger := env.user(t, 5660009)
	for _, cmd := range []string{"💳 Как оплатить", "/attendance", "📅 Посещения"} {
		if m := one(t, stranger.send(cmd)); m.Text != tx.NotLinked {
			t.Errorf("%s без привязки: %q", cmd, m.Text)
		}
	}
}

// setMyCommands: /start, /balance, /pay, /attendance, /help, /stop с русскими описаниями; /lang — только с ky.
func TestBotCommandsMenu(t *testing.T) {
	tg := telegramtest.New(t)
	ctx := context.Background()
	if err := bot.SetupCommands(ctx, tg.Client(), notify.Langs{}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetupCommands(ctx, tg.Client(), notify.MustLangs("ru,ky")); err != nil {
		t.Fatal(err)
	}
	var raws []string
	for _, c := range tg.Calls() {
		if c.Method == "setMyCommands" {
			raws = append(raws, string(c.Raw))
		}
	}
	if len(raws) != 2 {
		t.Fatalf("setMyCommands: %d вызовов", len(raws))
	}
	for _, cmd := range []string{`"start"`, `"balance"`, `"attendance"`, `"help"`, `"stop"`, "Посещения за месяц"} {
		if !strings.Contains(raws[0], cmd) {
			t.Errorf("в меню нет %s: %s", cmd, raws[0])
		}
	}
	// Оплаты в боте нет — и команды /pay в меню тоже.
	if strings.Contains(raws[0], `"pay"`) || strings.Contains(raws[0], "Оплатить") {
		t.Errorf("в меню осталась /pay: %s", raws[0])
	}
	if strings.Contains(raws[0], `"lang"`) || !strings.Contains(raws[1], `"lang"`) {
		t.Errorf("/lang: ru=%s ru,ky=%s", raws[0], raws[1])
	}
}

// BOT_LANGUAGES=ru (по умолчанию): команды /lang нет, кнопок языка нет, всё по-русски — даже если у родителя
// сохранён lang=ky или у клиента Telegram кыргызский; старая кнопка выбора языка ничего не делает.
func TestBotRussianOnly(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705650001")
	s := c.student(t, "Токтогулов Арсен", "0705 665 777", nil)
	u := env.user(t, 5650001)
	u.lang = "ky" // кыргызский Telegram
	linked := u.link(c, s.Parent.ID, "996705665777")
	if strings.Contains(linked.Text, "/lang") || !strings.Contains(linked.Text, "Готово!") {
		t.Errorf("привязка: %q", linked.Text)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Lang != "ru" {
		t.Errorf("parents.lang = %s, want ru", p.Lang)
	}
	// Выбор ky, сохранённый раньше (когда кыргызский был включён), не действует.
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE parents SET lang = 'ky' WHERE id = $1`, s.Parent.ID); err != nil {
		t.Fatal(err)
	}
	m := one(t, u.send("/lang"))
	if m.Text != env.langs.For("ru").Unknown {
		t.Errorf("/lang в режиме ru: %q", m.Text)
	}
	for _, b := range m.Buttons() {
		if strings.Contains(b, "lang:") {
			t.Errorf("кнопка выбора языка: %v", m.Buttons())
		}
	}
	if m := one(t, u.send("/help")); m.Text != env.langs.For("ru").Help || strings.Contains(m.Text, "/lang") {
		t.Errorf("/help: %q", m.Text)
	}
	if got := u.press(m, "lang:ky"); len(got) != 0 {
		t.Errorf("старая кнопка языка ответила: %+v", got)
	}
	if p := parentTGOf(t, env.api, s.Parent.ID); p.Lang != "ky" {
		t.Errorf("сохранённый выбор не должен меняться: %s", p.Lang)
	}
	// Напоминание о долге — тоже по-русски.
	c.mustOp(t, s.ID, map[string]any{"kind": "correction", "amount": -100_000, "comment": "Долг за сентябрь"})
	if rec := c.owner.do(http.MethodPost, centerPath(c.center, "/reminders"), map[string]any{"student_ids": []uuid.UUID{s.ID}}); rec.Code != http.StatusOK {
		t.Fatalf("reminders: %d %s", rec.Code, rec.Body)
	}
	if m := env.tg.Last(u.id); !strings.Contains(m.Text, "напоминание об оплате") || !strings.Contains(m.Text, "1 000 сом") {
		t.Errorf("напоминание: %q", m.Text)
	}
}

// /stop (R18): статус stopped виден администратору; без привязки — подсказка.
func TestBotStop(t *testing.T) {
	env := newBotEnv(t)
	c := newCatalogOn(t, env.api, "+996705700001")
	s := c.student(t, "Сыдыков Арген", "0705 777 888", nil)

	stranger := env.user(t, 5700009)
	if m := one(t, stranger.send("/stop")); m.Text != notify.For("ru").NotLinked {
		t.Errorf("/stop без привязки: %q", m.Text)
	}
	if m := one(t, stranger.send("/start")); m.Text != notify.For("ru").StartHint {
		t.Errorf("/start без привязки: %q", m.Text)
	}
	// Непривязанному на любой текст — как получить ссылку у центра.
	if m := one(t, stranger.send("привет")); m.Text != notify.For("ru").NotLinked {
		t.Errorf("текст: %q", m.Text)
	}
	if m := one(t, stranger.send("/start not-a-valid-token!")); m.Text != notify.For("ru").InviteInvalid {
		t.Errorf("кривой токен: %q", m.Text)
	}

	u := env.user(t, 5700001)
	u.link(c, s.Parent.ID, "996705777888")
	if m := one(t, u.send("/stop@"+testBotUsername)); m.Text != notify.For("ru").Stopped([]string{c.centerName(t)}) {
		t.Errorf("/stop: %q", m.Text)
	}
	card := decode[studentResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+s.ID.String()), nil))
	if card.Parent.TgStatus != "stopped" {
		t.Errorf("карточка: tg_status = %s", card.Parent.TgStatus)
	}
	// /balance после /stop работает: это запрос самого родителя.
	if m := one(t, u.send("/balance")); !strings.Contains(m.Text, "Сыдыков Арген") {
		t.Errorf("/balance после /stop: %s", m.Text)
	}
}

// 403 от Telegram (R19): связь blocked у всех родителей чата; родитель снова написал — active.
func TestBotBlocked(t *testing.T) {
	env := newBotEnv(t)
	a := newCatalogOn(t, env.api, "+996705800001")
	b := newCatalogOn(t, env.api, "+996705800002")
	sa := a.student(t, "Жумабаев Нурс", "0705 888 999", nil)
	sb := b.student(t, "Жумабаева Аяна", "0705 888 999", nil)
	u := env.user(t, 5800001)
	u.link(a, sa.Parent.ID, "996705888999")
	u.link(b, sb.Parent.ID, "996705888999")

	env.tg.Block(u.id)
	if msgs := u.send("/balance"); len(msgs) != 0 {
		t.Fatalf("сообщение дошло заблокировавшему: %v", msgs)
	}
	for _, pid := range []uuid.UUID{sa.Parent.ID, sb.Parent.ID} {
		if p := parentTGOf(t, env.api, pid); p.Status != "blocked" {
			t.Errorf("после 403: %+v", p)
		}
	}
	list := decode[studentListResp](t, a.owner.do(http.MethodGet, centerPath(a.center, "/students?tg=blocked"), nil))
	if len(list.Items) != 1 {
		t.Errorf("фильтр tg=blocked: %d", len(list.Items))
	}

	env.tg.Unblock(u.id)
	if m := one(t, u.send("/help")); m.Text != env.langs.For("ru").Help {
		t.Errorf("/help: %q", m.Text)
	}
	for _, pid := range []uuid.UUID{sa.Parent.ID, sb.Parent.ID} {
		if p := parentTGOf(t, env.api, pid); p.Status != "active" {
			t.Errorf("после разблокировки: %+v", p)
		}
	}
}

// 429 в боте: ответ уходит после retry_after; несколько ответов в один чат — не чаще раза в секунду.
func TestBotRateLimits(t *testing.T) {
	env := newBotEnv(t, notify.MustLangs("ru,ky")) // /lang → кнопка: два сообщения подряд
	u := env.user(t, 5900001)
	env.tg.Fail("sendMessage", u.id, http.StatusTooManyRequests, "Too Many Requests: retry after 3", 3, 1)
	m := one(t, u.send("/help"))
	var failedAt time.Time
	for _, c := range env.tg.Calls() {
		if c.Method == "sendMessage:error" && c.ChatID == u.id {
			failedAt = c.At
		}
	}
	if failedAt.IsZero() || m.At.Sub(failedAt) != 3*time.Second {
		t.Errorf("повтор после 429 через %v, want 3s", m.At.Sub(failedAt))
	}

	// /lang → кнопка: правка сообщения идёт не раньше чем через секунду после /lang.
	prompt := one(t, u.send("/lang"))
	edit := one(t, u.press(prompt, "lang:ru"))
	if d := edit.At.Sub(prompt.At); d < time.Second {
		t.Errorf("два сообщения в чат через %v", d)
	}
}

// Вебхук: секрет в пути и в заголовке; апдейт обрабатывается в очереди, ответ — сразу.
func TestTelegramWebhook(t *testing.T) {
	env := newBotEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go env.bot.Run(ctx, ctx)

	const chat = 6000001
	update := fmt.Sprintf(`{"update_id":1,"message":{"message_id":1,"from":{"id":%d,"first_name":"Т"},"chat":{"id":%d,"type":"private"},"date":1,"text":"/start","unknown_field":{"x":1}}}`, chat, chat)
	post := func(path, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(update))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set("X-Telegram-Bot-Api-Secret-Token", header)
		}
		rec := httptest.NewRecorder()
		env.api.h.ServeHTTP(rec, req)
		return rec
	}

	assertError(t, post("/webhooks/telegram/wrong_secret_0123456789", testWebhookSecret), http.StatusNotFound, "not_found")
	assertError(t, post("/webhooks/telegram/"+testWebhookSecret, ""), http.StatusNotFound, "not_found")
	assertError(t, post("/webhooks/telegram/"+testWebhookSecret, "wrong_secret_0123456789"), http.StatusNotFound, "not_found")
	if len(env.tg.Messages(chat)) != 0 {
		t.Fatal("апдейт с неверным секретом обработан")
	}
	if rec := post("/webhooks/telegram/"+testWebhookSecret, testWebhookSecret); rec.Code != http.StatusOK {
		t.Fatalf("вебхук: %d %s", rec.Code, rec.Body)
	}
	msg := waitMessage(t, env.tg, chat)
	if msg.Text != notify.For("ru").StartHint {
		t.Errorf("ответ: %q", msg.Text)
	}

	// Без бота (режим polling) маршрута нет.
	plain := newTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/"+testWebhookSecret, strings.NewReader(update))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", testWebhookSecret)
	rec := httptest.NewRecorder()
	plain.h.ServeHTTP(rec, req)
	assertError(t, rec, http.StatusNotFound, "not_found")
}

// Polling: вебхук снимается, апдейты из getUpdates доходят до бота.
func TestBotPolling(t *testing.T) {
	env := newBotEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go env.bot.Run(ctx, ctx)
	go bot.Poll(ctx, env.tg.Client(), env.bot, slog.New(slog.DiscardHandler))

	const chat = 6100001
	env.tg.PushUpdate(telegram.Update{UpdateID: 10, Message: &telegram.Message{MessageID: 1,
		From: &telegram.User{ID: chat}, Chat: telegram.Chat{ID: chat, Type: "private"}, Text: "/help"}})
	if m := waitMessage(t, env.tg, chat); m.Text != env.langs.For("ru").Help {
		t.Errorf("ответ: %q", m.Text)
	}
	calls := env.tg.Calls()
	if calls[0].Method != "setWebhook" || !strings.Contains(string(calls[0].Raw), `"url":""`) {
		t.Errorf("первый вызов: %s %s", calls[0].Method, calls[0].Raw)
	}
	// Сообщения из групп бот игнорирует.
	env.tg.PushUpdate(telegram.Update{UpdateID: 11, Message: &telegram.Message{MessageID: 2,
		From: &telegram.User{ID: chat}, Chat: telegram.Chat{ID: -100500, Type: "group"}, Text: "/help"}})
	env.tg.PushUpdate(telegram.Update{UpdateID: 12, Message: &telegram.Message{MessageID: 3,
		From: &telegram.User{ID: chat}, Chat: telegram.Chat{ID: chat, Type: "private"}, Text: "/help"}})
	waitMessages(t, env.tg, chat, 2)
	if len(env.tg.Messages(-100500)) != 0 {
		t.Error("бот ответил в группе")
	}
}

func waitMessage(t *testing.T, tg *telegramtest.Server, chat int64) telegramtest.Call {
	t.Helper()
	return waitMessages(t, tg, chat, 1)[0]
}

func waitMessages(t *testing.T, tg *telegramtest.Server, chat int64, n int) []telegramtest.Call {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m := tg.Messages(chat); len(m) >= n {
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("за 5 секунд в чат %d не пришло %d сообщений", chat, n)
	return nil
}

// POST /parents/{pid}/invite: ссылка с токеном (в БД — только hex(sha256)), тексты ru/ky и WhatsApp.
func TestParentInviteEndpoint(t *testing.T) {
	api := newTestAPI(t)
	c := newCatalogOn(t, api, "+996706000001")
	s := c.student(t, "Нурланова Дана", "0706 000 111", nil)

	inv, tok := inviteParent(t, c, s.Parent.ID)
	if len(tok) != 43 {
		t.Errorf("токен %d символов, want 43 (32 байта base64url)", len(tok))
	}
	if d := time.Until(inv.ExpiresAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("срок %v, want 30 дней", d)
	}
	if inv.Lang != "ru" || inv.TgStatus != "none" {
		t.Errorf("lang=%s tg_status=%s", inv.Lang, inv.TgStatus)
	}
	var stored, kind string
	var parentID uuid.UUID
	if err := api.pool.QueryRow(context.Background(), `SELECT token, kind, parent_id FROM invites WHERE id = $1`, inv.ID).Scan(&stored, &kind, &parentID); err != nil {
		t.Fatal(err)
	}
	if stored != token.HashHex(tok) || kind != "parent" || parentID != s.Parent.ID {
		t.Errorf("в БД: token=%s kind=%s parent=%s", stored, kind, parentID)
	}
	if _, err := hex.DecodeString(stored); err != nil || stored == tok {
		t.Error("в БД должен быть hex(sha256), а не токен")
	}

	for lang, text := range inv.Texts {
		if !strings.Contains(text, inv.BotURL) || !strings.Contains(text, c.centerName(t)) || !strings.Contains(text, "Родитель Нурланова Дана") {
			t.Errorf("текст %s: %s", lang, text)
		}
	}
	if len(inv.Texts) != 1 || inv.Texts["ru"] == "" { // бот только на русском — и приглашение (TestParentInviteLangs)
		t.Errorf("тексты: %v", inv.Texts)
	}
	wa, err := url.Parse(inv.WhatsAppURL)
	if err != nil || wa.Host != "wa.me" || wa.Path != "/996706000111" || wa.Query().Get("text") != inv.Texts["ru"] {
		t.Errorf("whatsapp_url = %s", inv.WhatsAppURL)
	}
	if strings.Contains(wa.RawQuery, "+") {
		t.Errorf("пробелы в whatsapp_url — «+»: %s", wa.RawQuery)
	}
	created := filterAudit(auditLog(t, api, c.center), "invite.")
	if len(created) != 1 || created[0].After["kind"] != "parent" || created[0].After["parent_id"] != s.Parent.ID.String() {
		t.Errorf("журнал: %+v", created)
	}

	// Второе приглашение не отменяет первое.
	inv2, tok2 := inviteParent(t, c, s.Parent.ID)
	if inv2.ID == inv.ID || tok2 == tok {
		t.Error("второе приглашение совпало с первым")
	}

	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/parents/"+uuid.NewString()+"/invite"), nil), http.StatusNotFound, "not_found")
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/parents/not-a-uuid/invite"), nil), http.StatusNotFound, "not_found")

	// Админ может, преподаватель — нет.
	admin, _ := addStaff(t, api, c.owner, c.center, "admin", "+996706000002")
	if rec := admin.do(http.MethodPost, centerPath(c.center, "/parents/"+s.Parent.ID.String()+"/invite"), nil); rec.Code != http.StatusCreated {
		t.Errorf("админ: %d %s", rec.Code, rec.Body)
	}
	teacher, _ := addStaff(t, api, c.owner, c.center, "teacher", "+996706000003")
	assertError(t, teacher.do(http.MethodPost, centerPath(c.center, "/parents/"+s.Parent.ID.String()+"/invite"), nil), http.StatusForbidden, "forbidden")
}

func parentsWithoutBot(api *testAPI) *parents.Service { return parents.NewService(api.pool, "") }

// centerName — название центра каталога (из БД).
func (c *catalog) centerName(t *testing.T) string {
	t.Helper()
	var name string
	if err := c.api.pool.QueryRow(context.Background(), `SELECT name FROM centers WHERE id = $1`, c.center).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestParentInviteBotNotConfigured(t *testing.T) {
	api := newTestAPIWith(t, func(api *testAPI, d *httpapi.Deps) {
		d.Parents = parentsWithoutBot(api)
	})
	c := newCatalogOn(t, api, "+996706100001")
	s := c.student(t, "Без Бота", "0706 100 111", nil)
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/parents/"+s.Parent.ID.String()+"/invite"), nil),
		http.StatusConflict, "telegram_not_configured")
}
