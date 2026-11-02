package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/httpapi"
	"okuu/internal/notify"
	"okuu/internal/payments"
	"okuu/internal/token"
)

// fakeProvider — онлайн-провайдер для тестов (вместо Finik): платёж fake-N, вебхук с HMAC-SHA256.
type fakeProvider struct {
	mu      sync.Mutex
	secret  []byte
	created int
	status  map[string]payments.Event // ответы CheckPayment
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{secret: []byte("fake-webhook-secret"), status: map[string]payments.Event{}}
}

func (f *fakeProvider) Name() string { return "finik" }

func (f *fakeProvider) CreatePayment(_ context.Context, _ payments.Center, amount int64, ref string) (payments.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created++
	id := fmt.Sprintf("fake-%d", f.created)
	return payments.Payment{ProviderPaymentID: id, PayURL: "https://pay.example/" + id, QRPayload: "QR:" + id}, nil
}

type fakeWebhook struct {
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
}

func (f *fakeProvider) sign(body []byte) string {
	m := hmac.New(sha256.New, f.secret)
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (f *fakeProvider) ParseWebhook(r *http.Request) (payments.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return payments.Event{}, err
	}
	if !hmac.Equal([]byte(f.sign(body)), []byte(r.Header.Get("X-Signature"))) {
		return payments.Event{}, payments.ErrBadSignature
	}
	var w fakeWebhook
	if err := json.Unmarshal(body, &w); err != nil {
		return payments.Event{}, payments.ErrBadEvent
	}
	return payments.Event{ProviderPaymentID: w.PaymentID, Status: w.Status, Amount: w.Amount}, nil
}

func (f *fakeProvider) CheckPayment(_ context.Context, _ payments.Center, id string) (payments.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.status[id]; ok {
		return ev, nil
	}
	return payments.Event{ProviderPaymentID: id, Status: payments.StatusPending}, nil
}

// webhook — POST /webhooks/finik; badSig — подпись чужим ключом.
func (e *botEnv) webhook(t *testing.T, w fakeWebhook, badSig bool) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(w)
	sig := e.provider.sign(body)
	if badSig {
		m := hmac.New(sha256.New, []byte("attacker"))
		m.Write(body)
		sig = hex.EncodeToString(m.Sum(nil))
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/finik", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	e.api.h.ServeHTTP(rec, req)
	return rec
}

type payPageResp struct {
	CenterName  string    `json:"center_name"`
	StudentName string    `json:"student_name"`
	Amount      int64     `json:"amount"`
	Provider    string    `json:"provider"`
	PayDetails  *string   `json:"pay_details"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type payStartResp struct {
	Provider   string  `json:"provider"`
	Amount     int64   `json:"amount"`
	PayURL     *string `json:"pay_url"`
	QRPayload  *string `json:"qr_payload"`
	PayDetails *string `json:"pay_details"`
}

// newPayLink — ссылка оплаты на долг ученика (онлайн-оплата, ONLINE_PAYMENTS_ENABLED=true). Бот её больше не
// выдаёт — оплата пока ручная; код ссылок остаётся для Finik. Возвращает токен из ссылки.
func newPayLink(t *testing.T, env *botEnv, centerID, sid uuid.UUID) string {
	t.Helper()
	link, err := env.pay.CreateLink(context.Background(), centerID, sid)
	if err != nil {
		t.Fatalf("ссылка оплаты: %v", err)
	}
	tok, ok := strings.CutPrefix(link.URL, testOrigin+"/pay/")
	if !ok {
		t.Fatalf("ссылка оплаты: %q", link.URL)
	}
	return tok
}

func payPage(c *client, tok string) *httptest.ResponseRecorder {
	return c.do(http.MethodGet, "/api/v1/pay/"+tok, nil)
}

func payStart(c *client, tok string) *httptest.ResponseRecorder {
	return c.do(http.MethodPost, "/api/v1/pay/"+tok+"/start", nil)
}

type paymentRow struct {
	ID         uuid.UUID
	Amount     int64
	Method     *string
	ExternalID *string
}

func ledgerPayments(t *testing.T, api *testAPI, sid uuid.UUID) []paymentRow {
	t.Helper()
	rows, err := api.pool.Query(context.Background(),
		`SELECT id, amount, method, external_id FROM ledger WHERE student_id = $1 AND kind = 'payment' ORDER BY created_at, id`, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []paymentRow
	for rows.Next() {
		var r paymentRow
		if err := rows.Scan(&r.ID, &r.Amount, &r.Method, &r.ExternalID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// Ссылка оплаты на долг ребёнка → публичная страница без лишних данных (онлайн-оплата включена).
func TestPayLinkManual(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708300001")
	c.patchSettings(t, map[string]any{"pay_details": "MBank: 0700 000 000"})
	s1 := c.debtor(t, "Асанов Бакыт", "0708 300 101")
	c.debtor(t, "Асанова Айпери", "0708 300 101")
	paid := c.student(t, "Асанов Без Долга", "0708 300 101", nil)

	tok := newPayLink(t, env, c.center, s1.ID)
	var stored, provider string
	var amount int64
	var expires time.Time
	var student uuid.UUID
	if err := env.api.pool.QueryRow(context.Background(),
		`SELECT token, provider, amount, expires_at, student_id FROM payment_links WHERE center_id = $1`, c.center,
	).Scan(&stored, &provider, &amount, &expires, &student); err != nil {
		t.Fatal(err)
	}
	// Срок — 24 часа от создания (часы теста сдвигает ещё и очередь отправки — на секунды).
	if d := expires.Sub(env.api.clock.Now()); stored != token.HashHex(tok) || provider != "manual" || amount != 300_000 ||
		student != s1.ID || d < 24*time.Hour-10*time.Second || d > 24*time.Hour {
		t.Errorf("payment_links: %s %s %d %v %s", stored, provider, amount, expires, student)
	}

	// Публичная страница: ФИО ученика целиком, без данных родителя.
	anon := env.api.client(t, "10.9.0.1")
	anon.origin = testOrigin
	rec := payPage(anon, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("страница: %d %s", rec.Code, rec.Body)
	}
	page := decode[payPageResp](t, rec)
	if page.CenterName != c.centerName(t) || page.StudentName != "Асанов Бакыт" || page.Amount != 300_000 ||
		page.Provider != "manual" || page.PayDetails == nil || *page.PayDetails != "MBank: 0700 000 000" || page.Status != "pending" {
		t.Errorf("страница: %+v", page)
	}
	// ФИО ученика есть, а родителя (имя, телефон), id и других детей — нет.
	for _, leak := range []string{"Родитель", "708300101", "Айпери", s1.ID.String(), s1.Parent.ID.String()} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("на странице лишнее %q: %s", leak, rec.Body)
		}
	}
	start := decode[payStartResp](t, payStart(anon, tok))
	if start.Provider != "manual" || start.PayDetails == nil || start.PayURL != nil || start.Amount != 300_000 {
		t.Errorf("start manual: %+v", start)
	}

	// Неизвестная, кривая и истёкшая ссылка — одинаковый 404.
	unknown, _ := token.New()
	expected := ""
	for _, bad := range []string{unknown, "short", tok} {
		if bad == tok {
			env.api.clock.Advance(24 * time.Hour)
		}
		rec := payPage(anon, bad)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%q: %d %s", bad, rec.Code, rec.Body)
		}
		if expected == "" {
			expected = rec.Body.String()
		} else if rec.Body.String() != expected {
			t.Errorf("ответы 404 различаются: %s vs %s", rec.Body, expected)
		}
		assertError(t, payStart(anon, bad), http.StatusNotFound, "not_found")
	}
	res, err := env.pay.Reconcile(context.Background())
	if err != nil || res.Expired != 1 {
		t.Errorf("reconcile: %+v %v", res, err)
	}

	// Без долга — ссылки нет.
	if _, err := env.pay.CreateLink(context.Background(), c.center, paid.ID); !errors.Is(err, payments.ErrNoDebt) {
		t.Errorf("без долга: %v", err)
	}
	var links int
	if err := env.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM payment_links`).Scan(&links); err != nil || links != 1 {
		t.Errorf("ссылок %d, want 1", links)
	}
}

// Онлайн-провайдер: старт платежа, вебхук записывает оплату один раз, неверная подпись — 401.
func TestPayOnlineWebhook(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708400001")
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE centers SET finik_creds = '\x01' WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	s := c.debtor(t, "Осмонова Мээрим", "0708 400 101")
	u := env.user(t, 8400101)
	linkDirect(t, env.api, s.Parent.ID, u.id)
	tok := newPayLink(t, env, c.center, s.ID)
	anon := env.api.client(t, "10.9.0.2")

	if p := decode[payPageResp](t, payPage(anon, tok)); p.Provider != "finik" || p.PayDetails != nil {
		t.Errorf("страница онлайн: %+v", p)
	}
	st := decode[payStartResp](t, payStart(anon, tok))
	if st.Provider != "finik" || st.PayURL == nil || *st.PayURL != "https://pay.example/fake-1" || st.QRPayload == nil {
		t.Fatalf("start: %+v", st)
	}
	if again := decode[payStartResp](t, payStart(anon, tok)); *again.PayURL != *st.PayURL || env.provider.created != 1 {
		t.Errorf("повторный start создал новый платёж: %+v, создано %d", again, env.provider.created)
	}

	// Неверная подпись — 401, ничего не записано.
	assertError(t, env.webhook(t, fakeWebhook{"fake-1", "paid", 300_000}, true), http.StatusUnauthorized, "bad_signature")
	if n := len(ledgerPayments(t, env.api, s.ID)); n != 0 {
		t.Fatalf("оплата по неверной подписи: %d", n)
	}
	// Не оплачен — ничего.
	if rec := env.webhook(t, fakeWebhook{"fake-1", "failed", 300_000}, false); rec.Code != http.StatusOK {
		t.Fatalf("failed: %d %s", rec.Code, rec.Body)
	}
	// Оплачен — оплата method=finik, ссылка paid, долг закрыт; повтор — без дубля.
	for i := range 2 {
		if rec := env.webhook(t, fakeWebhook{"fake-1", "paid", 300_000}, false); rec.Code != http.StatusOK {
			t.Fatalf("вебхук %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	pays := ledgerPayments(t, env.api, s.ID)
	if len(pays) != 1 || pays[0].Amount != 300_000 || pays[0].Method == nil || *pays[0].Method != "finik" {
		t.Fatalf("оплаты: %+v", pays)
	}
	if b := dbBalance(t, env.api, s.ID); b != 0 {
		t.Errorf("баланс после оплаты %d", b)
	}
	var status string
	var ledgerID *uuid.UUID
	if err := env.api.pool.QueryRow(context.Background(), `SELECT status, ledger_id FROM payment_links WHERE center_id = $1`, c.center).Scan(&status, &ledgerID); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || ledgerID == nil {
		t.Errorf("ссылка: %s %v", status, ledgerID)
	}
	if got := filterAudit(auditLog(t, env.api, c.center), "ledger.payment"); len(got) != 1 || got[0].UserID != nil || got[0].After["method"] != "finik" {
		t.Errorf("журнал: %+v", got)
	}
	// Родителю — «оплата получена» (один раз).
	if n := env.dispatch(t); n != 1 {
		t.Errorf("paid-уведомлений %d", n)
	}
	if m := env.tg.Last(u.id); m.Text != notify.For("ru").Paid(c.centerName(t), "Осмонова Мээрим", 300_000, 0) {
		t.Errorf("уведомление: %q", m.Text)
	}
	if p := decode[payPageResp](t, payPage(anon, tok)); p.Status != "paid" {
		t.Errorf("страница после оплаты: %+v", p)
	}
	assertError(t, payStart(anon, tok), http.StatusConflict, "already_paid")

	// Сумма не совпала — записываем как пришла, предупреждение в журнале.
	c.mustOp(t, s.ID, map[string]any{"kind": "correction", "amount": -100_000, "comment": "доп. занятие"})
	tok2 := newPayLink(t, env, c.center, s.ID)
	payStart(anon, tok2)
	if rec := env.webhook(t, fakeWebhook{"fake-2", "paid", 70_000}, false); rec.Code != http.StatusOK {
		t.Fatalf("вебхук: %d %s", rec.Code, rec.Body)
	}
	if b := dbBalance(t, env.api, s.ID); b != -30_000 {
		t.Errorf("баланс после неполной оплаты %d, want -30000", b)
	}
	if got := filterAudit(auditLog(t, env.api, c.center), "payment.amount_mismatch"); len(got) != 1 ||
		got[0].After["link_amount"] != float64(100_000) || got[0].After["paid_amount"] != float64(70_000) {
		t.Errorf("журнал расхождения: %+v", got)
	}
	// Неизвестный платёж — 200, без записи.
	if rec := env.webhook(t, fakeWebhook{"fake-999", "paid", 1000}, false); rec.Code != http.StatusOK {
		t.Errorf("неизвестный: %d", rec.Code)
	}
	if n := len(ledgerPayments(t, env.api, s.ID)); n != 2 {
		t.Errorf("оплат %d, want 2", n)
	}
	// Оплату Finik отменяет только владелец (R15).
	admin, _ := addStaff(t, env.api, c.owner, c.center, "admin", "+996708400002")
	assertError(t, c.cancel(t, admin, pays[0].ID, "ошибка"), http.StatusForbidden, "forbidden")
}

// payments.reconcile: потерянный вебхук — статус у провайдера; истёкшие — expired.
func TestPayReconcile(t *testing.T) {
	env := newBotEnv(t)
	atBishkek(env.api, 10, 0)
	c := newCatalogOn(t, env.api, "+996708500001")
	if _, err := env.api.pool.Exec(context.Background(), `UPDATE centers SET finik_creds = '\x01' WHERE id = $1`, c.center); err != nil {
		t.Fatal(err)
	}
	s := c.debtor(t, "Исаков Нурбек", "0708 500 101")
	anon := env.api.client(t, "10.9.0.3")
	tok := newPayLink(t, env, c.center, s.ID)
	payStart(anon, tok)                // fake-1
	newPayLink(t, env, c.center, s.ID) // вторая ссылка — платёж не начат

	res, err := env.pay.Reconcile(context.Background())
	if err != nil || res.Checked != 1 || res.Paid != 0 || res.Expired != 0 {
		t.Fatalf("pending у провайдера: %+v %v", res, err)
	}
	env.provider.mu.Lock()
	env.provider.status["fake-1"] = payments.Event{ProviderPaymentID: "fake-1", Status: payments.StatusPaid, Amount: 300_000}
	env.provider.mu.Unlock()
	env.api.clock.Advance(25 * time.Hour) // оплата прошла до истечения, вебхук потерялся
	if res, err = env.pay.Reconcile(context.Background()); err != nil || res.Paid != 1 || res.Expired != 1 {
		t.Fatalf("сверка: %+v %v", res, err)
	}
	if n := len(ledgerPayments(t, env.api, s.ID)); n != 1 {
		t.Errorf("оплат %d", n)
	}
	rows, err := env.api.pool.Query(context.Background(), `SELECT status FROM payment_links WHERE center_id = $1 ORDER BY created_at, id`, c.center)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for rows.Next() {
		var st string
		_ = rows.Scan(&st)
		statuses = append(statuses, st)
	}
	rows.Close()
	if fmt.Sprint(statuses) != "[paid expired]" && fmt.Sprint(statuses) != "[expired paid]" {
		t.Errorf("статусы ссылок: %v", statuses)
	}
	// Повторная сверка ничего не меняет.
	if res, _ := env.pay.Reconcile(context.Background()); res.Paid != 0 || res.Expired != 0 {
		t.Errorf("повтор: %+v", res)
	}
}

// ONLINE_PAYMENTS_ENABLED=false (Payments == nil): любая ссылка — тот же 404, что неизвестная, даже настоящая;
// вебхука нет; /config сообщает фронтенду, что онлайн-оплаты нет.
func TestOnlinePaymentsDisabled(t *testing.T) {
	api := newTestAPIWith(t, func(_ *testAPI, d *httpapi.Deps) { d.Payments = nil })
	c := newCatalogOn(t, api, "+996708600001")
	s := c.debtor(t, "Касымов Тимур", "0708 600 101")
	// Ссылка, созданная раньше (когда оплата была включена), — тоже не работает.
	link, err := payments.NewService(payments.Options{Pool: api.pool, AppOrigin: testOrigin, Now: api.clock.Now}).CreateLink(context.Background(), c.center, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	real := strings.TrimPrefix(link.URL, testOrigin+"/pay/")
	unknown, _ := token.New()
	anon := api.client(t, "10.9.2.1")
	want := payPage(anon, unknown)
	assertError(t, want, http.StatusNotFound, "not_found")
	if got := payPage(anon, real); got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() {
		t.Errorf("настоящая ссылка при выключенной оплате: %d %s", got.Code, got.Body)
	}
	assertError(t, payStart(anon, real), http.StatusNotFound, "not_found")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/finik", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	api.h.ServeHTTP(w, req)
	assertError(t, w, http.StatusNotFound, "not_found")

	if cfg := decode[map[string]bool](t, anon.do(http.MethodGet, "/api/v1/config", nil)); cfg["online_payments"] {
		t.Errorf("config: %v", cfg)
	}
	if cfg := decode[map[string]bool](t, newTestAPI(t).client(t, "10.9.2.2").do(http.MethodGet, "/api/v1/config", nil)); !cfg["online_payments"] {
		t.Errorf("config при включённой оплате: %v", cfg)
	}
}

// Лимит /pay по IP; без онлайн-провайдера маршрута вебхука нет.
func TestPayRateLimitAndNoWebhook(t *testing.T) {
	api := newTestAPI(t)
	a := api.client(t, "10.9.1.1")
	unknown, _ := token.New()
	for i := range 30 {
		if rec := payPage(a, unknown); rec.Code != http.StatusNotFound {
			t.Fatalf("запрос %d: %d", i, rec.Code)
		}
	}
	rec := payPage(a, unknown)
	assertError(t, rec, http.StatusTooManyRequests, "too_many_requests")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("нет Retry-After")
	}
	if rec := payPage(api.client(t, "10.9.1.2"), unknown); rec.Code != http.StatusNotFound {
		t.Errorf("другой IP: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/finik", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	api.h.ServeHTTP(w, req)
	assertError(t, w, http.StatusNotFound, "not_found")
}
