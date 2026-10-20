package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"okuu/internal/httpx"
)

type tariffResp struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Price        int64     `json:"price"`
	LessonsCount *int32    `json:"lessons_count"`
	Archived     bool      `json:"archived"`
}

type slotJSON struct {
	Dow   int    `json:"dow"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type groupResp struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	TeacherID       *uuid.UUID `json:"teacher_id"`
	TeacherName     *string    `json:"teacher_name"`
	Schedule        []slotJSON `json:"schedule"`
	DefaultTariffID *uuid.UUID `json:"default_tariff_id"`
	Archived        bool       `json:"archived"`
	StudentsCount   int64      `json:"students_count"`
}

func createTariff(t *testing.T, c *client, centerID uuid.UUID, body map[string]any) tariffResp {
	t.Helper()
	rec := c.do(http.MethodPost, centerPath(centerID, "/tariffs"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tariff %v: %d %s", body, rec.Code, rec.Body)
	}
	return decode[tariffResp](t, rec)
}

func monthly(name string, price int64) map[string]any {
	return map[string]any{"name": name, "kind": "monthly", "price": price, "lessons_count": nil}
}

func createGroup(t *testing.T, c *client, centerID uuid.UUID, body map[string]any) groupResp {
	t.Helper()
	rec := c.do(http.MethodPost, centerPath(centerID, "/groups"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group %v: %d %s", body, rec.Code, rec.Body)
	}
	return decode[groupResp](t, rec)
}

// assertField — ошибка validation с указанием поля.
func assertField(t *testing.T, rec *httptest.ResponseRecorder, status int, field string) {
	t.Helper()
	body := decode[httpx.ErrorBody](t, rec)
	if rec.Code != status || body.Error.Code != "validation" || body.Error.Field != field || body.Error.Message == "" {
		t.Fatalf("status=%d error=%+v, want %d validation field=%q", rec.Code, body.Error, status, field)
	}
}

// enrollDirect — запись ученика в группу прямо в БД (эндпоинты учеников появятся на шаге 7).
func enrollDirect(t *testing.T, api *testAPI, centerID, groupID, tariffID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var studentID uuid.UUID
	if err := api.pool.QueryRow(ctx, `INSERT INTO students (center_id, full_name) VALUES ($1, 'Ученик') RETURNING id`,
		centerID).Scan(&studentID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.pool.Exec(ctx, `INSERT INTO enrollments (center_id, student_id, group_id, tariff_id, start_date)
		VALUES ($1, $2, $3, $4, current_date)`, centerID, studentID, groupID, tariffID); err != nil {
		t.Fatal(err)
	}
}

func TestTariffsCRUD(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701000001", "Центр").Centers[0].ID
	admin, _ := addStaff(t, api, owner, centerID, "admin", "+996701000002")

	m := createTariff(t, owner, centerID, monthly("  Английский, месяц ", 300_000))
	if m.Name != "Английский, месяц" || m.Kind != "monthly" || m.Price != 300_000 || m.LessonsCount != nil || m.Archived {
		t.Errorf("monthly = %+v", m)
	}
	// Админ тоже управляет тарифами.
	p := createTariff(t, admin, centerID, map[string]any{"name": "Абонемент 8", "kind": "package", "price": 400_000, "lessons_count": 8})
	if p.LessonsCount == nil || *p.LessonsCount != 8 {
		t.Errorf("package = %+v", p)
	}

	invalid := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"package без lessons_count", map[string]any{"name": "x", "kind": "package", "price": 100}, "lessons_count"},
		{"monthly с lessons_count", map[string]any{"name": "x", "kind": "monthly", "price": 100, "lessons_count": 4}, "lessons_count"},
		{"отрицательная цена", map[string]any{"name": "x", "kind": "monthly", "price": -100}, "price"},
		{"цена выше максимума", map[string]any{"name": "x", "kind": "monthly", "price": 1_000_000_001}, "price"},
		{"неизвестный kind", map[string]any{"name": "x", "kind": "yearly", "price": 100}, "kind"},
		{"без названия", map[string]any{"kind": "monthly", "price": 100}, "name"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			assertField(t, owner.do(http.MethodPost, centerPath(centerID, "/tariffs"), tt.body), http.StatusBadRequest, tt.field)
		})
	}
	// Дробная цена — не JSON-число int64.
	assertError(t, owner.do(http.MethodPost, centerPath(centerID, "/tariffs"),
		map[string]any{"name": "x", "kind": "monthly", "price": 100.5}), http.StatusBadRequest, "bad_request")

	list := decode[[]tariffResp](t, owner.do(http.MethodGet, centerPath(centerID, "/tariffs"), nil))
	if len(list) != 2 || list[0].ID != p.ID || list[1].ID != m.ID { // по названию: «Абонемент…» < «Английский…»
		t.Fatalf("list = %+v", list)
	}

	// PATCH цены (R6: действует со следующего начисления — сейчас просто обновляем).
	rec := owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()), map[string]any{"price": 350_000})
	if rec.Code != http.StatusOK || decode[tariffResp](t, rec).Price != 350_000 {
		t.Fatalf("patch price: %d %s", rec.Code, rec.Body)
	}
	// Без изменений — 200 и без записи в журнал.
	if rec := owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()), map[string]any{"price": 350_000, "name": "Английский, месяц"}); rec.Code != http.StatusOK {
		t.Fatalf("no-op patch: %d", rec.Code)
	}
	// lessons_count: null у абонемента — нельзя.
	assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+p.ID.String()), map[string]any{"lessons_count": nil}),
		http.StatusBadRequest, "lessons_count")
	// Смена kind без записей учеников — можно (вместе с lessons_count).
	rec = owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()), map[string]any{"kind": "package", "lessons_count": 12})
	if rec.Code != http.StatusOK || decode[tariffResp](t, rec).Kind != "package" {
		t.Fatalf("patch kind: %d %s", rec.Code, rec.Body)
	}
	// package → monthly без сброса lessons_count — ошибка, со сбросом — ок.
	assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()), map[string]any{"kind": "monthly"}),
		http.StatusBadRequest, "lessons_count")
	if rec := owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()),
		map[string]any{"kind": "monthly", "lessons_count": nil}); rec.Code != http.StatusOK {
		t.Fatalf("patch back to monthly: %d %s", rec.Code, rec.Body)
	}
	assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+m.ID.String()), map[string]any{"name": nil}),
		http.StatusBadRequest, "name")
	assertError(t, owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+uuid.NewString()), map[string]any{"price": 1}),
		http.StatusNotFound, "not_found")

	// Архив: из активных пропадает, в ?archived=true виден; повтор — 204 без записи в журнал.
	for range 2 {
		if rec := owner.do(http.MethodDelete, centerPath(centerID, "/tariffs/"+p.ID.String()), nil); rec.Code != http.StatusNoContent {
			t.Fatalf("archive: %d %s", rec.Code, rec.Body)
		}
	}
	if list := decode[[]tariffResp](t, owner.do(http.MethodGet, centerPath(centerID, "/tariffs?archived=false"), nil)); len(list) != 1 || list[0].ID != m.ID {
		t.Errorf("активные = %+v", list)
	}
	if list := decode[[]tariffResp](t, owner.do(http.MethodGet, centerPath(centerID, "/tariffs?archived=true"), nil)); len(list) != 1 || list[0].ID != p.ID || !list[0].Archived {
		t.Errorf("архив = %+v", list)
	}
	assertField(t, owner.do(http.MethodGet, centerPath(centerID, "/tariffs?archived=maybe"), nil), http.StatusBadRequest, "archived")

	log := auditLog(t, api, centerID)
	var tariffLog []auditRow
	for _, r := range log {
		if strings.HasPrefix(r.Action, "tariff.") {
			tariffLog = append(tariffLog, r)
		}
	}
	if got := strings.Join(auditActions(tariffLog), ","); got !=
		"tariff.create,tariff.create,tariff.update,tariff.update,tariff.update,tariff.archive" {
		t.Fatalf("audit = %s", got)
	}
	if u := tariffLog[2]; u.Before["price"] != float64(300_000) || u.After["price"] != float64(350_000) || *u.EntityID != m.ID {
		t.Errorf("tariff.update = %+v", u)
	}
}

func TestTariffInUse(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701100001", "Центр").Centers[0].ID
	tariff := createTariff(t, owner, centerID, monthly("Месяц", 300_000))
	group := createGroup(t, owner, centerID, map[string]any{"name": "Группа"})
	enrollDirect(t, api, centerID, group.ID, tariff.ID)

	assertError(t, owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+tariff.ID.String()),
		map[string]any{"kind": "package", "lessons_count": 8}), http.StatusConflict, "tariff_in_use")

	// Цену и название менять можно.
	rec := owner.do(http.MethodPatch, centerPath(centerID, "/tariffs/"+tariff.ID.String()), map[string]any{"price": 320_000, "name": "Месяц 2026"})
	if rec.Code != http.StatusOK || decode[tariffResp](t, rec).Price != 320_000 {
		t.Fatalf("patch price: %d %s", rec.Code, rec.Body)
	}
	// Архив тарифа с учениками — можно: запрещены только новые назначения.
	if rec := owner.do(http.MethodDelete, centerPath(centerID, "/tariffs/"+tariff.ID.String()), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("archive: %d", rec.Code)
	}

	// Счётчик учеников в группе — по незавершённым записям.
	if g := decode[groupResp](t, owner.do(http.MethodGet, centerPath(centerID, "/groups/"+group.ID.String()), nil)); g.StudentsCount != 1 {
		t.Errorf("students_count = %d, want 1", g.StudentsCount)
	}
}

func TestGroupsCRUD(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701200001", "Центр").Centers[0].ID
	_, teacherID := addStaff(t, api, owner, centerID, "teacher", "+996701200002")
	tariff := createTariff(t, owner, centerID, monthly("Месяц", 300_000))

	schedule := []slotJSON{{1, "16:00", "17:30"}, {3, "16:00", "17:30"}}
	g := createGroup(t, owner, centerID, map[string]any{
		"name": "Английский A1", "teacher_id": teacherID, "schedule": schedule, "default_tariff_id": tariff.ID,
	})
	if g.Name != "Английский A1" || g.TeacherID == nil || *g.TeacherID != teacherID || g.TeacherName == nil ||
		*g.TeacherName != "Сотрудник +996701200002" || len(g.Schedule) != 2 || g.Schedule[1] != schedule[1] ||
		g.DefaultTariffID == nil || *g.DefaultTariffID != tariff.ID || g.StudentsCount != 0 || g.Archived {
		t.Fatalf("group = %+v", g)
	}

	// Без преподавателя, тарифа и расписания — schedule = [], а не null.
	bare := createGroup(t, owner, centerID, map[string]any{"name": "Шахматы"})
	if bare.Schedule == nil || len(bare.Schedule) != 0 || bare.TeacherID != nil || bare.TeacherName != nil {
		t.Errorf("bare = %+v", bare)
	}
	if rec := owner.do(http.MethodGet, centerPath(centerID, "/groups/"+bare.ID.String()), nil); !strings.Contains(rec.Body.String(), `"schedule":[]`) {
		t.Errorf("schedule не []: %s", rec.Body)
	}

	scheduleErrors := []struct {
		name     string
		schedule []slotJSON
	}{
		{"пересечение", []slotJSON{{2, "10:00", "11:30"}, {2, "11:00", "12:00"}}},
		{"end = start", []slotJSON{{2, "10:00", "10:00"}}},
		{"end < start", []slotJSON{{2, "12:00", "10:00"}}},
		{"dow 0", []slotJSON{{0, "10:00", "11:00"}}},
		{"dow 8", []slotJSON{{8, "10:00", "11:00"}}},
		{"время не HH:MM", []slotJSON{{2, "9:00", "10:00"}}},
	}
	for _, tt := range scheduleErrors {
		t.Run("POST "+tt.name, func(t *testing.T) {
			assertField(t, owner.do(http.MethodPost, centerPath(centerID, "/groups"), map[string]any{"name": "x", "schedule": tt.schedule}),
				http.StatusBadRequest, "schedule")
		})
		t.Run("PATCH "+tt.name, func(t *testing.T) {
			assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"schedule": tt.schedule}),
				http.StatusBadRequest, "schedule")
		})
	}
	// Неизвестное поле внутри элемента расписания — тело не принимается.
	assertError(t, owner.do(http.MethodPost, centerPath(centerID, "/groups"),
		map[string]any{"name": "x", "schedule": []map[string]any{{"dow": 1, "start": "10:00", "end": "11:00", "room": "5"}}}),
		http.StatusBadRequest, "bad_request")
}

func TestGroupsPatchAndArchive(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701300001", "Центр").Centers[0].ID
	_, teacherID := addStaff(t, api, owner, centerID, "teacher", "+996701300002")
	tariff := createTariff(t, owner, centerID, monthly("Месяц", 300_000))
	g := createGroup(t, owner, centerID, map[string]any{"name": "Группа", "teacher_id": teacherID, "default_tariff_id": tariff.ID})

	// Частичный PATCH: меняем только название, остальное на месте.
	rec := owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"name": "Группа 2"})
	if got := decode[groupResp](t, rec); rec.Code != http.StatusOK || got.Name != "Группа 2" || got.TeacherID == nil || got.DefaultTariffID == nil {
		t.Fatalf("patch name: %d %+v", rec.Code, got)
	}
	// null снимает преподавателя и тариф.
	rec = owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"teacher_id": nil, "default_tariff_id": nil})
	if got := decode[groupResp](t, rec); rec.Code != http.StatusOK || got.TeacherID != nil || got.TeacherName != nil || got.DefaultTariffID != nil {
		t.Fatalf("patch nulls: %d %+v", rec.Code, got)
	}
	// Расписание — массив; null нельзя, [] можно.
	assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"schedule": nil}),
		http.StatusBadRequest, "schedule")
	rec = owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"schedule": []slotJSON{{6, "10:00", "12:00"}}})
	if got := decode[groupResp](t, rec); rec.Code != http.StatusOK || len(got.Schedule) != 1 || got.Schedule[0].Dow != 6 {
		t.Fatalf("patch schedule: %d %+v", rec.Code, got)
	}

	// Архив; повтор — 204 без новой записи; в списке активных группы нет.
	for range 2 {
		if rec := owner.do(http.MethodDelete, centerPath(centerID, "/groups/"+g.ID.String()), nil); rec.Code != http.StatusNoContent {
			t.Fatalf("archive: %d", rec.Code)
		}
	}
	if list := decode[[]groupResp](t, owner.do(http.MethodGet, centerPath(centerID, "/groups"), nil)); len(list) != 0 {
		t.Errorf("активные = %+v", list)
	}
	if list := decode[[]groupResp](t, owner.do(http.MethodGet, centerPath(centerID, "/groups?archived=true"), nil)); len(list) != 1 || !list[0].Archived {
		t.Errorf("архив = %+v", list)
	}
	assertError(t, owner.do(http.MethodGet, centerPath(centerID, "/groups/"+uuid.NewString()), nil), http.StatusNotFound, "not_found")

	var groupLog []auditRow
	for _, r := range auditLog(t, api, centerID) {
		if strings.HasPrefix(r.Action, "group.") {
			groupLog = append(groupLog, r)
		}
	}
	if got := strings.Join(auditActions(groupLog), ","); got != "group.create,group.update,group.update,group.update,group.archive" {
		t.Fatalf("audit = %s", got)
	}
	if u := groupLog[2]; u.Before["teacher_id"] != teacherID.String() || u.After["teacher_id"] != nil || u.After["name"] != "Группа 2" {
		t.Errorf("group.update (nulls) = %+v", u)
	}
}

// Преподаватель и тариф проверяются в БД этого центра: чужие, архивные и бывшие сотрудники — 422 с полем.
func TestGroupReferences(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701400001", "Центр А").Centers[0].ID
	_, teacherID := addStaff(t, api, owner, centerID, "teacher", "+996701400002")
	_, formerID := addStaff(t, api, owner, centerID, "teacher", "+996701400003")
	if rec := owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+formerID.String()), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("remove staff: %d", rec.Code)
	}
	ownTariff := createTariff(t, owner, centerID, monthly("Месяц", 300_000))
	archived := createTariff(t, owner, centerID, monthly("Старый", 200_000))
	owner.do(http.MethodDelete, centerPath(centerID, "/tariffs/"+archived.ID.String()), nil)

	other := api.client(t, "10.0.0.2")
	otherMe := other.register("+996701400004", "Центр Б")
	otherTariff := createTariff(t, other, otherMe.Centers[0].ID, monthly("Чужой", 100_000))
	var stranger uuid.UUID // зарегистрирован, но ни в одном центре с нами
	_ = api.pool.QueryRow(context.Background(), `INSERT INTO users (phone, name, password_hash) VALUES ('+996701400005', 'Никто', 'x') RETURNING id`).Scan(&stranger)

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"учитель из другого центра", map[string]any{"name": "x", "teacher_id": otherMe.ID}, "teacher_id"},
		{"не сотрудник", map[string]any{"name": "x", "teacher_id": stranger}, "teacher_id"},
		{"удалённый сотрудник", map[string]any{"name": "x", "teacher_id": formerID}, "teacher_id"},
		{"несуществующий uuid", map[string]any{"name": "x", "teacher_id": uuid.New()}, "teacher_id"},
		{"тариф другого центра", map[string]any{"name": "x", "default_tariff_id": otherTariff.ID}, "default_tariff_id"},
		{"архивный тариф", map[string]any{"name": "x", "default_tariff_id": archived.ID}, "default_tariff_id"},
	}
	g := createGroup(t, owner, centerID, map[string]any{"name": "Группа", "teacher_id": teacherID, "default_tariff_id": ownTariff.ID})
	for _, tt := range cases {
		t.Run("POST "+tt.name, func(t *testing.T) {
			assertField(t, owner.do(http.MethodPost, centerPath(centerID, "/groups"), tt.body), http.StatusUnprocessableEntity, tt.field)
		})
		t.Run("PATCH "+tt.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range tt.body {
				if k != "name" {
					body[k] = v
				}
			}
			assertField(t, owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), body), http.StatusUnprocessableEntity, tt.field)
		})
	}
	assertError(t, owner.do(http.MethodPost, centerPath(centerID, "/groups"), map[string]any{"name": "x", "teacher_id": "not-a-uuid"}),
		http.StatusBadRequest, "bad_request")

	// Сам владелец может вести группу.
	ownerMe := decode[meResp](t, owner.do(http.MethodGet, "/api/v1/me", nil))
	createGroup(t, owner, centerID, map[string]any{"name": "Ведёт владелец", "teacher_id": ownerMe.ID})

	// Тариф группы архивировали — группу можно менять дальше, тариф остаётся (не «назначается заново»).
	owner.do(http.MethodDelete, centerPath(centerID, "/tariffs/"+ownTariff.ID.String()), nil)
	rec := owner.do(http.MethodPatch, centerPath(centerID, "/groups/"+g.ID.String()), map[string]any{"name": "Группа (переим.)"})
	if got := decode[groupResp](t, rec); rec.Code != http.StatusOK || got.DefaultTariffID == nil || *got.DefaultTariffID != ownTariff.ID {
		t.Fatalf("patch с архивным тарифом: %d %+v", rec.Code, got)
	}

	// Группа есть — удалить её преподавателя нельзя (связка со staff).
	assertError(t, owner.do(http.MethodDelete, centerPath(centerID, "/staff/"+teacherID.String()), nil), http.StatusConflict, "teacher_has_groups")
}

func TestTeacherSeesOnlyOwnGroups(t *testing.T) {
	api := newTestAPI(t)
	owner := api.client(t, "10.0.0.1")
	centerID := owner.register("+996701500001", "Центр").Centers[0].ID
	t1, t1ID := addStaff(t, api, owner, centerID, "teacher", "+996701500002")
	_, t2ID := addStaff(t, api, owner, centerID, "teacher", "+996701500003")
	mine := createGroup(t, owner, centerID, map[string]any{"name": "Моя", "teacher_id": t1ID})
	theirs := createGroup(t, owner, centerID, map[string]any{"name": "Чужая", "teacher_id": t2ID})
	nobody := createGroup(t, owner, centerID, map[string]any{"name": "Без учителя"})

	list := decode[[]groupResp](t, t1.do(http.MethodGet, centerPath(centerID, "/groups"), nil))
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("учитель видит %+v, want только «Моя»", list)
	}
	if rec := t1.do(http.MethodGet, centerPath(centerID, "/groups/"+mine.ID.String()), nil); rec.Code != http.StatusOK {
		t.Errorf("своя группа: %d", rec.Code)
	}
	for _, g := range []groupResp{theirs, nobody} {
		assertError(t, t1.do(http.MethodGet, centerPath(centerID, "/groups/"+g.ID.String()), nil), http.StatusNotFound, "not_found")
	}
	if all := decode[[]groupResp](t, owner.do(http.MethodGet, centerPath(centerID, "/groups"), nil)); len(all) != 3 {
		t.Errorf("владелец видит %d групп, want 3", len(all))
	}

	// Менять группы, даже свою, и трогать тарифы (деньги) учитель не может.
	forbidden := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/groups", map[string]any{"name": "x"}},
		{http.MethodPatch, "/groups/" + mine.ID.String(), map[string]any{"name": "x"}},
		{http.MethodDelete, "/groups/" + mine.ID.String(), nil},
		{http.MethodGet, "/tariffs", nil},
		{http.MethodPost, "/tariffs", monthly("x", 1)},
		{http.MethodPatch, "/tariffs/" + uuid.NewString(), map[string]any{"price": 1}},
		{http.MethodDelete, "/tariffs/" + uuid.NewString(), nil},
	}
	for _, f := range forbidden {
		t.Run(f.method+" "+f.path, func(t *testing.T) {
			assertError(t, t1.do(f.method, centerPath(centerID, f.path), f.body), http.StatusForbidden, "forbidden")
		})
	}
}

// Тест изоляции: каждый эндпоинт /tariffs и /groups для чужого центра — 404,
// и id из центра А не работают через путь своего центра Б.
func TestCatalogIsolation(t *testing.T) {
	api := newTestAPI(t)
	ownerA := api.client(t, "10.0.0.1")
	centerA := ownerA.register("+996701600001", "Центр А").Centers[0].ID
	tariffA := createTariff(t, ownerA, centerA, monthly("Тариф А", 300_000))
	groupA := createGroup(t, ownerA, centerA, map[string]any{"name": "Группа А", "default_tariff_id": tariffA.ID})

	ownerB := api.client(t, "10.0.0.2")
	centerB := ownerB.register("+996701600002", "Центр Б").Centers[0].ID

	endpoints := []struct {
		method, path string
		body         any
		withID       bool // путь содержит id сущности центра А
	}{
		{http.MethodGet, "/tariffs", nil, false},
		{http.MethodPost, "/tariffs", monthly("Взлом", 1), false},
		{http.MethodPatch, "/tariffs/" + tariffA.ID.String(), map[string]any{"price": 1}, true},
		{http.MethodDelete, "/tariffs/" + tariffA.ID.String(), nil, true},
		{http.MethodGet, "/groups", nil, false},
		{http.MethodGet, "/groups/" + groupA.ID.String(), nil, true},
		{http.MethodPost, "/groups", map[string]any{"name": "Взлом"}, false},
		{http.MethodPatch, "/groups/" + groupA.ID.String(), map[string]any{"name": "Взлом"}, true},
		{http.MethodDelete, "/groups/" + groupA.ID.String(), nil, true},
	}
	for _, e := range endpoints {
		t.Run("чужой центр "+e.method+" "+e.path, func(t *testing.T) {
			assertError(t, ownerB.do(e.method, centerPath(centerA, e.path), e.body), http.StatusNotFound, "not_found")
		})
		if e.withID {
			t.Run("id из А через Б "+e.method+" "+e.path, func(t *testing.T) {
				assertError(t, ownerB.do(e.method, centerPath(centerB, e.path), e.body), http.StatusNotFound, "not_found")
			})
		}
	}
	// Тариф А нельзя назначить группе Б даже по точному id.
	assertField(t, ownerB.do(http.MethodPost, centerPath(centerB, "/groups"), map[string]any{"name": "x", "default_tariff_id": tariffA.ID}),
		http.StatusUnprocessableEntity, "default_tariff_id")

	// В А ничего не изменилось, в Б пусто.
	tA := decode[[]tariffResp](t, ownerA.do(http.MethodGet, centerPath(centerA, "/tariffs"), nil))
	gA := decode[[]groupResp](t, ownerA.do(http.MethodGet, centerPath(centerA, "/groups"), nil))
	if len(tA) != 1 || tA[0].Price != 300_000 || tA[0].Archived || len(gA) != 1 || gA[0].Name != "Группа А" {
		t.Errorf("А после атаки: tariffs=%+v groups=%+v", tA, gA)
	}
	if n := len(decode[[]tariffResp](t, ownerB.do(http.MethodGet, centerPath(centerB, "/tariffs"), nil))); n != 0 {
		t.Errorf("тарифов в Б: %d", n)
	}
	if n := len(decode[[]groupResp](t, ownerB.do(http.MethodGet, centerPath(centerB, "/groups"), nil))); n != 0 {
		t.Errorf("групп в Б: %d", n)
	}
	if n := len(auditLog(t, api, centerB)); n != 0 {
		t.Errorf("в журнале Б %d записей", n)
	}
}
