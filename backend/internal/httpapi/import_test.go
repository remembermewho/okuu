package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"okuu/internal/civil"
	"okuu/internal/importer"
)

type importRowResp struct {
	Row         int         `json:"row"`
	Status      string      `json:"status"`
	Messages    []string    `json:"messages"`
	Skip        bool        `json:"skip"`
	FullName    string      `json:"full_name"`
	ParentPhone string      `json:"parent_phone"`
	Group       string      `json:"group"`
	GroupID     *uuid.UUID  `json:"group_id"`
	Tariff      string      `json:"tariff"`
	TariffKind  string      `json:"tariff_kind"`
	StartDate   *civil.Date `json:"start_date"`
	Debt        int64       `json:"debt"`
	LessonsLeft *int32      `json:"lessons_left"`
}

type previewResp struct {
	PreviewID uuid.UUID        `json:"preview_id"`
	ExpiresAt time.Time        `json:"expires_at"`
	Summary   importer.Summary `json:"summary"`
	Rows      []importRowResp  `json:"rows"`
}

// raw — запрос с произвольным телом (multipart) от имени клиента: cookie, Origin, заголовки.
func (c *client) raw(method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = c.ip + ":40000"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Origin", c.origin)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func (c *client) upload(path, filename string, data []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return c.raw(http.MethodPost, path, mw.FormDataContentType(), &buf)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "importer", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (c *catalog) preview(t *testing.T, filename string, data []byte) previewResp {
	t.Helper()
	rec := c.owner.upload(centerPath(c.center, "/import/preview"), filename, data)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview %s: %d %s", filename, rec.Code, rec.Body)
	}
	return decode[previewResp](t, rec)
}

func (c *catalog) commit(id uuid.UUID, firstMonth string) *httptest.ResponseRecorder {
	return c.owner.do(http.MethodPost, centerPath(c.center, "/import/commit"), map[string]any{"preview_id": id, "first_month": firstMonth})
}

func rowByNum(t *testing.T, p previewResp, n int) importRowResp {
	t.Helper()
	for _, r := range p.Rows {
		if r.Row == n {
			return r
		}
	}
	t.Fatalf("строки %d нет в превью", n)
	return importRowResp{}
}

func messagesContain(r importRowResp, sub string) bool {
	for _, m := range r.Messages {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func studentByName(t *testing.T, c *catalog, name string) studentResp {
	t.Helper()
	rec := c.owner.do(http.MethodGet, centerPath(c.center, "/students?q="+urlQuery(name)), nil)
	list := decode[studentListResp](t, rec)
	if len(list.Items) != 1 {
		t.Fatalf("ученик %q: найдено %d", name, len(list.Items))
	}
	return c.card(t, list.Items[0].ID)
}

func TestImportXLSX(t *testing.T) {
	c := newCatalog(t, "+996707000001")
	createTariff(t, c.owner, c.center, monthly("Английский, месяц", 300_000))

	p := c.preview(t, "Ученики центра.xlsx", fixture(t, "students.xlsx"))
	if s := p.Summary; s.Total != 7 || s.OK != 1 || s.Warning != 3 || s.Error != 3 || s.Skipped != 1 || s.ToImport != 3 {
		t.Fatalf("summary = %+v", s)
	}
	if !p.ExpiresAt.After(time.Now().Add(50 * time.Minute)) {
		t.Errorf("expires_at = %v, want ≈ +1 час", p.ExpiresAt)
	}
	r2 := rowByNum(t, p, 2)
	if r2.Status != "ok" || r2.ParentPhone != "+996700123456" || r2.GroupID == nil || *r2.StartDate != civil.New(2024, 9, 1) || r2.Debt != 150_000 {
		t.Errorf("строка 2 = %+v", r2)
	}
	r3 := rowByNum(t, p, 3)
	if r3.Status != "warning" || r3.ParentPhone != "+996700222333" || !messagesContain(r3, "без 0") ||
		!messagesContain(r3, "«Шахматы» не найдена — будет создана") || r3.Tariff != "Абонемент 8" || r3.GroupID != nil ||
		r3.TariffKind != "package" || r3.LessonsLeft == nil || *r3.LessonsLeft != 5 {
		t.Errorf("строка 3 = %+v", r3)
	}
	// Абонемент: остаток обязателен (строка 8) и не больше 200 (строка 9).
	if r8 := rowByNum(t, p, 8); r8.Status != "error" || !messagesContain(r8, "обязательно для абонемента") {
		t.Errorf("строка 8 = %+v", r8)
	}
	if r9 := rowByNum(t, p, 9); r9.Status != "error" || !messagesContain(r9, "Осталось занятий") {
		t.Errorf("строка 9 = %+v", r9)
	}
	r4 := rowByNum(t, p, 4)
	for _, want := range []string{"Имя родителя", "Телефон родителя", "«Несуществующий тариф» не найден", "Дата начала", "Текущий долг"} {
		if !messagesContain(r4, want) {
			t.Errorf("строка 4: нет сообщения про %q: %v", want, r4.Messages)
		}
	}
	if r4.Status != "error" {
		t.Errorf("строка 4 status = %s", r4.Status)
	}
	if r5 := rowByNum(t, p, 5); r5.Status != "warning" || !r5.Skip || !messagesContain(r5, "Повторяет строку 2") {
		t.Errorf("строка 5 = %+v", r5)
	}
	if r7 := rowByNum(t, p, 7); r7.Debt != 250_075 || *r7.StartDate != civil.New(2025, 3, 1) || r7.ParentPhone != "+996555444333" ||
		r7.LessonsLeft != nil || !messagesContain(r7, "для месячного тарифа") {
		t.Errorf("строка 7 = %+v", r7)
	}

	rec := c.commit(p.PreviewID, "none")
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body)
	}
	res := decode[importer.Result](t, rec)
	if res != (importer.Result{StudentsCreated: 3, ParentsCreated: 3, GroupsCreated: 1, EnrollmentsCreated: 3, Skipped: 1, DebtTotal: 400_075}) {
		t.Fatalf("result = %+v", res)
	}

	// Балансы: первый месяц none — начислений нет, только начальный долг; абонемент из Excel уже куплен —
	// не начисляется, остаток занятий — из файла.
	asanov := studentByName(t, c, "Асанов Бакыт")
	if asanov.Balance != -150_000 || len(asanov.Enrollments) != 1 || asanov.Enrollments[0].StartDate != civil.New(2024, 9, 1) ||
		asanov.Parent.Name != "Асанова Гульнара" {
		t.Errorf("Асанов = %+v", asanov)
	}
	bekova := studentByName(t, c, "Бекова Айпери")
	if e := bekova.Enrollments[0]; bekova.Balance != 0 || e.Tariff.Kind != "package" || *e.LessonsLeft != 5 || e.Group.Name != "Шахматы" {
		t.Errorf("Бекова = %+v", bekova)
	}
	if ops := ledgerOf(t, c.api, bekova.ID); len(ops) != 0 {
		t.Errorf("у импортированного абонемента есть операции: %+v", ops)
	}
	davletov := studentByName(t, c, "Давлетов Эрлан")
	if davletov.Balance != -250_075 || davletov.Enrollments[0].Group.ID != bekova.Enrollments[0].Group.ID {
		t.Errorf("Давлетов = %+v (группа «Шахматы» должна быть одна)", davletov)
	}
	if gs := decodeItems[groupResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/groups"), nil)); len(gs) != 2 {
		t.Errorf("групп %d, want 2 (Английский A1 + Шахматы)", len(gs))
	}

	// Начальный долг — в должниках: сегодня срок наступает (pending), завтра — просрочка (R14).
	d := decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors"), nil))
	if len(d.Items) != 0 || d.Summary.PendingCount != 2 || d.Summary.PendingTotal != 150_000+250_075 {
		t.Errorf("сегодня: items=%d pending=%d/%d", len(d.Items), d.Summary.PendingCount, d.Summary.PendingTotal)
	}
	c.api.clock.Advance(24 * time.Hour)
	d = decode[debtorsResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/debtors"), nil))
	if len(d.Items) != 2 || d.Items[0].FullName != "Давлетов Эрлан" || d.Items[0].Debt != 250_075 || d.Items[0].DaysOverdue != 1 ||
		d.Items[1].FullName != "Асанов Бакыт" {
		t.Errorf("завтра должники = %+v", d.Items)
	}

	log := auditLog(t, c.api, c.center)
	imp := filterAudit(log, "import.commit")
	if len(imp) != 1 || imp[0].After["students_created"] != float64(3) || imp[0].After["debt_total"] != float64(400_075) || *imp[0].EntityID != p.PreviewID {
		t.Errorf("import.commit = %+v", imp)
	}
	corr := filterAudit(log, "ledger.correction")
	if len(corr) != 2 || corr[0].After["comment"] != importer.InitialDebtComment {
		t.Errorf("ledger.correction = %+v", corr)
	}

	assertError(t, c.commit(p.PreviewID, "none"), http.StatusConflict, "import_already_committed")
}

// Три CSV с одними данными: UTF-8 с BOM и запятыми, UTF-8 с ;, windows-1251 с ;.
func TestImportCSV(t *testing.T) {
	var firstRows []importRowResp
	for i, name := range []string{"utf8_bom_comma.csv", "utf8_semicolon.csv", "cp1251_semicolon.csv"} {
		t.Run(name, func(t *testing.T) {
			c := newCatalog(t, "+99670710000"+string(rune('1'+i)))
			createTariff(t, c.owner, c.center, monthly("Английский, месяц", 300_000))

			p := c.preview(t, name, fixture(t, name))
			if s := p.Summary; s.Total != 4 || s.OK != 2 || s.Warning != 1 || s.Error != 1 || s.ToImport != 3 {
				t.Fatalf("summary = %+v; rows %+v", s, p.Rows)
			}
			if r := rowByNum(t, p, 2); r.FullName != "Касымов Тимур" || r.Debt != 150_050 || *r.StartDate != civil.New(2025, 9, 1) {
				t.Errorf("строка 2 = %+v", r)
			}
			if r := rowByNum(t, p, 3); r.FullName != "Ёлкина Алёна" || !messagesContain(r, "«Робототехника»") {
				t.Errorf("строка 3 = %+v", r)
			}
			if r := rowByNum(t, p, 4); r.Status != "ok" || r.GroupID == nil || *r.GroupID != c.group.ID || r.Tariff != "Английский, месяц" || r.StartDate != nil {
				t.Errorf("строка 4 (регистр группы и тарифа) = %+v", r)
			}
			if firstRows == nil {
				firstRows = p.Rows
			} else {
				for i := range p.Rows {
					a, b := p.Rows[i], firstRows[i]
					a.GroupID, b.GroupID = nil, nil // id групп в каждом центре свои
					if a.Status != b.Status || a.FullName != b.FullName || a.ParentPhone != b.ParentPhone || a.Debt != b.Debt ||
						len(a.Messages) != len(b.Messages) {
						t.Errorf("строка %d отличается от первого файла: %+v / %+v", a.Row, a, b)
					}
				}
			}

			rec := c.commit(p.PreviewID, "full")
			if rec.Code != http.StatusOK {
				t.Fatalf("commit: %d %s", rec.Code, rec.Body)
			}
			if res := decode[importer.Result](t, rec); res.StudentsCreated != 3 || res.GroupsCreated != 1 || res.DebtTotal != 180_050 {
				t.Errorf("result = %+v", res)
			}
			// first_month=full: месячный тариф — начисление за текущий месяц (R5) + начальный долг.
			if s := studentByName(t, c, "Нурланова Дана"); s.Balance != -330_000 ||
				s.Enrollments[0].StartDate != c.today.FirstOfMonth() {
				t.Errorf("Нурланова = %+v (want -330000, начало с 1-го числа)", s)
			}
			if s := studentByName(t, c, "Касымов Тимур"); s.Balance != -450_050 {
				t.Errorf("Касымов: balance = %d, want -450050", s.Balance)
			}
			// Абонемент: first_month=full на него не влияет — без начисления, остаток из файла.
			if s := studentByName(t, c, "Ёлкина Алёна"); s.Balance != 0 || *s.Enrollments[0].LessonsLeft != 3 {
				t.Errorf("Ёлкина: balance=%d lessons_left=%v, want 0 и 3", s.Balance, *s.Enrollments[0].LessonsLeft)
			}
		})
	}
}

func TestImportCommitErrorsAndLimits(t *testing.T) {
	c := newCatalog(t, "+996707200001")
	createTariff(t, c.owner, c.center, monthly("Английский, месяц", 300_000))
	ctx := context.Background()

	// Дубликат появился между превью и подтверждением — пропускается при commit.
	p := c.preview(t, "a.csv", fixture(t, "utf8_semicolon.csv"))
	c.owner.do(http.MethodPost, centerPath(c.center, "/students"), map[string]any{
		"full_name": "Касымов Тимур", "parent": map[string]string{"name": "Касымова Айгерим", "phone": "0555111222"}})
	rec := c.commit(p.PreviewID, "none")
	if res := decode[importer.Result](t, rec); rec.Code != 200 || res.StudentsCreated != 2 || res.Skipped != 1 {
		t.Errorf("commit с новым дубликатом: %d %+v", rec.Code, res)
	}

	// Превью старше часа — 410.
	p2 := c.preview(t, "b.csv", fixture(t, "utf8_semicolon.csv"))
	if _, err := c.api.pool.Exec(ctx, `UPDATE import_sessions SET expires_at = now() - interval '1 second' WHERE id = $1`, p2.PreviewID); err != nil {
		t.Fatal(err)
	}
	assertError(t, c.commit(p2.PreviewID, "none"), http.StatusGone, "import_expired")

	assertError(t, c.commit(uuid.New(), "none"), http.StatusNotFound, "not_found")
	other := newCatalogOn(t, c.api, "+996707200002")
	foreign := other.preview(t, "c.csv", fixture(t, "utf8_semicolon.csv"))
	assertError(t, c.commit(foreign.PreviewID, "none"), http.StatusNotFound, "not_found") // превью чужого центра
	assertField(t, c.commit(p2.PreviewID, "manual"), http.StatusBadRequest, "first_month")
	assertField(t, c.owner.do(http.MethodPost, centerPath(c.center, "/import/commit"), map[string]any{"first_month": "none"}),
		http.StatusBadRequest, "preview_id")

	// Лимиты и формат.
	path := centerPath(c.center, "/import/preview")
	// Имя файла не в UTF-8 (curl из консоли Windows шлёт «ученики.csv» в cp1251) — не 500.
	if rec := c.owner.upload(path, "\xf3\xf7\xe5\xed\xe8\xea\xe8.csv", fixture(t, "utf8_semicolon.csv")); rec.Code != http.StatusOK {
		t.Errorf("имя файла в cp1251: %d %s", rec.Code, rec.Body)
	}
	big := bytes.Repeat([]byte("x"), importer.MaxFileBytes+1)
	assertError(t, c.owner.upload(path, "big.csv", big), http.StatusRequestEntityTooLarge, "file_too_large")
	var many strings.Builder
	many.WriteString("ФИО;Родитель;Телефон;Группа;Тариф\n")
	for i := range importer.MaxRows + 1 {
		many.WriteString("Ученик " + string(rune('А'+i%30)) + ";Р;0700000000;Г;Т\n")
	}
	assertField(t, c.owner.upload(path, "many.csv", []byte(many.String())), http.StatusBadRequest, "file")
	assertField(t, c.owner.upload(path, "nohead.csv", []byte("Имя;Фамилия\nА;Б\n")), http.StatusBadRequest, "file")
	assertField(t, c.owner.raw(http.MethodPost, path, "application/json", strings.NewReader(`{}`)), http.StatusBadRequest, "file")

	// Преподаватель — 403 на всё.
	teacher, _ := addStaff(t, c.api, c.owner, c.center, "teacher", "+996707200003")
	assertError(t, teacher.upload(path, "a.csv", fixture(t, "utf8_semicolon.csv")), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodGet, centerPath(c.center, "/import/template.xlsx"), nil), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodPost, centerPath(c.center, "/import/commit"), map[string]any{"preview_id": p2.PreviewID, "first_month": "none"}),
		http.StatusForbidden, "forbidden")
}

// Тариф абонемента между превью и подтверждением стал месячным — остаток занятий к нему не применяется:
// подтверждение откатывается целиком с номером строки.
func TestImportPackageTariffChangedBeforeCommit(t *testing.T) {
	c := newCatalog(t, "+996707400001")
	createTariff(t, c.owner, c.center, monthly("Английский, месяц", 300_000))
	p := c.preview(t, "a.csv", fixture(t, "utf8_semicolon.csv"))
	if r := rowByNum(t, p, 3); r.TariffKind != "package" || r.LessonsLeft == nil {
		t.Fatalf("строка 3 = %+v", r)
	}
	if rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/tariffs/"+c.pkg.ID.String()),
		map[string]any{"kind": "monthly", "lessons_count": nil}); rec.Code != http.StatusOK {
		t.Fatalf("смена вида тарифа: %d %s", rec.Code, rec.Body)
	}
	rec := c.commit(p.PreviewID, "none")
	assertField(t, rec, http.StatusUnprocessableEntity, "rows")
	if !strings.Contains(rec.Body.String(), "Строка 3") {
		t.Errorf("в ошибке нет номера строки: %s", rec.Body)
	}
	if list := decode[studentListResp](t, c.owner.do(http.MethodGet, centerPath(c.center, "/students"), nil)); len(list.Items) != 0 {
		t.Errorf("после отката создано учеников: %d", len(list.Items))
	}
}

// Шаблон открывается в Excel, содержит заголовки и пример, а скачанный шаблон сразу проходит превью.
func TestImportTemplate(t *testing.T) {
	c := newCatalog(t, "+996707300001")
	rec := c.owner.do(http.MethodGet, centerPath(c.center, "/import/template.xlsx"), nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "okuu-import.xlsx") {
		t.Fatalf("template: %d %v", rec.Code, rec.Header())
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("шаблон не открывается: %v", err)
	}
	defer f.Close()
	rows, _ := f.GetRows("Ученики")
	if len(rows) != 2 || strings.Join(rows[0], "|") != strings.Join(importer.TemplateHeaders, "|") || rows[1][0] != "Асанов Бакыт" {
		t.Fatalf("лист «Ученики» = %v", rows)
	}
	help, _ := f.GetRows("Справка")
	var names []string
	for _, r := range help[1:] {
		names = append(names, r[0])
	}
	if !strings.Contains(strings.Join(names, "|"), "Абонемент 8") {
		t.Errorf("в справке нет тарифов центра: %v", help)
	}

	p := c.preview(t, "okuu-import.xlsx", rec.Body.Bytes())
	if p.Summary.Total != 1 || p.Summary.OK != 1 {
		t.Errorf("пример из шаблона: %+v %+v", p.Summary, p.Rows)
	}
}

func urlQuery(s string) string {
	return strings.NewReplacer(" ", "%20").Replace(s)
}
