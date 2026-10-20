package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"

	"okuu/internal/alerts"
	"okuu/internal/audit"
	"okuu/internal/auth"
	"okuu/internal/db/sqlc"
	"okuu/internal/httpapi"
	"okuu/internal/telegram"
	"okuu/internal/token"
)

// testProxy — адрес «Caddy» в тестах: от него верим X-Forwarded-For.
var testProxies = []netip.Prefix{netip.MustParsePrefix("192.168.100.0/24")}

// Лимит попыток входа по IP за доверенным прокси считает клиентов по X-Forwarded-For;
// от остальных заголовок не учитывается — его не подделать.
func TestLoginRateLimitBehindProxy(t *testing.T) {
	api := newTestAPIWith(t, func(_ *testAPI, d *httpapi.Deps) { d.TrustedProxies = testProxies })
	api.client(t, "10.0.0.1").register("+996709000001", "Центр")

	viaProxy := func(client string) *client {
		c := api.client(t, "192.168.100.2")
		c.headers = map[string]string{"X-Forwarded-For": client}
		return c
	}
	attacker := viaProxy("203.0.113.10")
	for i := range auth.LoginMaxFails {
		assertError(t, attacker.do(http.MethodPost, "/api/v1/auth/login",
			loginReq{Phone: "+99670900010" + strconv.Itoa(i), Password: "guess-pass"}), http.StatusUnauthorized, "invalid_credentials")
	}
	assertError(t, attacker.do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996709000001", Password: "secret-pass"}), http.StatusTooManyRequests, "too_many_attempts")
	// Другой клиент за тем же прокси — не заблокирован.
	if rec := viaProxy("203.0.113.11").do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996709000001", Password: "secret-pass"}); rec.Code != http.StatusOK {
		t.Fatalf("другой клиент за прокси: %d %s", rec.Code, rec.Body)
	}

	// Напрямую (не прокси): подделанный X-Forwarded-For не помогает обойти лимит.
	direct := api.client(t, "10.9.9.9")
	for i := range auth.LoginMaxFails {
		direct.headers = map[string]string{"X-Forwarded-For": "198.51.100." + strconv.Itoa(i)}
		direct.do(http.MethodPost, "/api/v1/auth/login", loginReq{Phone: "+99670900020" + strconv.Itoa(i), Password: "guess-pass"})
	}
	direct.headers = map[string]string{"X-Forwarded-For": "198.51.100.99"}
	assertError(t, direct.do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996709000001", Password: "secret-pass"}), http.StatusTooManyRequests, "too_many_attempts")
}

// Лимит страницы оплаты — тоже по клиенту за прокси.
func TestPayRateLimitBehindProxy(t *testing.T) {
	api := newTestAPIWith(t, func(_ *testAPI, d *httpapi.Deps) { d.TrustedProxies = testProxies })
	unknown, _ := token.New()
	a := api.client(t, "192.168.100.2")
	a.headers = map[string]string{"X-Forwarded-For": "203.0.113.20"}
	for range 30 {
		payPage(a, unknown)
	}
	assertError(t, payPage(a, unknown), http.StatusTooManyRequests, "too_many_requests")
	b := api.client(t, "192.168.100.2")
	b.headers = map[string]string{"X-Forwarded-For": "203.0.113.21"}
	if rec := payPage(b, unknown); rec.Code != http.StatusNotFound {
		t.Errorf("другой клиент за прокси: %d", rec.Code)
	}
}

func TestSecurityHeadersAndRequestID(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996709000101", "Центр")

	rec := c.do(http.MethodGet, "/api/v1/me", nil)
	h := rec.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cache-Control") != "no-store" {
		t.Errorf("заголовки API: %v", h)
	}
	if id := h.Get("X-Request-ID"); len(id) != 16 {
		t.Errorf("X-Request-ID = %q", id)
	}
	// Ошибки — с теми же заголовками.
	if h := c.do(http.MethodGet, "/api/v1/nope", nil).Header(); h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("заголовки 404: %v", h)
	}
	// /healthz — не API: nosniff есть, no-store нет.
	if h := c.do(http.MethodGet, "/healthz", nil).Header(); h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "" {
		t.Errorf("заголовки /healthz: %v", h)
	}

	// Свой id от прокси принимаем, мусорный — заменяем.
	for in, keep := range map[string]bool{"abc-123_x.y": true, "bad id with spaces": false, strings.Repeat("a", 65): false} {
		c.headers = map[string]string{"X-Request-ID": in}
		got := c.do(http.MethodGet, "/api/v1/me", nil).Header().Get("X-Request-ID")
		if (got == in) != keep || got == "" {
			t.Errorf("X-Request-ID %q → %q", in, got)
		}
	}
}

func TestBodyLimits(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	center := c.register("+996709000201", "Центр").Centers[0].ID

	big := bytes.Repeat([]byte("x"), 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(big))
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	api.h.ServeHTTP(rec, req)
	assertError(t, rec, http.StatusRequestEntityTooLarge, "too_large")

	// Без Content-Length (chunked) — обрезается на 1 МБ, разбор тела падает.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", struct{ *bytes.Reader }{bytes.NewReader(big)})
	req.ContentLength = -1
	req.Header.Set("Origin", testOrigin)
	rec = httptest.NewRecorder()
	api.h.ServeHTTP(rec, req)
	assertError(t, rec, http.StatusBadRequest, "bad_request")

	// Импорт: 5,5 МБ проходят лимит запроса и упираются в лимит файла (5 МБ) обработчика.
	rec = c.upload(centerPath(center, "/import/preview"), "big.csv", bytes.Repeat([]byte("a;b\n"), 5<<18+1<<16))
	assertError(t, rec, http.StatusRequestEntityTooLarge, "file_too_large")
}

func TestReadyz(t *testing.T) {
	ready := newTestAPIWith(t, func(_ *testAPI, d *httpapi.Deps) {
		d.Ready = func(context.Context) map[string]error { return map[string]error{"scheduler": nil, "migrations": nil} }
	})
	rec := ready.client(t, "10.0.0.1").do(http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Errorf("readyz: %d %s", rec.Code, rec.Body)
	}

	notReady := newTestAPIWith(t, func(_ *testAPI, d *httpapi.Deps) {
		d.Ready = func(context.Context) map[string]error {
			return map[string]error{"scheduler": errTest("планировщик не запущен"), "migrations": nil}
		}
	})
	rec = notReady.client(t, "10.0.0.1").do(http.MethodGet, "/readyz", nil)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Status != "not_ready" || body.Checks["db"] != "ok" ||
		body.Checks["scheduler"] != "планировщик не запущен" || body.Checks["migrations"] != "ok" {
		t.Errorf("readyz без планировщика: %d %s", rec.Code, rec.Body)
	}
	if rec := notReady.client(t, "10.0.0.1").do(http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("healthz проверяет только БД: %d", rec.Code)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// stubQueue — очередь апдейтов бота, которая только принимает.
type stubQueue struct{}

func (stubQueue) Enqueue(context.Context, telegram.Update) error { return nil }

// syncBuffer — буфер для логов, безопасный для одновременной записи.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// В логах нет паролей, телефонов, cookie, токенов и секретов — ни в журнале запросов, ни в ошибках.
func TestLogsHaveNoSecrets(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	const webhookSecret = "log_test_webhook_secret_0123456789"
	api := newTestAPIWith(t, func(api *testAPI, d *httpapi.Deps) {
		d.Logger = logger
		d.Auth = auth.NewService(api.pool, auth.Options{Now: api.clock.Now, Logger: logger})
		d.Bot, d.TelegramWebhookSecret = stubQueue{}, webhookSecret
	})

	const (
		phone    = "+996701234567"
		password = "very-secret-pass-1"
		wrong    = "wrong-pass-zzz-9"
	)
	owner := api.client(t, "10.0.0.1")
	rec := owner.do(http.MethodPost, "/api/v1/auth/register", registerReq{Phone: phone, Password: password, Name: "Владелец", CenterName: "Логи"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	center := decode[meResp](t, rec).Centers[0].ID
	cookie := owner.sessionToken()
	requestIDs := []string{rec.Header().Get("X-Request-ID")}

	stranger := api.client(t, "10.0.0.2")
	requestIDs = append(requestIDs, stranger.do(http.MethodPost, "/api/v1/auth/login", loginReq{Phone: "0701 234 567", Password: wrong}).Header().Get("X-Request-ID"))
	owner.do(http.MethodGet, centerPath(center, "/students?q=0701234567"), nil)
	owner.do(http.MethodPost, centerPath(center, "/students"), map[string]any{"full_name": "Ученик", "parent": map[string]string{"name": "Родитель", "phone": "0702 765 432"}})
	_, inviteToken := createInvite(t, owner, center, "admin", "")
	stranger.do(http.MethodGet, "/api/v1/invites/"+inviteToken, nil)
	stranger.do(http.MethodPost, "/api/v1/invites/"+inviteToken+"/accept", map[string]string{"phone": "0703 111 222", "password": "accept-pass-777"})
	payToken, _ := token.New()
	stranger.do(http.MethodGet, "/api/v1/pay/"+payToken, nil)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/"+webhookSecret, strings.NewReader(`{"update_id":1}`))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", webhookSecret)
	api.h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	for name, secret := range map[string]string{
		"пароль": password, "неверный пароль": wrong, "пароль сотрудника": "accept-pass-777", "телефон": "701234567",
		"телефон родителя": "702765432", "телефон сотрудника": "703111222", "cookie": cookie,
		"токен приглашения": inviteToken, "токен оплаты": payToken, "секрет вебхука": webhookSecret,
	} {
		if secret == "" {
			t.Fatalf("%s: пусто", name)
		}
		if strings.Contains(out, secret) {
			t.Errorf("в логах %s: %s", name, secret)
		}
	}

	// Журнал запросов: строка на каждый запрос с request_id, шаблоном маршрута, статусом и временем.
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["msg"] == "http" {
			lines = append(lines, m)
		}
	}
	if len(lines) < 8 {
		t.Fatalf("строк журнала запросов %d:\n%s", len(lines), out)
	}
	routes := map[string]bool{}
	ids := map[string]bool{}
	for _, l := range lines {
		for _, k := range []string{"request_id", "method", "route", "status", "duration_ms"} {
			if _, ok := l[k]; !ok {
				t.Errorf("нет %s в %v", k, l)
			}
		}
		routes[l["route"].(string)] = true
		ids[l["request_id"].(string)] = true
	}
	for _, want := range []string{"/api/v1/auth/register", "/api/v1/pay/{token}", "/api/v1/invites/{token}", "/webhooks/telegram/{secret}",
		"/api/v1/c/{centerId}/students"} {
		if !routes[want] {
			t.Errorf("нет маршрута %s в журнале: %v", want, routes)
		}
	}
	for _, id := range requestIDs {
		if !ids[id] {
			t.Errorf("X-Request-ID %s нет в журнале", id)
		}
	}
}

// fakeAlertSender — служебный чат для алертов.
type fakeAlertSender struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeAlertSender) Send(_ context.Context, p telegram.SendMessageParams) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, p.Text)
	return telegram.Message{}, nil
}

// Алерты по журналу: скидка больше цены и оплата не на сумму ссылки.
func TestAlertsFromAudit(t *testing.T) {
	c := newCatalog(t, "+996709000301")
	q := sqlc.New(c.api.pool)
	last, err := q.MaxAuditID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := c.student(t, "Скидочный Ученик", "0709 000 302", &discountJSON{"fixed", 400_000}) // скидка больше цены 3 000
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "full"})
	if err := audit.Record(context.Background(), c.api.pool, audit.Entry{CenterID: c.center, Action: audit.PaymentAmountMismatch,
		Entity: "payment_link", After: map[string]any{"link_amount": 300_000, "paid_amount": 70_000, "external_id": "fake-9"}}); err != nil {
		t.Fatal(err)
	}

	sender := &fakeAlertSender{}
	a := alerts.New(alerts.Options{Sender: sender, ChatID: -100, Env: "test", Logger: slog.New(slog.DiscardHandler)})
	if last, err = a.CheckAudit(context.Background(), q, last); err != nil {
		t.Fatal(err)
	}
	a.Wait()
	if len(sender.texts) != 2 {
		t.Fatalf("алертов %d: %q", len(sender.texts), sender.texts)
	}
	joined := strings.Join(sender.texts, "\n")
	for _, want := range []string{"скидка больше цены", "на 700 сом, а ссылка была на 3 000 сом", c.centerName(t), "fake-9"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в алертах нет %q:\n%s", want, joined)
		}
	}
	// Повторная проверка с того же места — без новых алертов.
	if _, err := a.CheckAudit(context.Background(), q, last); err != nil {
		t.Fatal(err)
	}
	a.Wait()
	if len(sender.texts) != 2 {
		t.Errorf("повтор: %d", len(sender.texts))
	}
}
