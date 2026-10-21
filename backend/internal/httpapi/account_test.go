package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/token"
)

type resetLinkResp struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type publicResetResp struct {
	Name       string  `json:"name"`
	CenterName *string `json:"center_name"`
}

// login — новый «браузер» входит телефоном и паролем; ok=false — вход не удался.
func login(t *testing.T, api *testAPI, ip, phone, password string) (*client, bool) {
	t.Helper()
	c := api.client(t, ip)
	rec := c.do(http.MethodPost, "/api/v1/auth/login", loginReq{Phone: phone, Password: password})
	return c, rec.Code == http.StatusOK
}

func meOK(c *client) bool { return c.do(http.MethodGet, "/api/v1/me", nil).Code == http.StatusOK }

// noSecretsInAudit — в журнале нет паролей и токенов.
func noSecretsInAudit(t *testing.T, api *testAPI, centerID uuid.UUID, secrets ...string) {
	t.Helper()
	for _, r := range auditLog(t, api, centerID) {
		b, _ := json.Marshal(r)
		for _, s := range secrets {
			if s != "" && strings.Contains(string(b), s) {
				t.Errorf("секрет %q попал в журнал: %s", s, b)
			}
		}
	}
}

func TestPatchMeName(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700810001", "Центр")
	rec := c.do(http.MethodPatch, "/api/v1/me", map[string]string{"name": "  Айгуль Асанова "})
	if rec.Code != http.StatusOK || decode[meResp](t, rec).Name != "Айгуль Асанова" {
		t.Fatalf("patch me: %d %s", rec.Code, rec.Body)
	}
	assertField(t, c.do(http.MethodPatch, "/api/v1/me", map[string]string{"name": " "}), http.StatusBadRequest, "name")
	// Телефон — логин: менять его через /me нельзя.
	assertError(t, c.do(http.MethodPatch, "/api/v1/me", map[string]string{"phone": "+996700810009"}), http.StatusBadRequest, "bad_request")
	assertError(t, api.client(t, "10.0.0.2").do(http.MethodPatch, "/api/v1/me", map[string]string{"name": "x"}), http.StatusUnauthorized, "unauthorized")
}

func TestChangePassword(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	center := c.register("+996700820001", "Центр").Centers[0].ID
	other, ok := login(t, api, "10.0.0.2", "+996700820001", "secret-pass")
	if !ok {
		t.Fatal("второй вход не удался")
	}
	path := "/api/v1/me/password"

	assertField(t, c.do(http.MethodPost, path, map[string]string{"current_password": "wrong-pass", "new_password": "new-secret"}), http.StatusBadRequest, "current_password")
	assertField(t, c.do(http.MethodPost, path, map[string]string{"current_password": "secret-pass", "new_password": "short"}), http.StatusBadRequest, "new_password")
	if !meOK(other) {
		t.Fatal("неудачная смена пароля закрыла сессии")
	}

	if rec := c.do(http.MethodPost, path, map[string]string{"current_password": "secret-pass", "new_password": "new-secret"}); rec.Code != http.StatusNoContent {
		t.Fatalf("смена пароля: %d %s", rec.Code, rec.Body)
	}
	if !meOK(c) {
		t.Error("текущая сессия должна остаться")
	}
	if meOK(other) {
		t.Error("другая сессия должна закрыться")
	}
	if _, ok := login(t, api, "10.0.0.3", "+996700820001", "secret-pass"); ok {
		t.Error("старый пароль всё ещё работает")
	}
	if _, ok := login(t, api, "10.0.0.4", "+996700820001", "new-secret"); !ok {
		t.Error("новый пароль не работает")
	}
	if got := strings.Join(auditActions(auditLog(t, api, center)), ","); got != "user.password_change" {
		t.Errorf("audit = %s", got)
	}
	noSecretsInAudit(t, api, center, "secret-pass", "new-secret")
}

func TestChangePasswordRateLimit(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	c.register("+996700830001", "Центр")
	for i := 0; i < auth.LoginMaxFails; i++ {
		assertField(t, c.do(http.MethodPost, "/api/v1/me/password", map[string]string{"current_password": "wrong-pass", "new_password": "new-secret"}),
			http.StatusBadRequest, "current_password")
	}
	rec := c.do(http.MethodPost, "/api/v1/me/password", map[string]string{"current_password": "secret-pass", "new_password": "new-secret"})
	assertError(t, rec, http.StatusTooManyRequests, "too_many_attempts")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("нет Retry-After")
	}
}

func TestLogoutAll(t *testing.T) {
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")
	center := c.register("+996700840001", "Центр").Centers[0].ID
	other, _ := login(t, api, "10.0.0.2", "+996700840001", "secret-pass")
	rec := c.do(http.MethodPost, "/api/v1/auth/logout-all", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", rec.Code, rec.Body)
	}
	if c.sessionToken() != "" {
		t.Error("cookie не очищена")
	}
	if meOK(c) || meOK(other) {
		t.Error("сессии остались")
	}
	var n int
	_ = api.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.phone = '+996700840001'`).Scan(&n)
	if n != 0 {
		t.Errorf("в БД осталось %d сессий", n)
	}
	if got := strings.Join(auditActions(auditLog(t, api, center)), ","); got != "user.logout_all" {
		t.Errorf("audit = %s", got)
	}
	assertError(t, api.client(t, "10.0.0.3").do(http.MethodPost, "/api/v1/auth/logout-all", nil), http.StatusUnauthorized, "unauthorized")
}

func issueReset(t *testing.T, owner *client, center, userID uuid.UUID) (resetLinkResp, string) {
	t.Helper()
	rec := owner.do(http.MethodPost, centerPath(center, "/staff/"+userID.String()+"/password-reset"), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("password-reset: %d %s", rec.Code, rec.Body)
	}
	res := decode[resetLinkResp](t, rec)
	prefix := testOrigin + "/reset/"
	if !strings.HasPrefix(res.URL, prefix) {
		t.Fatalf("url = %q", res.URL)
	}
	return res, strings.TrimPrefix(res.URL, prefix)
}

// Владелец выдаёт ссылку преподавателю, который работает только в его центре: новый пароль, все сессии
// закрыты, ссылка одноразовая; новая ссылка отменяет прежнюю.
func TestStaffPasswordReset(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	center := owner.register("+996700850001", "Центр Ресет").Centers[0].ID
	teacher, teacherID := addStaff(t, api, owner, center, "teacher", "+996700850002")
	ctx := context.Background()

	first, firstTok := issueReset(t, owner, center, teacherID)
	if first.Name != "Сотрудник +996700850002" {
		t.Errorf("name = %q", first.Name)
	}
	res, tok := issueReset(t, owner, center, teacherID)
	anon := api.client(t, "10.0.0.9")
	assertError(t, anon.do(http.MethodGet, "/api/v1/password-reset/"+firstTok, nil), http.StatusNotFound, "reset_invalid")

	var raw int
	_ = api.pool.QueryRow(ctx, `SELECT count(*) FROM password_resets WHERE token = $1`, tok).Scan(&raw)
	if raw != 0 {
		t.Error("в БД открытый токен")
	}
	pub := decode[publicResetResp](t, anon.do(http.MethodGet, "/api/v1/password-reset/"+tok, nil))
	if pub.Name != res.Name || pub.CenterName == nil || *pub.CenterName != "Центр Ресет" {
		t.Errorf("public = %+v", pub)
	}

	// Короткий пароль — ошибка поля, ссылка не расходуется.
	assertField(t, anon.do(http.MethodPost, "/api/v1/password-reset/"+tok, map[string]string{"new_password": "short"}), http.StatusBadRequest, "new_password")
	if rec := anon.do(http.MethodPost, "/api/v1/password-reset/"+tok, map[string]string{"new_password": "reset-pass-1"}); rec.Code != http.StatusNoContent {
		t.Fatalf("сброс: %d %s", rec.Code, rec.Body)
	}
	if meOK(teacher) {
		t.Error("сессия сотрудника должна закрыться")
	}
	if _, ok := login(t, api, "10.0.0.3", "+996700850002", "staff-pass"); ok {
		t.Error("старый пароль работает")
	}
	if _, ok := login(t, api, "10.0.0.4", "+996700850002", "reset-pass-1"); !ok {
		t.Error("новый пароль не работает")
	}
	assertError(t, anon.do(http.MethodPost, "/api/v1/password-reset/"+tok, map[string]string{"new_password": "reset-pass-2"}), http.StatusNotFound, "reset_invalid")

	got := strings.Join(auditActions(auditLog(t, api, center)), ",")
	if !strings.HasSuffix(got, "user.password_reset_issue,user.password_reset_issue,user.password_reset_done") {
		t.Errorf("audit = %s", got)
	}
	noSecretsInAudit(t, api, center, tok, firstTok, "reset-pass-1")
}

// Неизвестная, просроченная, использованная и мусорная ссылки — одинаковый 404.
func TestPasswordResetInvalidSameAnswer(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	center := owner.register("+996700860001", "Центр").Centers[0].ID
	_, t1 := addStaff(t, api, owner, center, "teacher", "+996700860002")
	_, t2 := addStaff(t, api, owner, center, "admin", "+996700860003")
	_, expired := issueReset(t, owner, center, t1)
	if _, err := api.pool.Exec(context.Background(), `UPDATE password_resets SET expires_at = now() - interval '1 second' WHERE token = $1`, token.HashHex(expired)); err != nil {
		t.Fatal(err)
	}
	_, used := issueReset(t, owner, center, t2)
	anon := api.client(t, "10.0.0.9")
	if rec := anon.do(http.MethodPost, "/api/v1/password-reset/"+used, map[string]string{"new_password": "reset-pass-1"}); rec.Code != http.StatusNoContent {
		t.Fatalf("сброс: %d %s", rec.Code, rec.Body)
	}
	unknown, _ := token.New()
	var bodies []string
	for name, tok := range map[string]string{"просроченная": expired, "использованная": used, "неизвестная": unknown, "мусор": "x", "хэш из БД": token.HashHex(used)} {
		t.Run(name, func(t *testing.T) {
			get := anon.do(http.MethodGet, "/api/v1/password-reset/"+tok, nil)
			assertError(t, get, http.StatusNotFound, "reset_invalid")
			assertError(t, anon.do(http.MethodPost, "/api/v1/password-reset/"+tok, map[string]string{"new_password": "reset-pass-9"}), http.StatusNotFound, "reset_invalid")
			bodies = append(bodies, get.Body.String())
		})
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("ответы различаются:\n%s\n%s", bodies[0], b)
		}
	}
}

// Сброс запрещён, если сотрудник работает ещё в другом центре или он владелец; чужой сотрудник — 404;
// admin и teacher выдать ссылку не могут.
func TestPasswordResetNotAllowed(t *testing.T) {
	api := newTestAPI(t)
	ownerA := api.client(t, "10.0.0.1")
	centerA := ownerA.register("+996700870001", "Центр А").Centers[0].ID
	ownerB := api.client(t, "10.0.0.2")
	centerB := ownerB.register("+996700870002", "Центр Б").Centers[0].ID

	// Преподаватель в двух центрах.
	both, bothID := addStaff(t, api, ownerA, centerA, "teacher", "+996700870003")
	_, tokB := createInvite(t, ownerB, centerB, "teacher", "")
	if rec := both.do(http.MethodPost, "/api/v1/invites/"+tokB+"/accept", nil); rec.Code != http.StatusOK {
		t.Fatalf("второй центр: %d %s", rec.Code, rec.Body)
	}
	assertError(t, ownerA.do(http.MethodPost, centerPath(centerA, "/staff/"+bothID.String()+"/password-reset"), nil), http.StatusConflict, "reset_not_allowed")
	assertError(t, ownerB.do(http.MethodPost, centerPath(centerB, "/staff/"+bothID.String()+"/password-reset"), nil), http.StatusConflict, "reset_not_allowed")

	// Владелец — сам себе (и другому владельцу) — нельзя.
	ownerAID := decode[meResp](t, ownerA.do(http.MethodGet, "/api/v1/me", nil)).ID
	assertError(t, ownerA.do(http.MethodPost, centerPath(centerA, "/staff/"+ownerAID.String()+"/password-reset"), nil), http.StatusConflict, "reset_not_allowed")

	// Сотрудник только центра А: владелец Б через свой центр — 404; admin и teacher А — 403.
	admin, _ := addStaff(t, api, ownerA, centerA, "admin", "+996700870004")
	teacher, onlyAID := addStaff(t, api, ownerA, centerA, "teacher", "+996700870005")
	assertError(t, ownerB.do(http.MethodPost, centerPath(centerB, "/staff/"+onlyAID.String()+"/password-reset"), nil), http.StatusNotFound, "not_found")
	assertError(t, ownerB.do(http.MethodPost, centerPath(centerA, "/staff/"+onlyAID.String()+"/password-reset"), nil), http.StatusNotFound, "not_found")
	assertError(t, admin.do(http.MethodPost, centerPath(centerA, "/staff/"+onlyAID.String()+"/password-reset"), nil), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodPost, centerPath(centerA, "/staff/"+onlyAID.String()+"/password-reset"), nil), http.StatusForbidden, "forbidden")

	var n int
	_ = api.pool.QueryRow(context.Background(), `SELECT count(*) FROM password_resets`).Scan(&n)
	if n != 0 {
		t.Errorf("создано %d ссылок, want 0", n)
	}
	issueReset(t, ownerA, centerA, onlyAID) // ему — можно
}

// Поддержка (команда server user reset-link) выдаёт ссылку и владельцу; центр в ссылке не указан.
func TestSupportPasswordReset(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	center := owner.register("+996700880001", "Центр").Centers[0].ID
	if _, err := api.auth.IssueSupportPasswordReset(context.Background(), "0700 000 000"); err != auth.ErrUserNotFound {
		t.Errorf("неизвестный телефон: %v", err)
	}
	res, err := api.auth.IssueSupportPasswordReset(context.Background(), "0700 880 001")
	if err != nil {
		t.Fatal(err)
	}
	anon := api.client(t, "10.0.0.9")
	pub := decode[publicResetResp](t, anon.do(http.MethodGet, "/api/v1/password-reset/"+res.Token, nil))
	if pub.CenterName != nil || pub.Name != "Владелец Центр" {
		t.Errorf("public = %+v", pub)
	}
	if rec := anon.do(http.MethodPost, "/api/v1/password-reset/"+res.Token, map[string]string{"new_password": "owner-new-pass"}); rec.Code != http.StatusNoContent {
		t.Fatalf("сброс: %d %s", rec.Code, rec.Body)
	}
	if meOK(owner) {
		t.Error("сессия владельца должна закрыться")
	}
	if _, ok := login(t, api, "10.0.0.4", "+996700880001", "owner-new-pass"); !ok {
		t.Error("новый пароль не работает")
	}
	log := auditLog(t, api, center)
	if got := strings.Join(auditActions(log), ","); got != "user.password_reset_issue,user.password_reset_done" {
		t.Errorf("audit = %s", got)
	}
	if log[0].UserID != nil {
		t.Error("выдача поддержкой — без автора-сотрудника")
	}
	noSecretsInAudit(t, api, center, res.Token, "owner-new-pass")
}
