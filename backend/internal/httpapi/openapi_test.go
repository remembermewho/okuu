package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"

	"okuu/internal/payments"
)

// Спецификация для фронтенда — docs/openapi.yaml. Здесь два теста:
//   - сверка: каждый маршрут роутера под /api/v1 описан в спецификации, и наоборот;
//   - реальные ответы ключевых эндпоинтов (и сами запросы) проходят валидацию по схеме.
//
// Валидатор — github.com/pb33f/libopenapi-validator: поддерживает OpenAPI 3.1 и JSON Schema
// 2020-12 (type: [string, "null"], unevaluatedProperties), проверяет *http.Request и *http.Response
// целиком — путь, параметры, заголовки, тело, коды ответов. kin-openapi полноценно умеет только
// 3.0. Библиотека подключена только в тестах и в бинарник сервера не попадает.

const specPath = "../../../docs/openapi.yaml"

func loadSpec(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(specPath))
	if err != nil {
		t.Fatalf("спецификация: %v", err)
	}
	return b
}

// specRoutes — «МЕТОД путь» всех операций спецификации.
func specRoutes(t *testing.T, spec []byte) []string {
	t.Helper()
	doc, err := libopenapi.NewDocument(spec)
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("спецификация не собирается: %v", err)
	}
	var out []string
	for path, item := range model.Model.Paths.PathItems.FromOldest() {
		for method := range item.GetOperations().FromOldest() {
			out = append(out, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(out)
	return out
}

// apiRoutes — «МЕТОД шаблон» маршрутов роутера под /api/v1.
func apiRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	var out []string
	err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			out = append(out, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// Сверка, как в тесте изоляции: новый маршрут без описания в openapi.yaml — тест красный.
func TestOpenAPIMatchesRouter(t *testing.T) {
	api := newTestAPI(t)
	inRouter, inSpec := apiRoutes(t, api.h), specRoutes(t, loadSpec(t))
	var missing, stale []string
	for _, r := range inRouter {
		if !slices.Contains(inSpec, r) {
			missing = append(missing, r)
		}
	}
	for _, r := range inSpec {
		if !slices.Contains(inRouter, r) {
			stale = append(stale, r)
		}
	}
	if len(missing) > 0 {
		t.Errorf("маршруты без описания в docs/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("в docs/openapi.yaml есть пути, которых нет в роутере:\n  %s", strings.Join(stale, "\n  "))
	}
	if len(inRouter) < 50 {
		t.Errorf("маршрутов под /api/v1 всего %d — обход роутера сломался?", len(inRouter))
	}
}

// specValidator — валидатор запросов и ответов по спецификации (с проверкой форматов uuid, date…).
func specValidator(t *testing.T) validator.Validator {
	t.Helper()
	doc, err := libopenapi.NewDocument(loadSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	v, errs := validator.NewValidator(doc, config.WithFormatAssertions())
	if len(errs) > 0 {
		t.Fatalf("валидатор: %v", errs)
	}
	if ok, verrs := v.ValidateDocument(); !ok {
		for _, e := range verrs {
			t.Errorf("спецификация: %s: %s", e.Message, e.Reason)
		}
		t.FailNow()
	}
	return v
}

// checked выполняет запрос клиентом и проверяет по спецификации и запрос, и ответ.
func checked(t *testing.T, v validator.Validator, c *client, method, path string, body any, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	// Тот же запрос, что отправит client.do (заголовки, cookie), — для валидатора.
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	for k, val := range c.headers {
		req.Header.Set(k, val)
	}
	if method != http.MethodGet && c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if body == nil {
		req.Body, req.ContentLength = http.NoBody, 0
		req.Header.Del("Content-Type")
	}

	rec := c.do(method, path, body)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, rec.Code, wantStatus, rec.Body)
	}
	resp := rec.Result()
	resp.Request = req
	// Запрос с ошибкой (4xx) нарочно не по спецификации — у него проверяем только ответ.
	validate := v.ValidateHttpRequestResponse
	if wantStatus >= 400 {
		validate = v.ValidateHttpResponse
	}
	if ok, errs := validate(req, resp); !ok {
		for _, e := range errs {
			msg := e.Message + ": " + e.Reason
			for _, se := range e.SchemaValidationErrors {
				msg += "\n    " + se.FieldPath + " (" + se.KeywordLocation + "): " + se.Reason
			}
			t.Errorf("%s %s (%d) не по спецификации: %s", method, path, rec.Code, msg)
		}
		t.Logf("тело ответа: %s", rec.Body)
	}
	return rec
}

// Реальные ответы ключевых эндпоинтов — по схеме из docs/openapi.yaml.
func TestOpenAPIResponsesMatchSpec(t *testing.T) {
	v := specValidator(t)
	api := newTestAPI(t)
	c := api.client(t, "10.0.0.1")

	// auth
	me := decode[meResp](t, checked(t, v, c, http.MethodPost, "/api/v1/auth/register",
		registerReq{Phone: "0709 500 001", Password: "secret-pass", Name: "Владелец", CenterName: "Спека"}, http.StatusCreated))
	center := me.Centers[0].ID
	cp := func(rest string) string { return centerPath(center, rest) }
	checked(t, v, c, http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	checked(t, v, api.client(t, "10.0.0.9"), http.MethodPost, "/api/v1/auth/login",
		loginReq{Phone: "+996709500001", Password: "wrong-pass"}, http.StatusUnauthorized)
	checked(t, v, api.client(t, "10.0.0.8"), http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)

	// настройки
	checked(t, v, c, http.MethodPatch, cp("/settings"), map[string]any{"pay_details": "MBank 0700", "billing_day": 15, "phone": nil, "overdue_repeat": "off"}, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/settings"), nil, http.StatusOK)

	// сотрудники и приглашения
	inv := decode[inviteResp](t, checked(t, v, c, http.MethodPost, cp("/staff/invites"), map[string]string{"role": "teacher", "name": "Учитель"}, http.StatusCreated))
	checked(t, v, c, http.MethodGet, cp("/staff/invites"), nil, http.StatusOK)
	inv = decode[inviteResp](t, checked(t, v, c, http.MethodPost, cp("/staff/invites/"+inv.ID.String()+"/reissue"), nil, http.StatusCreated))
	checked(t, v, c, http.MethodGet, "/api/v1/invites/"+strings.TrimPrefix(inv.URL, testOrigin+"/invite/"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/staff"), nil, http.StatusOK)

	// личный кабинет и сброс пароля
	checked(t, v, c, http.MethodPatch, "/api/v1/me", map[string]string{"name": "Владелец Спеки"}, http.StatusOK)
	checked(t, v, c, http.MethodPost, "/api/v1/me/password", map[string]string{"current_password": "secret-pass", "new_password": "secret-pass-2"}, http.StatusNoContent)
	checked(t, v, c, http.MethodPost, "/api/v1/me/password", map[string]string{"current_password": "wrong-pass", "new_password": "secret-pass-3"}, http.StatusBadRequest)
	staffTok := strings.TrimPrefix(inv.URL, testOrigin+"/invite/")
	staffer := api.client(t, "10.0.0.6")
	staffMe := decode[meResp](t, checked(t, v, staffer, http.MethodPost, "/api/v1/invites/"+staffTok+"/accept",
		acceptReq{Name: "Учитель", Phone: "0709 500 002", Password: "staff-pass"}, http.StatusOK))
	reset := decode[resetLinkResp](t, checked(t, v, c, http.MethodPost, cp("/staff/"+staffMe.ID.String()+"/password-reset"), nil, http.StatusCreated))
	resetTok := strings.TrimPrefix(reset.URL, testOrigin+"/reset/")
	checked(t, v, staffer, http.MethodGet, "/api/v1/password-reset/"+resetTok, nil, http.StatusOK)
	checked(t, v, staffer, http.MethodPost, "/api/v1/password-reset/"+resetTok, map[string]string{"new_password": "staff-pass-2"}, http.StatusNoContent)
	checked(t, v, staffer, http.MethodGet, "/api/v1/password-reset/"+resetTok, nil, http.StatusNotFound)
	checked(t, v, c, http.MethodPost, cp("/staff/"+me.ID.String()+"/password-reset"), nil, http.StatusConflict)
	other := api.client(t, "10.0.0.5")
	other.register("0709 500 003", "Вторая")
	checked(t, v, other, http.MethodPost, "/api/v1/auth/logout-all", nil, http.StatusNoContent)

	// каталог
	monthly := decode[tariffResp](t, checked(t, v, c, http.MethodPost, cp("/tariffs"), monthly("Месяц", 300_000), http.StatusCreated))
	pkg := decode[tariffResp](t, checked(t, v, c, http.MethodPost, cp("/tariffs"),
		map[string]any{"name": "Абонемент 8", "kind": "package", "price": 400_000, "lessons_count": 8}, http.StatusCreated))
	checked(t, v, c, http.MethodGet, cp("/tariffs"), nil, http.StatusOK)
	group := decode[groupResp](t, checked(t, v, c, http.MethodPost, cp("/groups"), map[string]any{
		"name": "Английский", "default_tariff_id": monthly.ID, "schedule": []map[string]any{{"dow": 1, "start": "16:00", "end": "17:30"}},
	}, http.StatusCreated))
	checked(t, v, c, http.MethodGet, cp("/groups/"+group.ID.String()), nil, http.StatusOK)

	// ученики
	st := decode[studentResp](t, checked(t, v, c, http.MethodPost, cp("/students"), map[string]any{
		"full_name": "Асанов Бакыт", "parent": map[string]string{"name": "Асанова Гульнара", "phone": "0709 500 101"},
		"discount": map[string]any{"kind": "percent", "value": 10},
	}, http.StatusCreated))
	pst := decode[studentResp](t, checked(t, v, c, http.MethodPost, cp("/students"), map[string]any{
		"full_name": "Бекова Айпери", "parent": map[string]string{"name": "Бекова Нургуль", "phone": "0709 500 102"},
	}, http.StatusCreated))
	today := todayIn(t, api)
	checked(t, v, c, http.MethodPost, cp("/students/"+st.ID.String()+"/enrollments"),
		map[string]any{"group_id": group.ID, "start_date": today, "first_month": "full"}, http.StatusCreated)
	checked(t, v, c, http.MethodPost, cp("/students/"+pst.ID.String()+"/enrollments"),
		map[string]any{"group_id": group.ID, "tariff_id": pkg.ID, "start_date": today}, http.StatusCreated)
	checked(t, v, c, http.MethodGet, cp("/students?limit=1"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/students?hide_left=true"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/students/"+st.ID.String()), nil, http.StatusOK)
	checked(t, v, c, http.MethodPatch, cp("/students/"+st.ID.String()), map[string]any{"birth_date": "2015-03-01", "discount": nil}, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/students/"+uuid.NewString()), nil, http.StatusNotFound)
	checked(t, v, c, http.MethodPost, cp("/students"), map[string]any{"full_name": "", "parent": map[string]string{"name": "x", "phone": "1"}}, http.StatusBadRequest)

	// деньги
	c.headers = map[string]string{"Idempotency-Key": uuid.NewString()}
	checked(t, v, c, http.MethodPost, cp("/students/"+st.ID.String()+"/payments"), pay(100_000), http.StatusCreated)
	c.headers = nil
	checked(t, v, c, http.MethodGet, cp("/students/"+st.ID.String()+"/ledger"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/debtors"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/debtors?status=pending"), nil, http.StatusOK)
	checked(t, v, c, http.MethodPost, cp("/reminders"), map[string]any{"all": true}, http.StatusConflict) // бот выключен

	// импорт: превью (multipart) и подтверждение; first_month — только full | none, как в бэкенде
	csv := "ФИО ученика*;Имя родителя*;Телефон родителя*;Группа*;Тариф*\nИмпортов Азамат;Гульнара;0709 500 201;Английский;Месяц\n"
	rec := c.upload(cp("/import/preview"), "students.csv", []byte(csv))
	if rec.Code != http.StatusOK {
		t.Fatalf("import/preview: %d %s", rec.Code, rec.Body)
	}
	previewReq := httptest.NewRequest(http.MethodPost, cp("/import/preview"), http.NoBody)
	previewHTTP := rec.Result()
	previewHTTP.Request = previewReq
	if ok, errs := v.ValidateHttpResponse(previewReq, previewHTTP); !ok {
		for _, e := range errs {
			t.Errorf("import/preview: ответ не по спецификации: %s: %s", e.Message, e.Reason)
		}
	}
	prev := decode[previewResp](t, rec)
	manual := map[string]any{"preview_id": prev.PreviewID, "first_month": "manual"}
	if specAcceptsRequest(t, v, c, http.MethodPost, cp("/import/commit"), manual) {
		t.Error("спецификация принимает first_month = manual у /import/commit, а бэкенд — нет")
	}
	checked(t, v, c, http.MethodPost, cp("/import/commit"), manual, http.StatusBadRequest)
	checked(t, v, c, http.MethodPost, cp("/import/commit"), map[string]any{"preview_id": prev.PreviewID, "first_month": "none"}, http.StatusOK)

	// посещаемость
	lesson := decode[lessonResp](t, checked(t, v, c, http.MethodPost, cp("/lessons"),
		map[string]any{"group_id": group.ID, "date": today, "start_time": "08:00"}, http.StatusCreated))
	checked(t, v, c, http.MethodGet, cp("/lessons?date="+today), nil, http.StatusOK)
	checked(t, v, c, http.MethodPut, cp("/lessons/"+lesson.ID.String()+"/attendance"), map[string]any{"marks": []map[string]any{
		{"student_id": st.ID, "status": "present"}, {"student_id": pst.ID, "status": "absent"},
	}}, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/students/"+pst.ID.String()+"/attendance"), nil, http.StatusOK)
	leaver := decode[studentResp](t, checked(t, v, c, http.MethodPost, cp("/students"), map[string]any{
		"full_name": "Уходящий Ученик", "parent": map[string]string{"name": "Родитель", "phone": "0709 500 103"},
	}, http.StatusCreated))
	checked(t, v, c, http.MethodPatch, cp("/students/"+leaver.ID.String()), map[string]any{"status": "left", "left_date": today}, http.StatusOK)

	// отчёт и журнал
	checked(t, v, c, http.MethodGet, cp("/reports/month"), nil, http.StatusOK)
	checked(t, v, c, http.MethodGet, cp("/audit?limit=5"), nil, http.StatusOK)

	// страница оплаты
	pays := payments.NewService(payments.Options{Pool: api.pool, AppOrigin: testOrigin, Now: api.clock.Now})
	link, err := pays.CreateLink(context.Background(), center, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	payToken := strings.TrimPrefix(link.URL, testOrigin+"/pay/")
	anon := api.client(t, "10.0.0.7")
	checked(t, v, anon, http.MethodGet, "/api/v1/pay/"+payToken, nil, http.StatusOK)
	checked(t, v, anon, http.MethodPost, "/api/v1/pay/"+payToken+"/start", nil, http.StatusOK)
	checked(t, v, anon, http.MethodGet, "/api/v1/pay/"+strings.Repeat("A", 43), nil, http.StatusNotFound)
}

// specAcceptsRequest — проходит ли запрос (JSON-тело, cookie и Origin клиента) валидацию по спецификации.
// Сам запрос не выполняется.
func specAcceptsRequest(t *testing.T, v validator.Validator, c *client, method, path string, body any) bool {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	ok, _ := v.ValidateHttpRequest(req)
	return ok
}

// todayIn — сегодня по Бишкеку для часов теста.
func todayIn(t *testing.T, api *testAPI) string {
	t.Helper()
	return api.clock.Now().In(bishkek).Format("2006-01-02")
}
