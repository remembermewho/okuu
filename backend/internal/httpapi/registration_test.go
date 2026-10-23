package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"okuu/internal/auth"
	"okuu/internal/httpapi"
	"okuu/internal/notify"
	"okuu/internal/platform"
)

// newTestAPIMode — API с режимом регистрации mode (REGISTRATION_MODE) и номером поддержки.
func newTestAPIMode(t *testing.T, mode string) *testAPI {
	t.Helper()
	return newTestAPIWith(t, func(api *testAPI, d *httpapi.Deps) {
		api.auth = auth.NewService(api.pool, auth.Options{Now: api.clock.Now, RegistrationMode: mode})
		d.Auth = api.auth
		d.SupportWhatsApp = "996555000100"
	})
}

// registrationLink — ссылка регистрации от «консоли» (как server admin registration-link); токен.
func registrationLink(t *testing.T, api *testAPI, note string) string {
	t.Helper()
	link, err := platform.NewService(api.pool, api.auth, api.clock.Now).CreateRegistrationLink(context.Background(), nil, note)
	if err != nil {
		t.Fatal(err)
	}
	return link.Token
}

type registrationInfo struct {
	Mode            string  `json:"mode"`
	SupportWhatsApp *string `json:"support_whatsapp"`
}

func registerWith(c *client, phone, center, tok string) *http.Response {
	c.t.Helper()
	body := map[string]string{"phone": phone, "password": "secret-pass", "name": "Владелец", "center_name": center}
	if tok != "" {
		body["registration_token"] = tok
	}
	return c.do(http.MethodPost, "/api/v1/auth/register", body).Result()
}

func setCenterStatus(t *testing.T, api *testAPI, id uuid.UUID, status string) {
	t.Helper()
	if _, err := api.pool.Exec(context.Background(), `UPDATE centers SET status = $2 WHERE id = $1`, id, status); err != nil {
		t.Fatal(err)
	}
}

// invite: без ссылки — 403 registration_closed; ссылка одноразовая, занятый телефон её не «сжигает».
func TestRegistrationInvite(t *testing.T) {
	api := newTestAPIMode(t, auth.RegistrationInvite)
	c := api.client(t, "10.0.1.1")

	info := decode[registrationInfo](t, c.do(http.MethodGet, "/api/v1/auth/registration", nil))
	if info.Mode != "invite" || info.SupportWhatsApp == nil || *info.SupportWhatsApp != "996555000100" {
		t.Errorf("registration: %+v", info)
	}
	assertError(t, c.do(http.MethodPost, "/api/v1/auth/register", registerReq{Phone: "0709100001", Password: "secret-pass", Name: "А", CenterName: "Без ссылки"}),
		http.StatusForbidden, "registration_closed")

	tok := registrationLink(t, api, "Центр «Билим», Айгуль")
	if rec := c.do(http.MethodGet, "/api/v1/auth/registration/"+tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("проверка ссылки: %d %s", rec.Code, rec.Body)
	}
	assertError(t, c.do(http.MethodGet, "/api/v1/auth/registration/"+tok+"x", nil), http.StatusNotFound, "registration_invalid")

	// Телефон занят — 409, ссылка остаётся действующей.
	if r := registerWith(api.client(t, "10.0.1.2"), "0709100002", "Уже есть", registrationLink(t, api, "первый")); r.StatusCode != http.StatusCreated {
		t.Fatalf("первый центр: %d", r.StatusCode)
	}
	if r := registerWith(c, "0709100002", "Повтор", tok); r.StatusCode != http.StatusConflict {
		t.Fatalf("занятый телефон по ссылке: %d", r.StatusCode)
	}
	if r := registerWith(c, "0709100003", "Билим", tok); r.StatusCode != http.StatusCreated {
		t.Fatalf("регистрация по ссылке: %d", r.StatusCode)
	}
	me := decode[meResp](t, c.do(http.MethodGet, "/api/v1/me", nil))
	if len(me.Centers) != 1 || me.Centers[0].Status != "active" || me.IsPlatformAdmin {
		t.Errorf("me: %+v", me)
	}
	if rec := c.do(http.MethodGet, centerPath(me.Centers[0].ID, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("центр по ссылке: %d %s", rec.Code, rec.Body)
	}

	// Повторно — нельзя; ссылка привязана к центру.
	other := api.client(t, "10.0.1.3")
	assertError(t, other.do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"phone": "0709100004", "password": "secret-pass", "name": "Б", "center_name": "Второй", "registration_token": tok,
	}), http.StatusNotFound, "registration_invalid")
	assertError(t, other.do(http.MethodGet, "/api/v1/auth/registration/"+tok, nil), http.StatusNotFound, "registration_invalid")
	var linked uuid.UUID
	if err := api.pool.QueryRow(context.Background(), `SELECT center_id FROM registration_links WHERE note = 'Центр «Билим», Айгуль'`).Scan(&linked); err != nil || linked != me.Centers[0].ID {
		t.Errorf("ссылка → центр: %v %v", linked, err)
	}
}

// approval: центр — заявка (pending): /c/* закрыты, /me и выход работают; по ссылке — сразу активен.
func TestRegistrationApproval(t *testing.T) {
	api := newTestAPIMode(t, auth.RegistrationApproval)
	c := api.client(t, "10.0.2.1")
	me := c.register("0709200001", "Заявка")
	if me.Centers[0].Status != "pending" {
		t.Fatalf("статус: %+v", me.Centers)
	}
	cid := me.Centers[0].ID
	for _, path := range []string{"/students", "/settings", "/debtors", "/lessons"} {
		assertError(t, c.do(http.MethodGet, centerPath(cid, path), nil), http.StatusForbidden, "center_pending")
	}
	assertError(t, c.do(http.MethodPost, centerPath(cid, "/tariffs"), map[string]any{"name": "Месяц", "kind": "monthly", "price": 300000}),
		http.StatusForbidden, "center_pending")
	if rec := c.do(http.MethodGet, "/api/v1/me", nil); rec.Code != http.StatusOK {
		t.Errorf("/me у заявки: %d", rec.Code)
	}
	// Чужой центр — по-прежнему 404, а не center_pending: существование не раскрывается.
	stranger := api.client(t, "10.0.2.2")
	stranger.register("0709200002", "Чужой")
	if r := stranger.do(http.MethodGet, centerPath(cid, "/students"), nil); r.Code != http.StatusNotFound {
		t.Errorf("чужой pending-центр: %d", r.Code)
	}

	// Одобрили — доступ есть.
	setCenterStatus(t, api, cid, "active")
	if rec := c.do(http.MethodGet, centerPath(cid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("после одобрения: %d %s", rec.Code, rec.Body)
	}

	// По ссылке — сразу активен.
	tok := registrationLink(t, api, "по ссылке")
	byLink := api.client(t, "10.0.2.3")
	if r := registerWith(byLink, "0709200003", "По ссылке", tok); r.StatusCode != http.StatusCreated {
		t.Fatalf("по ссылке: %d", r.StatusCode)
	}
	if m := decode[meResp](t, byLink.do(http.MethodGet, "/api/v1/me", nil)); m.Centers[0].Status != "active" {
		t.Errorf("центр по ссылке в approval: %+v", m.Centers)
	}
	if rec := c.do(http.MethodPost, "/api/v1/auth/logout", nil); rec.Code != http.StatusNoContent {
		t.Errorf("выход: %d", rec.Code)
	}
}

// open — свободно и сразу активен; blocked — 403 center_blocked на всё под /c/, /me и выход работают.
func TestRegistrationOpenAndBlocked(t *testing.T) {
	api := newTestAPIMode(t, auth.RegistrationOpen)
	c := api.client(t, "10.0.3.1")
	me := c.register("0709300001", "Открытый")
	if me.Centers[0].Status != "active" {
		t.Fatalf("open: %+v", me.Centers)
	}
	if info := decode[registrationInfo](t, c.do(http.MethodGet, "/api/v1/auth/registration", nil)); info.Mode != "open" {
		t.Errorf("mode: %+v", info)
	}
	cid := me.Centers[0].ID
	setCenterStatus(t, api, cid, "blocked")
	for _, path := range []string{"/students", "/settings", "/staff", "/reports/month"} {
		assertError(t, c.do(http.MethodGet, centerPath(cid, path), nil), http.StatusForbidden, "center_blocked")
	}
	if m := decode[meResp](t, c.do(http.MethodGet, "/api/v1/me", nil)); m.Centers[0].Status != "blocked" {
		t.Errorf("/me: %+v", m.Centers)
	}
	if rec := c.do(http.MethodPost, "/api/v1/auth/logout", nil); rec.Code != http.StatusNoContent {
		t.Errorf("выход: %d", rec.Code)
	}
}

// Бот и напоминания не работают для заявки и заблокированного центра, даже если родитель привязан.
func TestInactiveCenterSilent(t *testing.T) {
	for _, status := range []string{"pending", "blocked"} {
		t.Run(status, func(t *testing.T) {
			env := newBotEnv(t)
			atBishkek(env.api, 10, 0)
			normal := newCatalogOn(t, env.api, "+996707910001")
			normal.patchSettings(t, map[string]any{"due_days": 3})
			ns := normal.debtor(t, "Обычный Должник", "0707 910 101")
			linkDirect(t, env.api, ns.Parent.ID, 7910101)

			c := newCatalogOn(t, env.api, "+996707910002")
			c.patchSettings(t, map[string]any{"due_days": 3})
			s := c.debtor(t, "Неактивный Должник", "0707 910 201")
			const chat = 7910201
			linkDirect(t, env.api, s.Parent.ID, chat)
			_, stale := inviteParent(t, c, s.Parent.ID)
			c.mustOp(t, s.ID, pay(50_000)) // событие «оплата получена» уже в очереди
			setCenterStatus(t, env.api, c.center, status)

			if res := env.runAuto(t); res.Sent != 1 {
				t.Errorf("автонапоминания: %+v, want одно — обычному центру", res)
			}
			if n := env.dispatch(t); n != 0 || outboxCount(t, env.api) != 0 {
				t.Errorf("dispatch: %d, в очереди %d", n, outboxCount(t, env.api))
			}
			tx := notify.For(notify.LangRU)
			if m := one(t, env.user(t, chat).send("/balance")); m.Text != tx.NotLinked {
				t.Errorf("/balance: %q", m.Text)
			}
			if m := one(t, env.user(t, 7910299).send("/start "+stale)); m.Text != tx.InviteInvalid {
				t.Errorf("приглашение: %q", m.Text)
			}
			for _, m := range env.tg.Messages(chat) {
				if strings.Contains(m.Text, "Неактивный Должник") {
					t.Errorf("бот показал ученика: %q", m.Text)
				}
			}
		})
	}
}
