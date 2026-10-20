package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/token"
)

type inviteResp struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	Role      string    `json:"role"`
	Name      *string   `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type staffResp struct {
	UserID uuid.UUID `json:"user_id"`
	Name   string    `json:"name"`
	Phone  string    `json:"phone"`
	Role   string    `json:"role"`
}

type acceptReq struct {
	Name     string `json:"name,omitempty"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

func centerPath(centerID uuid.UUID, rest string) string {
	return "/api/v1/c/" + centerID.String() + rest
}

// createInvite — owner создаёт приглашение; возвращает ответ и открытый токен из url.
func createInvite(t *testing.T, owner *client, centerID uuid.UUID, role, name string) (inviteResp, string) {
	t.Helper()
	rec := owner.do(http.MethodPost, centerPath(centerID, "/staff/invites"), map[string]string{"role": role, "name": name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite: %d %s", rec.Code, rec.Body)
	}
	inv := decode[inviteResp](t, rec)
	prefix := testOrigin + "/invite/"
	if !strings.HasPrefix(inv.URL, prefix) {
		t.Fatalf("url = %q, want prefix %q", inv.URL, prefix)
	}
	return inv, strings.TrimPrefix(inv.URL, prefix)
}

// addStaff — новый человек принимает приглашение с ролью role и получает свою сессию.
func addStaff(t *testing.T, api *testAPI, owner *client, centerID uuid.UUID, role, phone string) (*client, uuid.UUID) {
	t.Helper()
	_, tok := createInvite(t, owner, centerID, role, "Сотрудник "+phone)
	c := api.client(t, "10.5.0.1")
	rec := c.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", acceptReq{Phone: phone, Password: "staff-pass"})
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	return c, decode[meResp](t, rec).ID
}

func listStaff(t *testing.T, c *client, centerID uuid.UUID) []staffResp {
	t.Helper()
	rec := c.do(http.MethodGet, centerPath(centerID, "/staff"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list staff: %d %s", rec.Code, rec.Body)
	}
	return decodeItems[staffResp](t, rec)
}

type auditRow struct {
	UserID   *uuid.UUID
	Action   string
	Entity   string
	EntityID *uuid.UUID
	Before   map[string]any
	After    map[string]any
}

func auditLog(t *testing.T, api *testAPI, centerID uuid.UUID) []auditRow {
	t.Helper()
	rows, err := api.pool.Query(context.Background(),
		`SELECT user_id, action, entity, entity_id, before, after FROM audit_log WHERE center_id = $1 ORDER BY id`, centerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		var before, after []byte
		if err := rows.Scan(&r.UserID, &r.Action, &r.Entity, &r.EntityID, &before, &after); err != nil {
			t.Fatal(err)
		}
		if before != nil {
			_ = json.Unmarshal(before, &r.Before)
		}
		if after != nil {
			_ = json.Unmarshal(after, &r.After)
		}
		out = append(out, r)
	}
	return out
}

func auditActions(rows []auditRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out
}

func TestStaffInviteFullFlow(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	ownerMe := owner.register("+996700200001", "Билим")
	centerID := ownerMe.Centers[0].ID

	inv, tok := createInvite(t, owner, centerID, "teacher", "  Бекзат  ")
	if inv.Role != "teacher" || inv.Name == nil || *inv.Name != "Бекзат" {
		t.Errorf("приглашение = %+v", inv)
	}
	if d := time.Until(inv.ExpiresAt); d < 7*24*time.Hour-time.Minute || d > 7*24*time.Hour+time.Minute {
		t.Errorf("срок приглашения %v, want 7 дней", d)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(tok); err != nil || len(raw) != 32 {
		t.Errorf("токен не 32 байта base64url: %q", tok)
	}

	// В БД — только hex(sha256) токена.
	var stored string
	if err := api.pool.QueryRow(context.Background(), `SELECT token FROM invites WHERE id = $1`, inv.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != token.HashHex(tok) || strings.Contains(stored, tok) {
		t.Errorf("в БД token = %q, want hex(sha256) от токена", stored)
	}

	// Список — без токенов.
	rec := owner.do(http.MethodGet, centerPath(centerID, "/staff/invites"), nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), tok) || strings.Contains(rec.Body.String(), stored) ||
		strings.Contains(rec.Body.String(), `"url"`) {
		t.Fatalf("список приглашений: %d %s", rec.Code, rec.Body)
	}
	if list := decodeItems[inviteResp](t, rec); len(list) != 1 || list[0].ID != inv.ID {
		t.Fatalf("список приглашений = %+v", list)
	}

	// Публичная страница приглашения — без входа.
	newbie := api.client(t, "10.0.0.2")
	rec = newbie.do(http.MethodGet, "/api/v1/invites/"+tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET invite: %d %s", rec.Code, rec.Body)
	}
	pub := decode[struct {
		CenterName string  `json:"center_name"`
		Role       string  `json:"role"`
		Name       *string `json:"name"`
	}](t, rec)
	if pub.CenterName != "Билим" || pub.Role != "teacher" || pub.Name == nil || *pub.Name != "Бекзат" {
		t.Errorf("публичное приглашение = %+v", pub)
	}

	// Принятие новым человеком: имя берётся из приглашения, сразу выдаётся сессия.
	rec = newbie.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", acceptReq{Phone: "0700 200 002", Password: "teacher-pass"})
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	me := decode[meResp](t, rec)
	if me.Name != "Бекзат" || me.Phone != "+996700200002" || len(me.Centers) != 1 ||
		me.Centers[0].ID != centerID || me.Centers[0].Role != "teacher" {
		t.Fatalf("accept → me = %+v", me)
	}
	if newbie.sessionToken() == "" {
		t.Fatal("новому сотруднику не выдана сессия")
	}
	if rec := newbie.do(http.MethodGet, "/api/v1/me", nil); rec.Code != http.StatusOK {
		t.Fatalf("me нового сотрудника: %d", rec.Code)
	}

	staff := listStaff(t, owner, centerID)
	if len(staff) != 2 || staff[0].Role != "owner" || staff[0].UserID != ownerMe.ID ||
		staff[1].Role != "teacher" || staff[1].UserID != me.ID || staff[1].Name != "Бекзат" || staff[1].Phone != "+996700200002" {
		t.Fatalf("staff = %+v", staff)
	}

	// Использованное приглашение больше не действует и пропало из списка.
	assertError(t, newbie.do(http.MethodGet, "/api/v1/invites/"+tok, nil), http.StatusNotFound, "invite_invalid")
	if list := decodeItems[inviteResp](t, owner.do(http.MethodGet, centerPath(centerID, "/staff/invites"), nil)); len(list) != 0 {
		t.Errorf("использованное приглашение в списке: %+v", list)
	}

	log := auditLog(t, api, centerID)
	if got := auditActions(log); strings.Join(got, ",") != "invite.create,invite.accept" {
		t.Fatalf("audit = %v", got)
	}
	if c := log[0]; *c.UserID != ownerMe.ID || c.Entity != "invite" || *c.EntityID != inv.ID ||
		c.After["role"] != "teacher" || c.After["name"] != "Бекзат" || c.Before != nil {
		t.Errorf("invite.create = %+v", c)
	}
	if a := log[1]; *a.UserID != me.ID || *a.EntityID != inv.ID || a.After["role"] != "teacher" ||
		a.After["user_id"] != me.ID.String() || a.After["new_user"] != true {
		t.Errorf("invite.accept = %+v", a)
	}
}

// Один человек в двух центрах: владелец центра Б по приглашению становится админом центра А.
func TestAcceptInviteLoggedInUserFromOtherCenter(t *testing.T) {
	api := newTestAPI(t)
	ownerA := api.client(t, "10.0.0.1")
	centerA := ownerA.register("+996700300001", "Центр А").Centers[0].ID
	ownerB := api.client(t, "10.0.0.2")
	meB := ownerB.register("+996700300002", "Центр Б")
	centerB := meB.Centers[0].ID

	_, tok := createInvite(t, ownerA, centerA, "admin", "")

	// Вошедший пользователь принимает без тела.
	rec := ownerB.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	me := decode[meResp](t, rec)
	roles := map[uuid.UUID]string{}
	for _, c := range me.Centers {
		roles[c.ID] = c.Role
	}
	if me.ID != meB.ID || len(me.Centers) != 2 || roles[centerA] != "admin" || roles[centerB] != "owner" {
		t.Fatalf("me = %+v", me)
	}

	// Админ А читает сотрудников А, но не управляет ими; в своём Б остаётся владельцем.
	if staff := listStaff(t, ownerB, centerA); len(staff) != 2 {
		t.Errorf("staff A = %+v", staff)
	}
	assertError(t, ownerB.do(http.MethodPost, centerPath(centerA, "/staff/invites"), map[string]string{"role": "teacher"}),
		http.StatusForbidden, "forbidden")
	createInvite(t, ownerB, centerB, "teacher", "")

	var users int
	_ = api.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&users)
	if users != 2 {
		t.Errorf("пользователей %d, want 2: вошедший не должен создаваться заново", users)
	}
}

func TestInviteInvalid(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996700400001", "Центр").Centers[0].ID
	ctx := context.Background()

	_, expiredTok := createInvite(t, owner, centerID, "teacher", "")
	if _, err := api.pool.Exec(ctx, `UPDATE invites SET expires_at = now() - interval '1 second' WHERE token = $1`,
		token.HashHex(expiredTok)); err != nil {
		t.Fatal(err)
	}

	revoked, revokedTok := createInvite(t, owner, centerID, "teacher", "")
	if rec := owner.do(http.MethodDelete, centerPath(centerID, "/staff/invites/"+revoked.ID.String()), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/invites/"+revoked.ID.String()), nil),
		http.StatusNotFound, "not_found")

	_, usedTok := createInvite(t, owner, centerID, "teacher", "")
	if rec := api.client(t, "10.0.0.2").do(http.MethodPost, "/api/v1/invites/"+usedTok+"/accept",
		acceptReq{Name: "Первый", Phone: "+996700400002", Password: "first-pass"}); rec.Code != http.StatusOK {
		t.Fatalf("первое принятие: %d %s", rec.Code, rec.Body)
	}

	unknownTok, _ := token.New()
	cases := map[string]string{
		"просроченное":     expiredTok,
		"отозванное":       revokedTok,
		"использованное":   usedTok,
		"неизвестное":      unknownTok,
		"мусорный токен":   "not-a-token",
		"hex из БД вместо": token.HashHex(usedTok), // хэш из БД не работает как токен
	}
	var bodies []string
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			c := api.client(t, "10.0.0.3")
			get := c.do(http.MethodGet, "/api/v1/invites/"+tok, nil)
			assertError(t, get, http.StatusNotFound, "invite_invalid")
			assertError(t, c.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept",
				acceptReq{Name: "Кто-то", Phone: "+996700400009", Password: "some-pass"}), http.StatusNotFound, "invite_invalid")
			bodies = append(bodies, get.Body.String())
		})
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("ответы различаются:\n%s\n%s", bodies[0], b)
		}
	}

	var users int
	_ = api.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE phone = '+996700400009'`).Scan(&users)
	if users != 0 {
		t.Error("по недействительному приглашению создан пользователь")
	}
	if got := auditActions(auditLog(t, api, centerID)); strings.Join(got, ",") !=
		"invite.create,invite.create,invite.revoke,invite.create,invite.accept" {
		t.Errorf("audit = %v", got)
	}
}

func TestAcceptLoginRequiredAndAlreadyMember(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996700500001", "Центр").Centers[0].ID
	other := api.client(t, "10.0.0.2")
	other.register("+996700500002", "Другой центр")

	_, tok := createInvite(t, owner, centerID, "teacher", "")
	stillValid := func(t *testing.T) {
		t.Helper()
		if rec := api.client(t, "10.0.0.9").do(http.MethodGet, "/api/v1/invites/"+tok, nil); rec.Code != http.StatusOK {
			t.Fatalf("приглашение испорчено неудачным принятием: %d %s", rec.Code, rec.Body)
		}
	}

	// Без входа с телефоном существующего аккаунта (в другом формате) — нужно войти.
	anon := api.client(t, "10.0.0.3")
	assertError(t, anon.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept",
		acceptReq{Name: "Двойник", Phone: "0700500002", Password: "whatever-pass"}), http.StatusConflict, "login_required")
	if anon.sessionToken() != "" {
		t.Error("выдана сессия при login_required")
	}
	stillValid(t)

	// Ошибка валидации — тоже без последствий.
	assertError(t, anon.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept",
		acceptReq{Name: "Новый", Phone: "+996700500003", Password: "short"}), http.StatusBadRequest, "validation")
	assertError(t, anon.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept",
		acceptReq{Phone: "+996700500003", Password: "long-enough"}), http.StatusBadRequest, "validation") // без имени в приглашении и в теле
	stillValid(t)

	// Владелец открывает приглашение в свой же центр — уже сотрудник.
	assertError(t, owner.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", nil), http.StatusConflict, "already_member")
	stillValid(t)

	// Вошёл и принял.
	if rec := other.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", nil); rec.Code != http.StatusOK {
		t.Fatalf("accept после входа: %d %s", rec.Code, rec.Body)
	}
	if got := auditActions(auditLog(t, api, centerID)); strings.Join(got, ",") != "invite.create,invite.accept" {
		t.Errorf("audit = %v (неудачные попытки не должны попадать в журнал)", got)
	}
}

// Два человека одновременно принимают одно приглашение — проходит ровно один.
func TestAcceptInviteConcurrent(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996700600001", "Центр").Centers[0].ID
	_, tok := createInvite(t, owner, centerID, "teacher", "Учитель")

	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := api.client(t, "10.6.0.1")
			phone := "+99670060010" + string(rune('0'+i))
			codes[i] = c.do(http.MethodPost, "/api/v1/invites/"+tok+"/accept", acceptReq{Phone: phone, Password: "race-pass"}).Code
		}()
	}
	wg.Wait()

	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusNotFound:
		default:
			t.Errorf("неожиданный статус %d", c)
		}
	}
	if ok != 1 {
		t.Errorf("приглашение принято %d раз, want 1 (коды %v)", ok, codes)
	}
	if staff := listStaff(t, owner, centerID); len(staff) != 2 {
		t.Errorf("staff = %+v", staff)
	}
}

func TestStaffPermissions(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996700700001", "Центр").Centers[0].ID
	admin, _ := addStaff(t, api, owner, centerID, "admin", "+996700700002")
	teacher, teacherID := addStaff(t, api, owner, centerID, "teacher", "+996700700003")
	inv, _ := createInvite(t, owner, centerID, "teacher", "")

	ownerOnly := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/staff/invites", map[string]string{"role": "teacher"}},
		{http.MethodGet, "/staff/invites", nil},
		{http.MethodDelete, "/staff/invites/" + inv.ID.String(), nil},
		{http.MethodPatch, "/staff/" + teacherID.String(), map[string]string{"role": "admin"}},
		{http.MethodDelete, "/staff/" + teacherID.String(), nil},
	}
	for _, who := range []struct {
		name string
		c    *client
	}{{"admin", admin}, {"teacher", teacher}} {
		for _, e := range ownerOnly {
			t.Run(who.name+" "+e.method+" "+e.path, func(t *testing.T) {
				assertError(t, who.c.do(e.method, centerPath(centerID, e.path), e.body), http.StatusForbidden, "forbidden")
			})
		}
	}

	// Список сотрудников: owner и admin — да, teacher — нет.
	if staff := listStaff(t, admin, centerID); len(staff) != 3 {
		t.Errorf("admin видит %d сотрудников, want 3", len(staff))
	}
	assertError(t, teacher.do(http.MethodGet, centerPath(centerID, "/staff"), nil), http.StatusForbidden, "forbidden")

	// Ничего из запрещённого не произошло.
	if got := auditActions(auditLog(t, api, centerID)); strings.Count(strings.Join(got, ","), "invite.create") != 3 ||
		strings.Contains(strings.Join(got, ","), "staff.") || strings.Contains(strings.Join(got, ","), "revoke") {
		t.Errorf("audit = %v", got)
	}
}

// Тест изоляции: каждый эндпоинт /c/{id}/staff... для чужого центра — 404, и id из центра А
// не работают через путь своего центра Б.
func TestStaffIsolation(t *testing.T) {
	api := newTestAPI(t)
	ownerA := api.client(t, "10.0.0.1")
	centerA := ownerA.register("+996700800001", "Центр А").Centers[0].ID
	_, teacherA := addStaff(t, api, ownerA, centerA, "teacher", "+996700800002")
	invA, tokA := createInvite(t, ownerA, centerA, "admin", "")

	ownerB := api.client(t, "10.0.0.2")
	centerB := ownerB.register("+996700800003", "Центр Б").Centers[0].ID

	endpoints := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/staff", nil},
		{http.MethodPost, "/staff/invites", map[string]string{"role": "teacher"}},
		{http.MethodGet, "/staff/invites", nil},
		{http.MethodDelete, "/staff/invites/" + invA.ID.String(), nil},
		{http.MethodPatch, "/staff/" + teacherA.String(), map[string]string{"role": "admin"}},
		{http.MethodDelete, "/staff/" + teacherA.String(), nil},
	}
	for _, e := range endpoints {
		t.Run("чужой центр "+e.method+" "+e.path, func(t *testing.T) {
			assertError(t, ownerB.do(e.method, centerPath(centerA, e.path), e.body), http.StatusNotFound, "not_found")
		})
	}
	for _, e := range endpoints[3:] {
		t.Run("id из А через Б "+e.method+" "+e.path, func(t *testing.T) {
			assertError(t, ownerB.do(e.method, centerPath(centerB, e.path), e.body), http.StatusNotFound, "not_found")
		})
	}

	// Список Б не содержит людей и приглашений А.
	if staff := listStaff(t, ownerB, centerB); len(staff) != 1 {
		t.Errorf("staff B = %+v", staff)
	}
	if list := decodeItems[inviteResp](t, ownerB.do(http.MethodGet, centerPath(centerB, "/staff/invites"), nil)); len(list) != 0 {
		t.Errorf("invites B = %+v", list)
	}

	// В А ничего не изменилось.
	staffA := listStaff(t, ownerA, centerA)
	if len(staffA) != 2 || staffA[1].Role != "teacher" {
		t.Errorf("staff A после атаки = %+v", staffA)
	}
	if rec := api.client(t, "10.0.0.3").do(http.MethodGet, "/api/v1/invites/"+tokA, nil); rec.Code != http.StatusOK {
		t.Errorf("приглашение А отозвано из Б: %d", rec.Code)
	}
	if n := len(auditLog(t, api, centerB)); n != 0 {
		t.Errorf("в журнале Б %d записей", n)
	}
}

func TestStaffRoleChangeAndRemove(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	ownerMe := owner.register("+996700900001", "Центр")
	centerID := ownerMe.Centers[0].ID
	teacher, teacherID := addStaff(t, api, owner, centerID, "teacher", "+996700900002")
	ctx := context.Background()

	// teacher → admin.
	rec := owner.do(http.MethodPatch, centerPath(centerID, "/staff/"+teacherID.String()), map[string]string{"role": "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if m := decode[staffResp](t, rec); m.UserID != teacherID || m.Role != "admin" {
		t.Errorf("patch → %+v", m)
	}
	if rec := teacher.do(http.MethodGet, centerPath(centerID, "/staff"), nil); rec.Code != http.StatusOK {
		t.Errorf("новая роль admin не действует сразу: %d", rec.Code)
	}
	// Та же роль — без изменений и без записи в журнал.
	if rec := owner.do(http.MethodPatch, centerPath(centerID, "/staff/"+teacherID.String()), map[string]string{"role": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("patch той же роли: %d", rec.Code)
	}
	// admin → teacher.
	if rec := owner.do(http.MethodPatch, centerPath(centerID, "/staff/"+teacherID.String()), map[string]string{"role": "teacher"}); rec.Code != http.StatusOK {
		t.Fatalf("patch → teacher: %d", rec.Code)
	}

	for _, role := range []string{"owner", "parent", ""} {
		assertError(t, owner.do(http.MethodPatch, centerPath(centerID, "/staff/"+teacherID.String()), map[string]string{"role": role}),
			http.StatusBadRequest, "validation")
	}
	assertError(t, owner.do(http.MethodPost, centerPath(centerID, "/staff/invites"), map[string]string{"role": "owner"}),
		http.StatusBadRequest, "validation")

	// Владельца нельзя ни понизить, ни удалить (в том числе самому себе).
	assertError(t, owner.do(http.MethodPatch, centerPath(centerID, "/staff/"+ownerMe.ID.String()), map[string]string{"role": "admin"}),
		http.StatusConflict, "owner_protected")
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+ownerMe.ID.String()), nil),
		http.StatusConflict, "owner_protected")

	// Несуществующий сотрудник и мусорный id — 404.
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+uuid.NewString()), nil), http.StatusNotFound, "not_found")
	assertError(t, owner.do(http.MethodPatch, centerPath(centerID, "/staff/not-a-uuid"), map[string]string{"role": "admin"}),
		http.StatusNotFound, "not_found")

	// Преподаватель с активной группой — сначала переназначить; архивная группа не мешает.
	var groupID uuid.UUID
	if err := api.pool.QueryRow(ctx, `INSERT INTO groups (center_id, name, teacher_id) VALUES ($1, 'Англ A1', $2) RETURNING id`,
		centerID, teacherID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.pool.Exec(ctx, `INSERT INTO groups (center_id, name, teacher_id, archived) VALUES ($1, 'Старая', $2, true)`,
		centerID, teacherID); err != nil {
		t.Fatal(err)
	}
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+teacherID.String()), nil),
		http.StatusConflict, "teacher_has_groups")

	if _, err := api.pool.Exec(ctx, `UPDATE groups SET archived = true WHERE id = $1`, groupID); err != nil {
		t.Fatal(err)
	}
	if rec := owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+teacherID.String()), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}

	// Доступ пропал сразу, сессия при этом жива (человек может работать в других центрах).
	assertError(t, teacher.do(http.MethodGet, centerPath(centerID, "/staff"), nil), http.StatusNotFound, "not_found")
	if rec := teacher.do(http.MethodGet, "/api/v1/me", nil); rec.Code != http.StatusOK || len(decode[meResp](t, rec).Centers) != 0 {
		t.Errorf("me после удаления: %d %s", rec.Code, rec.Body)
	}
	if staff := listStaff(t, owner, centerID); len(staff) != 1 {
		t.Errorf("staff = %+v", staff)
	}
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+teacherID.String()), nil), http.StatusNotFound, "not_found")

	log := auditLog(t, api, centerID)
	if got := auditActions(log); strings.Join(got, ",") !=
		"invite.create,invite.accept,staff.role_change,staff.role_change,staff.remove" {
		t.Fatalf("audit = %v", got)
	}
	up, down, rm := log[2], log[3], log[4]
	if *up.UserID != ownerMe.ID || up.Entity != "membership" || *up.EntityID != teacherID ||
		up.Before["role"] != "teacher" || up.After["role"] != "admin" {
		t.Errorf("staff.role_change ↑ = %+v", up)
	}
	if down.Before["role"] != "admin" || down.After["role"] != "teacher" {
		t.Errorf("staff.role_change ↓ = %+v", down)
	}
	if *rm.EntityID != teacherID || rm.Before["role"] != "teacher" || rm.Before["phone"] != "+996700900002" || rm.After != nil {
		t.Errorf("staff.remove = %+v", rm)
	}
}
