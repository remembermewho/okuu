package httpapi_test

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const centerPrefix = "/api/v1/c/{centerId}/"

// isolationCases — тело запроса для КАЖДОГО маршрута под /api/v1/c/{centerId}/.
// Добавили эндпоинт в роутер — добавьте строку сюда; иначе TestIsolationAllCenterRoutes упадёт.
// Тело должно быть валидным для своего центра: 404 обязан прийти из-за чужого центра или id,
// а не из-за ошибки в запросе.
var isolationCases = map[string]any{
	"GET /api/v1/c/{centerId}/staff":                       nil,
	"POST /api/v1/c/{centerId}/staff/invites":              map[string]any{"role": "teacher"},
	"GET /api/v1/c/{centerId}/staff/invites":               nil,
	"DELETE /api/v1/c/{centerId}/staff/invites/{inviteId}": nil,
	"PATCH /api/v1/c/{centerId}/staff/{userId}":            map[string]any{"role": "admin"},
	"DELETE /api/v1/c/{centerId}/staff/{userId}":           nil,

	"GET /api/v1/c/{centerId}/tariffs":               nil,
	"POST /api/v1/c/{centerId}/tariffs":              map[string]any{"name": "Взлом", "kind": "monthly", "price": 1},
	"PATCH /api/v1/c/{centerId}/tariffs/{tariffId}":  map[string]any{"price": 1},
	"DELETE /api/v1/c/{centerId}/tariffs/{tariffId}": nil,

	"GET /api/v1/c/{centerId}/groups":              nil,
	"POST /api/v1/c/{centerId}/groups":             map[string]any{"name": "Взлом"},
	"GET /api/v1/c/{centerId}/groups/{groupId}":    nil,
	"PATCH /api/v1/c/{centerId}/groups/{groupId}":  map[string]any{"name": "Взлом"},
	"DELETE /api/v1/c/{centerId}/groups/{groupId}": nil,

	"GET /api/v1/c/{centerId}/students":               nil,
	"POST /api/v1/c/{centerId}/students":              map[string]any{"full_name": "Взлом", "parent": map[string]string{"name": "x", "phone": "0700000001"}},
	"GET /api/v1/c/{centerId}/students/{studentId}":   nil,
	"PATCH /api/v1/c/{centerId}/students/{studentId}": map[string]any{"status": "left"},
	"POST /api/v1/c/{centerId}/students/{studentId}/enrollments": map[string]any{
		"group_id": "{groupId}", "start_date": "{today}", "first_month": "full",
	},
	"POST /api/v1/c/{centerId}/students/{studentId}/enrollments/{enrollmentId}/end":   nil,
	"POST /api/v1/c/{centerId}/students/{studentId}/enrollments/{enrollmentId}/renew": nil,

	// Деньги. Idempotency-Key клиент чужого центра шлёт всегда (см. тест).
	"POST /api/v1/c/{centerId}/students/{studentId}/payments": map[string]any{"kind": "payment", "amount": 100, "method": "cash"},
	"GET /api/v1/c/{centerId}/students/{studentId}/ledger":    nil,
	"POST /api/v1/c/{centerId}/ledger/{ledgerId}/cancel":      map[string]any{"reason": "тест изоляции"},
	"GET /api/v1/c/{centerId}/debtors":                        nil,
}

var routeParam = regexp.MustCompile(`\{(\w+)\}`)

// Сводный тест изоляции: каждый маршрут /api/v1/c/{centerId}/… для пользователя чужого центра — 404,
// и id сущностей центра А не работают через путь своего центра Б. Таблица сверяется с роутером.
func TestIsolationAllCenterRoutes(t *testing.T) {
	api := newTestAPI(t)
	routes := centerRoutes(t, api.h)

	var missing, stale []string
	for _, r := range routes {
		if _, ok := isolationCases[r]; !ok {
			missing = append(missing, r)
		}
	}
	for r := range isolationCases {
		if !slices.Contains(routes, r) {
			stale = append(stale, r)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("маршруты без теста изоляции — добавьте их в isolationCases (isolation_test.go):\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Fatalf("в isolationCases есть маршруты, которых нет в роутере — уберите или переименуйте:\n  %s",
			strings.Join(stale, "\n  "))
	}

	// Центр А со всеми видами сущностей.
	a := newCatalogOn(t, api, "+996703900001")
	_, teacherA := addStaff(t, api, a.owner, a.center, "teacher", "+996703900002")
	inviteA, _ := createInvite(t, a.owner, a.center, "admin", "")
	studentA := a.student(t, "Ученик А", "+996703900101", nil)
	enrA := a.enroll(t, studentA.ID, map[string]any{"group_id": a.group.ID, "tariff_id": a.pkg.ID, "start_date": a.today.String()})

	// Центр Б — владелец Б и есть «чужой пользователь».
	ownerB := api.client(t, "10.0.0.2")
	centerB := ownerB.register("+996703900003", "Центр Б").Centers[0].ID
	ownerB.headers = map[string]string{"Idempotency-Key": "isolation-test"}

	paramsA := map[string]string{
		"centerId": a.center.String(), "userId": teacherA.String(), "inviteId": inviteA.ID.String(),
		"tariffId": a.monthly.ID.String(), "groupId": a.group.ID.String(), "studentId": studentA.ID.String(),
		"enrollmentId": enrA.Enrollment.ID.String(), "ledgerId": enrA.Charge.ID.String(), "today": a.today.String(),
	}
	before := centerFingerprint(t, api, a.center, centerB)

	for _, route := range routes {
		method, pattern, _ := strings.Cut(route, " ")
		for _, m := range routeParam.FindAllStringSubmatch(pattern, -1) {
			if _, ok := paramsA[m[1]]; !ok {
				t.Fatalf("%s: нет значения для параметра {%s} — добавьте его в paramsA", route, m[1])
			}
		}
		body := fillBody(isolationCases[route], paramsA)

		t.Run("чужой центр "+route, func(t *testing.T) {
			assertError(t, ownerB.do(method, fill(pattern, paramsA), body), http.StatusNotFound, "not_found")
		})
		if len(routeParam.FindAllString(pattern, -1)) > 1 {
			viaB := map[string]string{}
			for k, v := range paramsA {
				viaB[k] = v
			}
			viaB["centerId"] = centerB.String()
			t.Run("id из А через Б "+route, func(t *testing.T) {
				assertError(t, ownerB.do(method, fill(pattern, viaB), body), http.StatusNotFound, "not_found")
			})
		}
	}

	if after := centerFingerprint(t, api, a.center, centerB); after != before {
		t.Errorf("данные изменились после запросов чужого центра:\n before %s\n after  %s", before, after)
	}
}

// centerRoutes — «МЕТОД шаблон» всех маршрутов роутера под /api/v1/c/{centerId}/.
func centerRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	routesOf, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("роутер %T не chi.Routes — тест изоляции не может обойти маршруты", h)
	}
	var out []string
	err := chi.Walk(routesOf, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, centerPrefix) {
			out = append(out, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	if len(out) == 0 {
		t.Fatal("не найдено ни одного маршрута под " + centerPrefix)
	}
	return out
}

func fill(s string, params map[string]string) string {
	return routeParam.ReplaceAllStringFunc(s, func(m string) string { return params[m[1:len(m)-1]] })
}

func fillBody(body any, params map[string]string) any {
	m, ok := body.(map[string]any)
	if !ok {
		return body
	}
	out := map[string]any{}
	for k, v := range m {
		if s, ok := v.(string); ok {
			v = fill(s, params)
		}
		out[k] = v
	}
	return out
}

// centerFingerprint — число строк центров во всех таблицах с данными; любое изменение его сдвинет.
func centerFingerprint(t *testing.T, api *testAPI, centers ...uuid.UUID) string {
	t.Helper()
	var fp string
	err := api.pool.QueryRow(context.Background(), `
		SELECT string_agg(x, ' ' ORDER BY x) FROM (
		  SELECT 'memberships:' || count(*) x FROM memberships WHERE center_id = ANY($1)
		  UNION ALL SELECT 'invites:' || count(*) || '/' || count(revoked_at) FROM invites WHERE center_id = ANY($1)
		  UNION ALL SELECT 'tariffs:' || count(*) || '/' || sum(price) || '/' || count(*) FILTER (WHERE archived) FROM tariffs WHERE center_id = ANY($1)
		  UNION ALL SELECT 'groups:' || count(*) || '/' || count(*) FILTER (WHERE archived) || '/' || string_agg(name, ',' ORDER BY name) FROM groups WHERE center_id = ANY($1)
		  UNION ALL SELECT 'students:' || count(*) || '/' || string_agg(status, ',' ORDER BY id) FROM students WHERE center_id = ANY($1)
		  UNION ALL SELECT 'parents:' || count(*) FROM parents WHERE center_id = ANY($1)
		  UNION ALL SELECT 'enrollments:' || count(*) || '/' || count(end_date) || '/' || coalesce(sum(lessons_left), 0) FROM enrollments WHERE center_id = ANY($1)
		  UNION ALL SELECT 'ledger:' || count(*) || '/' || coalesce(sum(amount), 0) FROM ledger WHERE center_id = ANY($1)
		  UNION ALL SELECT 'audit:' || count(*) FROM audit_log WHERE center_id = ANY($1)
		) s`, centers).Scan(&fp)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}
