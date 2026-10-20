package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/attendance"
	"okuu/internal/auth"
	"okuu/internal/billing"
	"okuu/internal/db/sqlc"
	"okuu/internal/groups"
	"okuu/internal/httpapi"
	"okuu/internal/httpx"
	"okuu/internal/importer"
	"okuu/internal/parents"
	"okuu/internal/reports"
	"okuu/internal/settings"
	"okuu/internal/staff"
	"okuu/internal/students"
	"okuu/internal/tariffs"
	"okuu/internal/testdb"
)

const testOrigin = "http://localhost:5173"

// clock — управляемые часы для лимита попыток и сроков сессий.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testAPI struct {
	h     http.Handler
	auth  *auth.Service
	pool  *pgxpool.Pool
	clock *clock
}

// testBotUsername — имя бота в ссылках-приглашениях тестов.
const testBotUsername = "okuu_test_bot"

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	return newTestAPIWith(t, nil)
}

// newTestAPIWith — тестовый API; setup может поменять зависимости до сборки роутера
// (пул и часы в api уже есть).
func newTestAPIWith(t *testing.T, setup func(api *testAPI, d *httpapi.Deps)) *testAPI {
	t.Helper()
	pool := testdb.New(t)
	clk := &clock{t: time.Now()}
	svc := auth.NewService(pool, auth.Options{Now: clk.Now})
	st, gr := students.NewService(pool, clk.Now), groups.NewService(pool)
	api := &testAPI{auth: svc, pool: pool, clock: clk}
	d := httpapi.Deps{DB: pool, Auth: svc, Staff: staff.NewService(pool, svc),
		Tariffs: tariffs.NewService(pool), Groups: gr, Students: st,
		Billing: billing.NewService(pool, clk.Now, nil), Attendance: attendance.NewService(pool, clk.Now),
		Importer: importer.NewService(pool, st, gr, clk.Now),
		Settings: settings.NewService(pool, clk.Now), Reports: reports.NewService(pool, clk.Now),
		Parents: parents.NewService(pool, testBotUsername), Pool: pool,
		AppOrigin: testOrigin}
	if setup != nil {
		setup(api, &d)
	}
	api.h = httpapi.NewRouter(d)
	return api
}

// client — браузер: хранит cookie, шлёт Origin на изменяющих запросах, имеет свой IP.
type client struct {
	t       *testing.T
	h       http.Handler
	ip      string
	origin  string
	cookies map[string]*http.Cookie
	headers map[string]string // добавляются к каждому запросу (например, Idempotency-Key)
}

func (api *testAPI) client(t *testing.T, ip string) *client {
	return &client{t: t, h: api.h, ip: ip, origin: testOrigin, cookies: map[string]*http.Cookie{}}
}

func (c *client) do(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.RemoteAddr = c.ip + ":40000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okuu-test")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if method != http.MethodGet && c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck
		}
	}
	return rec
}

func (c *client) sessionToken() string {
	if ck := c.cookies[auth.SessionCookie]; ck != nil {
		return ck.Value
	}
	return ""
}

type registerReq struct {
	Phone      string `json:"phone"`
	Password   string `json:"password"`
	Name       string `json:"name"`
	CenterName string `json:"center_name"`
}

type loginReq struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

type meResp struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Phone   string    `json:"phone"`
	Centers []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
		Role string    `json:"role"`
	} `json:"centers"`
}

func (c *client) register(phone, center string) meResp {
	c.t.Helper()
	rec := c.do(http.MethodPost, "/api/v1/auth/register", registerReq{
		Phone: phone, Password: "secret-pass", Name: "Владелец " + center, CenterName: center,
	})
	if rec.Code != http.StatusCreated {
		c.t.Fatalf("register %s: %d %s", phone, rec.Code, rec.Body)
	}
	return decode[meResp](c.t, rec)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return v
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	body := decode[httpx.ErrorBody](t, rec)
	if body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("error = %+v, want code %q", body.Error, code)
	}
}

func TestRegisterMeLogout(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")

	rec := c.do(http.MethodPost, "/api/v1/auth/register", registerReq{
		Phone: "0700 123 456", Password: "secret-pass", Name: " Айгуль ", CenterName: "Билим",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	reg := decode[meResp](t, rec)
	if reg.Phone != "+996700123456" || reg.Name != "Айгуль" || len(reg.Centers) != 1 ||
		reg.Centers[0].Name != "Билим" || reg.Centers[0].Role != "owner" {
		t.Fatalf("register response = %+v", reg)
	}

	// Cookie: только токен, HttpOnly, SameSite=Lax, Path=/, 30 дней, без Secure вне prod.
	ck := c.cookies[auth.SessionCookie]
	if ck == nil {
		t.Fatal("нет cookie сессии")
	}
	if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Secure ||
		ck.MaxAge < int((30*24*time.Hour-time.Minute).Seconds()) || ck.MaxAge > int((30*24*time.Hour).Seconds()) {
		t.Errorf("cookie = %+v", ck)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(ck.Value); err != nil || len(raw) != 32 {
		t.Errorf("токен не 32 байта base64url: %q", ck.Value)
	}

	// В БД — sha256 токена, а не сам токен; центр создан с телефоном владельца.
	h := sha256.Sum256([]byte(ck.Value))
	var n int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE token_hash = $1 AND user_agent = 'okuu-test'`, h[:]).Scan(&n); err != nil || n != 1 {
		t.Fatalf("сессия по sha256 токена: n=%d err=%v", n, err)
	}
	var centerPhone string
	if err := api.pool.QueryRow(context.Background(), `SELECT phone FROM centers WHERE id = $1`, reg.Centers[0].ID).
		Scan(&centerPhone); err != nil || centerPhone != "+996700123456" {
		t.Errorf("телефон центра = %q, %v", centerPhone, err)
	}

	rec = c.do(http.MethodGet, "/api/v1/me", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	me := decode[meResp](t, rec)
	if me.ID != reg.ID || me.Phone != "+996700123456" || len(me.Centers) != 1 {
		t.Fatalf("me = %+v", me)
	}
	// DTO, а не модель sqlc: никаких password_hash и прочих внутренних полей.
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, k := range []string{"password_hash", "telegram_id", "created_at"} {
		if _, ok := raw[k]; ok {
			t.Errorf("в /me утекло поле %q", k)
		}
	}

	oldToken := c.sessionToken()
	rec = c.do(http.MethodPost, "/api/v1/auth/logout", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	if c.sessionToken() != "" {
		t.Error("cookie не очищена после logout")
	}
	assertError(t, c.do(http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "unauthorized")

	// Старый токен после logout тоже не работает.
	c.cookies[auth.SessionCookie] = &http.Cookie{Name: auth.SessionCookie, Value: oldToken}
	assertError(t, c.do(http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "unauthorized")

	// И вход обратно.
	rec = c.do(http.MethodPost, "/api/v1/auth/login", loginReq{Phone: "+996700123456", Password: "secret-pass"})
	if rec.Code != http.StatusOK || c.sessionToken() == oldToken {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if c.do(http.MethodGet, "/api/v1/me", nil).Code != http.StatusOK {
		t.Fatal("me после login")
	}
}

func TestRegisterPhoneTaken(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700000001", "Первый")

	// Тот же номер в другом формате.
	rec := c.do(http.MethodPost, "/api/v1/auth/register", registerReq{
		Phone: "0700000001", Password: "other-pass", Name: "Кто-то", CenterName: "Второй",
	})
	assertError(t, rec, http.StatusConflict, "phone_taken")

	var users, centers, members int
	err := api.pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM centers), (SELECT count(*) FROM memberships)`).
		Scan(&users, &centers, &members)
	if err != nil || users != 1 || centers != 1 || members != 1 {
		t.Errorf("после отказа: users=%d centers=%d memberships=%d err=%v; транзакция должна откатиться",
			users, centers, members, err)
	}
}

func TestRegisterValidation(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")

	ok := registerReq{Phone: "0700000002", Password: "secret-pass", Name: "Имя", CenterName: "Центр"}
	tests := []struct {
		name   string
		mutate func(r *registerReq)
	}{
		{"телефон не KG", func(r *registerReq) { r.Phone = "+7 999 123 45 67" }},
		{"телефон с буквами", func(r *registerReq) { r.Phone = "0700abc456" }},
		{"короткий пароль", func(r *registerReq) { r.Password = "1234567" }},
		{"пустое имя", func(r *registerReq) { r.Name = "   " }},
		{"пустой центр", func(r *registerReq) { r.CenterName = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ok
			tt.mutate(&req)
			assertError(t, c.do(http.MethodPost, "/api/v1/auth/register", req), http.StatusBadRequest, "validation")
		})
	}

	t.Run("имя не в UTF-8", func(t *testing.T) {
		// "Смоук" в cp1251 — так шлёт curl из консоли Windows.
		body := []byte(`{"phone":"0700000002","password":"secret-pass","name":"` + "\xd1\xec\xee\xf3\xea" + `","center_name":"Центр"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Origin", testOrigin)
		rec := httptest.NewRecorder()
		api.h.ServeHTTP(rec, req)
		assertError(t, rec, http.StatusBadRequest, "validation")
	})

	t.Run("неизвестное поле", func(t *testing.T) {
		rec := c.do(http.MethodPost, "/api/v1/auth/register", map[string]string{"phone": "0700000002", "role": "owner"})
		assertError(t, rec, http.StatusBadRequest, "bad_request")
	})

	var users int
	_ = api.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&users)
	if users != 0 {
		t.Errorf("создано %d пользователей при ошибках валидации", users)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	api := newTestAPI(t)
	api.client(t, "10.0.0.1").register("+996700000003", "Центр")

	wrongPass := api.client(t, "10.0.0.2").do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700000003", Password: "wrong-pass"})
	unknownPhone := api.client(t, "10.0.0.3").do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700999999", Password: "secret-pass"})

	assertError(t, wrongPass, http.StatusUnauthorized, "invalid_credentials")
	assertError(t, unknownPhone, http.StatusUnauthorized, "invalid_credentials")
	if wrongPass.Body.String() != unknownPhone.Body.String() {
		t.Errorf("ответы различаются — выдают, есть ли номер:\n%s\n%s", wrongPass.Body, unknownPhone.Body)
	}
	if len(wrongPass.Result().Cookies()) != 0 {
		t.Error("cookie выдана при неверном пароле")
	}

	assertError(t, api.client(t, "10.0.0.4").do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "12345", Password: "x"}), http.StatusBadRequest, "validation")
}

func TestLoginRateLimitByPhone(t *testing.T) {
	api := newTestAPI(t)
	api.client(t, "10.0.0.1").register("+996700000004", "Центр")

	// 5 неудач с разных IP — срабатывает лимит по телефону.
	for i := range auth.LoginMaxFails {
		c := api.client(t, "10.1.0."+strconv.Itoa(i+1))
		assertError(t, c.do(http.MethodPost, "/api/v1/auth/login",
			loginReq{Phone: "0700000004", Password: "wrong-pass"}), http.StatusUnauthorized, "invalid_credentials")
	}

	// Даже верный пароль с нового IP — 429, cookie не выдаётся.
	c := api.client(t, "10.2.0.1")
	rec := c.do(http.MethodPost, "/api/v1/auth/login", loginReq{Phone: "+996700000004", Password: "secret-pass"})
	assertError(t, rec, http.StatusTooManyRequests, "too_many_attempts")
	if ra, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || ra <= 0 || ra > 15*60 {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	if c.sessionToken() != "" {
		t.Error("cookie выдана при 429")
	}

	// Через 15 минут — снова можно.
	api.clock.Advance(auth.LoginWindow)
	if rec := c.do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700000004", Password: "secret-pass"}); rec.Code != http.StatusOK {
		t.Fatalf("после окна: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginRateLimitByIP(t *testing.T) {
	api := newTestAPI(t)
	api.client(t, "10.0.0.1").register("+996700000005", "Центр")

	// С одного IP перебирают разные номера — у каждого номера по 1 неудаче, но IP исчерпал лимит.
	attacker := api.client(t, "10.9.9.9")
	for i := range auth.LoginMaxFails {
		assertError(t, attacker.do(http.MethodPost, "/api/v1/auth/login",
			loginReq{Phone: "+99670011100" + strconv.Itoa(i), Password: "guess-pass"}), http.StatusUnauthorized, "invalid_credentials")
	}
	assertError(t, attacker.do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700000005", Password: "secret-pass"}), http.StatusTooManyRequests, "too_many_attempts")

	// С другого IP владелец входит.
	if rec := api.client(t, "10.0.0.1").do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700000005", Password: "secret-pass"}); rec.Code != http.StatusOK {
		t.Fatalf("другой IP: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginDeletesExpiredSessions(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	me := c.register("+996700000006", "Центр")

	ctx := context.Background()
	if _, err := api.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ('\x00', $1, now() - interval '1 day'), ('\x01', $1, now() - interval '1 minute')`, me.ID); err != nil {
		t.Fatal(err)
	}
	if rec := c.do(http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996700000006", Password: "secret-pass"}); rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var expired, live int
	_ = api.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE expires_at <= now()), count(*) FILTER (WHERE expires_at > now())
		FROM sessions`).Scan(&expired, &live)
	if expired != 0 || live != 2 {
		t.Errorf("после login: просроченных %d (want 0), живых %d (want 2: register + login)", expired, live)
	}
}

func TestExpiredSessionUnauthorized(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700000007", "Центр")

	h := sha256.Sum256([]byte(c.sessionToken()))
	if _, err := api.pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, h[:]); err != nil {
		t.Fatal(err)
	}
	assertError(t, c.do(http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "unauthorized")

	// Мусорный токен и отсутствие cookie — тоже 401.
	c.cookies[auth.SessionCookie] = &http.Cookie{Name: auth.SessionCookie, Value: "garbage"}
	assertError(t, c.do(http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "unauthorized")
	assertError(t, api.client(t, "10.0.0.1").do(http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "unauthorized")
}

func TestSessionExtendedAtMostDaily(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700000008", "Центр")
	h := sha256.Sum256([]byte(c.sessionToken()))
	ctx := context.Background()

	expiresAt := func() time.Time {
		var e time.Time
		if err := api.pool.QueryRow(ctx, `SELECT expires_at FROM sessions WHERE token_hash = $1`, h[:]).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}

	// Активность в первые сутки — не продлевает и cookie не трогает.
	before := expiresAt()
	api.clock.Advance(23 * time.Hour)
	rec := c.do(http.MethodGet, "/api/v1/me", nil)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 || !expiresAt().Equal(before) {
		t.Fatalf("продление раньше суток: code=%d cookies=%v", rec.Code, rec.Result().Cookies())
	}

	// Прошли сутки — продлеваем на 30 дней от «сейчас» и обновляем cookie.
	api.clock.Advance(2 * time.Hour)
	rec = c.do(http.MethodGet, "/api/v1/me", nil)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("продление после суток: code=%d cookies=%v", rec.Code, rec.Result().Cookies())
	}
	after := expiresAt()
	want := api.clock.Now().Add(auth.SessionTTL)
	if d := after.Sub(want); d < -time.Second || d > time.Second {
		t.Errorf("expires_at = %v, want ≈ %v", after, want)
	}

	// Сразу следующий запрос — уже без продления.
	if rec := c.do(http.MethodGet, "/api/v1/me", nil); len(rec.Result().Cookies()) != 0 || !expiresAt().Equal(after) {
		t.Error("повторное продление в те же сутки")
	}
}

func TestOriginCheck(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700000009", "Центр")

	for _, origin := range []string{"", "https://evil.example", "http://localhost:5174", "null"} {
		t.Run("Origin="+origin, func(t *testing.T) {
			bad := api.client(t, "10.0.0.2")
			bad.origin = origin
			for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/logout"} {
				assertError(t, bad.do(http.MethodPost, path, loginReq{Phone: "+996700000009", Password: "secret-pass"}),
					http.StatusForbidden, "bad_origin")
			}
		})
	}

	// Logout с чужим Origin не убивает сессию.
	evil := *c
	evil.origin = "https://evil.example"
	assertError(t, evil.do(http.MethodPost, "/api/v1/auth/logout", nil), http.StatusForbidden, "bad_origin")
	if c.do(http.MethodGet, "/api/v1/me", nil).Code != http.StatusOK {
		t.Error("сессия пропала после logout с чужим Origin")
	}

	// GET без Origin разрешён.
	c.origin = ""
	if rec := c.do(http.MethodGet, "/api/v1/me", nil); rec.Code != http.StatusOK {
		t.Errorf("GET без Origin: %d", rec.Code)
	}
}

// RequireCenter проверяем на тестовых маршрутах: настоящие /c/{centerId}/... появятся на шагах 5–7,
// и тест изоляции для каждого из них добавится там.
func TestRequireCenter(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerA := owner.register("+996700000010", "Центр А").Centers[0].ID
	stranger := api.client(t, "10.0.0.2")
	centerB := stranger.register("+996700000011", "Центр Б").Centers[0].ID
	teacher := api.client(t, "10.0.0.3")
	teacherID := teacher.register("+996700000012", "Свой центр учителя").ID

	if _, err := sqlc.New(api.pool).CreateMembership(context.Background(), sqlc.CreateMembershipParams{
		UserID: teacherID, CenterID: centerA, Role: auth.RoleTeacher,
	}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(api.auth.RequireAuth)
	echo := func(w http.ResponseWriter, r *http.Request) {
		m, _ := auth.MembershipFrom(r.Context())
		httpx.JSON(w, http.StatusOK, map[string]string{"center_id": m.CenterID.String(), "role": m.Role})
	}
	r.With(api.auth.RequireCenter(auth.RoleOwner)).Get("/c/{centerId}/owner-only", echo)
	r.With(api.auth.RequireCenter(auth.RoleOwner, auth.RoleAdmin, auth.RoleTeacher)).Get("/c/{centerId}/any", echo)
	for _, c := range []*client{owner, stranger, teacher} {
		c.h = r
	}

	type echoResp struct {
		CenterID string `json:"center_id"`
		Role     string `json:"role"`
	}
	tests := []struct {
		name     string
		c        *client
		path     string
		status   int
		code     string
		wantRole string
	}{
		{"владелец в своём центре", owner, "/c/" + centerA.String() + "/owner-only", 200, "", "owner"},
		{"чужой центр → 404", owner, "/c/" + centerB.String() + "/owner-only", 404, "not_found", ""},
		{"чужой центр, любой роли → 404", owner, "/c/" + centerB.String() + "/any", 404, "not_found", ""},
		{"несуществующий центр → 404", owner, "/c/" + uuid.NewString() + "/any", 404, "not_found", ""},
		{"не uuid → 404", owner, "/c/not-a-uuid/any", 404, "not_found", ""},
		{"учитель на owner-эндпоинт → 403", teacher, "/c/" + centerA.String() + "/owner-only", 403, "forbidden", ""},
		{"учитель на общий эндпоинт", teacher, "/c/" + centerA.String() + "/any", 200, "", "teacher"},
		{"B в центре A → 404", stranger, "/c/" + centerA.String() + "/any", 404, "not_found", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := tt.c.do(http.MethodGet, tt.path, nil)
			if tt.code != "" {
				assertError(t, rec, tt.status, tt.code)
				return
			}
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; %s", rec.Code, tt.status, rec.Body)
			}
			got := decode[echoResp](t, rec)
			if got.CenterID != centerA.String() || got.Role != tt.wantRole {
				t.Errorf("membership в context = %+v", got)
			}
		})
	}

	anon := api.client(t, "10.0.0.4")
	anon.h = r
	assertError(t, anon.do(http.MethodGet, "/c/"+centerA.String()+"/any", nil), http.StatusUnauthorized, "unauthorized")
}

// itemsResp — единый формат списков API.
type itemsResp[T any] struct {
	Items      *[]T    `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// decodeItems разбирает список {items, next_cursor} без пагинации: items — массив (не null),
// next_cursor — null.
func decodeItems[T any](t *testing.T, rec *httptest.ResponseRecorder) []T {
	t.Helper()
	body := decode[itemsResp[T]](t, rec)
	if body.Items == nil {
		t.Fatalf("items отсутствует или null: %s", rec.Body)
	}
	if body.NextCursor != nil {
		t.Fatalf("next_cursor = %q, want null для короткого списка", *body.NextCursor)
	}
	return *body.Items
}
