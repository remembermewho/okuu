package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"okuu/internal/platform"
)

// Панель суперадмина (/api/v1/admin): права только через консоль, всем остальным — 404,
// только агрегаты по центрам.

const adminPhone = "+996707950001"

// adminClient — пользователь с центром (обычный владелец), которому консоль выдала права суперадмина.
func adminClient(t *testing.T, api *testAPI) *client {
	t.Helper()
	c := api.client(t, "10.0.9.1")
	c.register(adminPhone, "Центр суперадмина")
	if _, err := platform.NewService(api.pool, api.auth, api.clock.Now).SetPlatformAdmin(context.Background(), adminPhone, true); err != nil {
		t.Fatal(err)
	}
	return c
}

type adminCenterResp struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	BlockedReason  *string   `json:"blocked_reason"`
	Plan           string    `json:"plan"`
	PaidUntil      *string   `json:"paid_until"`
	PaymentOverdue bool      `json:"payment_overdue"`
	Owner          struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	} `json:"owner"`
	ActiveStudents int64   `json:"active_students"`
	LastActivity   *string `json:"last_activity"`
	Students       struct {
		Active int64 `json:"active"`
	} `json:"students"`
	TelegramParents int64 `json:"telegram_parents"`
	Audit           []struct {
		Action    string  `json:"action"`
		AdminName *string `json:"admin_name"`
	} `json:"audit"`
}

type linkResp struct {
	ID     uuid.UUID `json:"id"`
	Note   string    `json:"note"`
	URL    string    `json:"url"`
	Status string    `json:"status"`
	Center *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"center"`
}

// adminRoutes — все маршруты панели с телами (id подставляет тест).
func adminRoutes(centerID, linkID uuid.UUID) []struct {
	method, path string
	body         any
} {
	cid, lid := centerID.String(), linkID.String()
	return []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/admin/centers", nil},
		{http.MethodGet, "/api/v1/admin/centers/" + cid, nil},
		{http.MethodPatch, "/api/v1/admin/centers/" + cid, map[string]any{"plan": "start", "paid_until": nil}},
		{http.MethodPost, "/api/v1/admin/centers/" + cid + "/approve", nil},
		{http.MethodPost, "/api/v1/admin/centers/" + cid + "/block", map[string]string{"reason": "тест"}},
		{http.MethodPost, "/api/v1/admin/centers/" + cid + "/unblock", nil},
		{http.MethodPost, "/api/v1/admin/centers/" + cid + "/owner-password-reset", nil},
		{http.MethodGet, "/api/v1/admin/registration-links", nil},
		{http.MethodPost, "/api/v1/admin/registration-links", map[string]string{"note": "взлом"}},
		{http.MethodDelete, "/api/v1/admin/registration-links/" + lid, nil},
		{http.MethodGet, "/api/v1/admin/audit", nil},
	}
}

// Обычный владелец и гость получают 404 на любой маршрут панели — и ничего не меняется.
func TestAdminAccess(t *testing.T) {
	api := newTestAPI(t)
	admin := adminClient(t, api)
	owner := api.client(t, "10.0.9.2")
	me := owner.register("0707950002", "Обычный центр")
	cid := me.Centers[0].ID
	link := decode[linkResp](t, admin.do(http.MethodPost, "/api/v1/admin/registration-links", map[string]string{"note": "Айгуль"}))

	guest := api.client(t, "10.0.9.3")
	for _, r := range adminRoutes(cid, link.ID) {
		assertError(t, owner.do(r.method, r.path, r.body), http.StatusNotFound, "not_found")
		assertError(t, guest.do(r.method, r.path, r.body), http.StatusNotFound, "not_found")
	}
	// Ничего не изменилось: центр активен, ссылка действует.
	if rec := owner.do(http.MethodGet, centerPath(cid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("центр после попыток: %d", rec.Code)
	}
	links := decodeItems[linkResp](t, admin.do(http.MethodGet, "/api/v1/admin/registration-links", nil))
	if len(links) != 1 || links[0].Status != "active" {
		t.Errorf("ссылки: %+v", links)
	}
	if m := decode[meResp](t, admin.do(http.MethodGet, "/api/v1/me", nil)); !m.IsPlatformAdmin {
		t.Error("/me суперадмина: is_platform_admin = false")
	}
	if m := decode[meResp](t, owner.do(http.MethodGet, "/api/v1/me", nil)); m.IsPlatformAdmin {
		t.Error("/me владельца: is_platform_admin = true")
	}
	// Сам себе права через API не выдать: такого маршрута нет, PATCH /me поле не принимает.
	assertError(t, owner.do(http.MethodPatch, "/api/v1/me", map[string]any{"is_platform_admin": true}), http.StatusBadRequest, "bad_request")

	// Права снимает только консоль — после revoke панель снова 404.
	svc := platform.NewService(api.pool, api.auth, api.clock.Now)
	if _, err := svc.SetPlatformAdmin(context.Background(), adminPhone, false); err != nil {
		t.Fatal(err)
	}
	assertError(t, admin.do(http.MethodGet, "/api/v1/admin/centers", nil), http.StatusNotFound, "not_found")
	if _, err := svc.SetPlatformAdmin(context.Background(), "0707959999", true); !errors.Is(err, platform.ErrUserNotFound) {
		t.Errorf("grant неизвестному телефону: %v", err)
	}
	var n int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM platform_audit_log WHERE action IN ('admin.grant', 'admin.revoke') AND admin_id IS NULL`).Scan(&n); err != nil || n != 2 {
		t.Errorf("журнал grant/revoke: %d %v", n, err)
	}
}

// approve, block, unblock меняют доступ центра; тариф и «оплачено до» доступ не меняют; изоляция центров.
func TestAdminCenterLifecycle(t *testing.T) {
	v := specValidator(t)
	api := newTestAPI(t)
	admin := adminClient(t, api)
	owner := api.client(t, "10.0.9.4")
	cid := owner.register("0707950004", "Билим").Centers[0].ID
	neighbour := api.client(t, "10.0.9.5")
	nid := neighbour.register("0707950005", "Соседний").Centers[0].ID
	setCenterStatus(t, api, cid, "pending")

	list := decodeItems[adminCenterResp](t, checked(t, v, admin, http.MethodGet, "/api/v1/admin/centers", nil, http.StatusOK))
	if len(list) != 3 || list[0].ID != cid || list[0].Status != "pending" || list[0].Owner.Phone != "+996707950004" || list[0].LastActivity == nil {
		t.Fatalf("список (заявки сверху): %+v", list)
	}
	if got := decodeItems[adminCenterResp](t, checked(t, v, admin, http.MethodGet, "/api/v1/admin/centers?status=pending", nil, http.StatusOK)); len(got) != 1 {
		t.Errorf("фильтр pending: %d", len(got))
	}
	if got := decodeItems[adminCenterResp](t, checked(t, v, admin, http.MethodGet, "/api/v1/admin/centers?q=0707950005", nil, http.StatusOK)); len(got) != 1 || got[0].ID != nid {
		t.Errorf("поиск по телефону: %+v", got)
	}
	if got := decodeItems[adminCenterResp](t, admin.do(http.MethodGet, "/api/v1/admin/centers?q=били", nil)); len(got) != 1 || got[0].ID != cid {
		t.Errorf("поиск по названию: %+v", got)
	}
	assertError(t, admin.do(http.MethodGet, "/api/v1/admin/centers?status=deleted", nil), http.StatusBadRequest, "validation")

	assertError(t, owner.do(http.MethodGet, centerPath(cid, "/students"), nil), http.StatusForbidden, "center_pending")
	card := decode[adminCenterResp](t, checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/approve", nil, http.StatusOK))
	if card.Status != "active" {
		t.Errorf("после approve: %+v", card)
	}
	if rec := owner.do(http.MethodGet, centerPath(cid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("доступ после approve: %d", rec.Code)
	}
	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/approve", nil, http.StatusConflict)

	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/block", map[string]string{"reason": " "}, http.StatusBadRequest)
	card = decode[adminCenterResp](t, checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/block",
		map[string]string{"reason": "Не оплачен октябрь"}, http.StatusOK))
	if card.Status != "blocked" || card.BlockedReason == nil || *card.BlockedReason != "Не оплачен октябрь" {
		t.Errorf("после block: %+v", card)
	}
	assertError(t, owner.do(http.MethodGet, centerPath(cid, "/students"), nil), http.StatusForbidden, "center_blocked")
	if rec := neighbour.do(http.MethodGet, centerPath(nid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("соседний центр задело блокировкой: %d", rec.Code)
	}
	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/block", map[string]string{"reason": "ещё раз"}, http.StatusConflict)
	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+nid.String()+"/unblock", nil, http.StatusConflict)
	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/unblock", nil, http.StatusOK)
	if rec := owner.do(http.MethodGet, centerPath(cid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("доступ после unblock: %d", rec.Code)
	}

	// Тариф и «оплачено до» в прошлом — подсветка, но доступ не трогаем.
	checked(t, v, admin, http.MethodPatch, "/api/v1/admin/centers/"+cid.String(), map[string]any{"plan": "vip", "paid_until": nil}, http.StatusBadRequest)
	past := api.clock.Now().AddDate(0, 0, -3).Format("2006-01-02")
	card = decode[adminCenterResp](t, checked(t, v, admin, http.MethodPatch, "/api/v1/admin/centers/"+cid.String(),
		map[string]any{"plan": "start", "paid_until": past}, http.StatusOK))
	if card.Plan != "start" || card.PaidUntil == nil || *card.PaidUntil != past || !card.PaymentOverdue {
		t.Errorf("тариф: %+v", card)
	}
	if rec := owner.do(http.MethodGet, centerPath(cid, "/students"), nil); rec.Code != http.StatusOK {
		t.Errorf("просрочка оплаты заблокировала центр: %d", rec.Code)
	}
	if s := decode[settingsResp](t, owner.do(http.MethodGet, centerPath(cid, "/settings"), nil)); s.Plan != "start" {
		t.Errorf("тариф в настройках центра: %q", s.Plan)
	}
	card = decode[adminCenterResp](t, admin.do(http.MethodPatch, "/api/v1/admin/centers/"+cid.String(), map[string]any{"plan": "founder", "paid_until": nil}))
	if card.PaidUntil != nil || card.PaymentOverdue {
		t.Errorf("paid_until = null: %+v", card)
	}

	// Ссылка сброса пароля владельца работает на публичной странице.
	reset := decode[resetLinkResp](t, checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+cid.String()+"/owner-password-reset", nil, http.StatusCreated))
	if rec := owner.do(http.MethodGet, "/api/v1/password-reset/"+strings.TrimPrefix(reset.URL, testOrigin+"/reset/"), nil); rec.Code != http.StatusOK {
		t.Errorf("ссылка сброса владельца: %d %s", rec.Code, rec.Body)
	}

	// Журнал: по карточке и общий.
	card = decode[adminCenterResp](t, checked(t, v, admin, http.MethodGet, "/api/v1/admin/centers/"+cid.String(), nil, http.StatusOK))
	var actions []string
	for _, a := range card.Audit {
		actions = append(actions, a.Action)
		if a.AdminName == nil {
			t.Errorf("действие %s без суперадмина", a.Action)
		}
	}
	want := "center.owner_password_reset center.plan center.plan center.unblock center.block center.approve"
	if strings.Join(actions, " ") != want {
		t.Errorf("журнал центра: %v", actions)
	}
	checked(t, v, admin, http.MethodGet, "/api/v1/admin/audit?limit=5", nil, http.StatusOK)
	checked(t, v, admin, http.MethodGet, "/api/v1/admin/audit?limit=500", nil, http.StatusBadRequest)

	// Неизвестный и некорректный id — 404.
	checked(t, v, admin, http.MethodGet, "/api/v1/admin/centers/"+uuid.NewString(), nil, http.StatusNotFound)
	assertError(t, admin.do(http.MethodPost, "/api/v1/admin/centers/xyz/approve", nil), http.StatusNotFound, "not_found")
	checked(t, v, admin, http.MethodPost, "/api/v1/admin/centers/"+uuid.NewString()+"/block", map[string]string{"reason": "нет"}, http.StatusNotFound)
}

// Ссылки регистрации одноразовые; отозванная не работает; использованную не отозвать.
func TestAdminRegistrationLinks(t *testing.T) {
	v := specValidator(t)
	api := newTestAPIMode(t, "invite")
	admin := api.client(t, "10.0.9.6")
	if r := registerWith(admin, adminPhone, "Центр суперадмина", registrationLink(t, api, "сам себе")); r.StatusCode != http.StatusCreated {
		t.Fatalf("регистрация суперадмина: %d", r.StatusCode)
	}
	if _, err := platform.NewService(api.pool, api.auth, api.clock.Now).SetPlatformAdmin(context.Background(), adminPhone, true); err != nil {
		t.Fatal(err)
	}

	checked(t, v, admin, http.MethodPost, "/api/v1/admin/registration-links", map[string]string{"note": ""}, http.StatusBadRequest)
	link := decode[linkResp](t, checked(t, v, admin, http.MethodPost, "/api/v1/admin/registration-links",
		map[string]string{"note": "Садик «Балапан», Нургуль"}, http.StatusCreated))
	tok, ok := strings.CutPrefix(link.URL, testOrigin+"/register/")
	if !ok || tok == "" {
		t.Fatalf("url = %q", link.URL)
	}
	var stored string
	if err := api.pool.QueryRow(context.Background(), `SELECT token FROM registration_links WHERE id = $1`, link.ID).Scan(&stored); err != nil || stored == tok {
		t.Errorf("в БД сам токен, а не хэш: %v", err)
	}

	newbie := api.client(t, "10.0.9.7")
	if r := registerWith(newbie, "0707950007", "Балапан", tok); r.StatusCode != http.StatusCreated {
		t.Fatalf("по ссылке: %d", r.StatusCode)
	}
	assertError(t, api.client(t, "10.0.9.8").do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"phone": "0707950008", "password": "secret-pass", "name": "Второй", "center_name": "Второй", "registration_token": tok,
	}), http.StatusNotFound, "registration_invalid")

	links := decodeItems[linkResp](t, checked(t, v, admin, http.MethodGet, "/api/v1/admin/registration-links", nil, http.StatusOK))
	var used *linkResp
	for i := range links {
		if links[i].ID == link.ID {
			used = &links[i]
		}
	}
	if used == nil || used.Status != "used" || used.Center == nil || used.Center.Name != "Балапан" {
		t.Fatalf("использованная ссылка: %+v", used)
	}
	checked(t, v, admin, http.MethodDelete, "/api/v1/admin/registration-links/"+link.ID.String(), nil, http.StatusConflict)

	// Отозванная — недействительна.
	other := decode[linkResp](t, admin.do(http.MethodPost, "/api/v1/admin/registration-links", map[string]string{"note": "передумали"}))
	otherTok := strings.TrimPrefix(other.URL, testOrigin+"/register/")
	checked(t, v, admin, http.MethodDelete, "/api/v1/admin/registration-links/"+other.ID.String(), nil, http.StatusNoContent)
	assertError(t, newbie.do(http.MethodGet, "/api/v1/auth/registration/"+otherTok, nil), http.StatusNotFound, "registration_invalid")
	checked(t, v, admin, http.MethodDelete, "/api/v1/admin/registration-links/"+other.ID.String(), nil, http.StatusConflict)
	checked(t, v, admin, http.MethodDelete, "/api/v1/admin/registration-links/"+uuid.NewString(), nil, http.StatusNotFound)
}

// Ни один ответ панели не содержит ФИО учеников, имён и телефонов родителей.
func TestAdminNoPersonalData(t *testing.T) {
	api := newTestAPI(t)
	admin := adminClient(t, api)
	c := newCatalogOn(t, api, "+996707950010")
	s := c.debtor(t, "Секретова Айсулуу", "0707 950 111")
	linkDirect(t, api, s.Parent.ID, 7950111)
	c.student(t, "Тайнов Бекзат", "0707 950 112", nil)
	link := decode[linkResp](t, admin.do(http.MethodPost, "/api/v1/admin/registration-links", map[string]string{"note": "для теста"}))
	admin.do(http.MethodPost, "/api/v1/admin/centers/"+c.center.String()+"/block", map[string]string{"reason": "проверка"})
	admin.do(http.MethodPost, "/api/v1/admin/centers/"+c.center.String()+"/unblock", nil)

	secrets := []string{"Секретова", "Айсулуу", "Тайнов", "Бекзат", "Родитель", "+996707950111", "707950111", "+996707950112", "707950112"}
	var bodies []string
	for _, r := range adminRoutes(c.center, link.ID) {
		if r.method != http.MethodGet {
			continue
		}
		rec := admin.do(r.method, r.path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", r.path, rec.Code, rec.Body)
		}
		bodies = append(bodies, r.path+": "+rec.Body.String())
	}
	for _, b := range bodies {
		for _, sec := range secrets {
			if strings.Contains(b, sec) {
				t.Errorf("в ответе панели %q:\n%s", sec, b)
			}
		}
	}
	card := decode[adminCenterResp](t, admin.do(http.MethodGet, "/api/v1/admin/centers/"+c.center.String(), nil))
	if card.Students.Active != 2 || card.TelegramParents != 1 {
		t.Errorf("агрегаты: %+v", card)
	}
}
