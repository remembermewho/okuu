package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"okuu/internal/civil"
)

type lessonStudentResp struct {
	StudentID   uuid.UUID `json:"student_id"`
	FullName    string    `json:"full_name"`
	Status      *string   `json:"status"`
	LessonsLeft *int32    `json:"lessons_left"`
	HasDebt     *bool     `json:"has_debt"`
}

type lessonResp struct {
	ID            uuid.UUID           `json:"id"`
	Group         refJSON             `json:"group"`
	Date          civil.Date          `json:"date"`
	StartTime     string              `json:"start_time"`
	EndTime       *string             `json:"end_time"`
	Cancelled     bool                `json:"cancelled"`
	StudentsCount int                 `json:"students_count"`
	MarkedCount   int                 `json:"marked_count"`
	Students      []lessonStudentResp `json:"students"`
}

// «Сегодня» центра — в его часовом поясе (не по UTC и не по часам браузера): в /me и в /lessons.
// По нему интерфейс строит окно дат посещаемости.
func TestTodayInMeAndLessons(t *testing.T) {
	c := newCatalog(t, "+996707300001")
	// 18:30 UTC 20.10: в Бишкеке (UTC+6) уже 00:30 21.10, в Ташкенте (UTC+5) ещё 23:30 20.10.
	c.api.clock.Set(time.Date(2026, 10, 20, 18, 30, 0, 0, time.UTC))
	check := func(want string) {
		t.Helper()
		me := decode[meResp](t, c.owner.do(http.MethodGet, "/api/v1/me", nil))
		if len(me.Centers) != 1 || me.Centers[0].Today != want {
			t.Errorf("/me centers = %+v, want today %s", me.Centers, want)
		}
		rec := c.owner.do(http.MethodGet, centerPath(c.center, "/lessons?date="+want), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("lessons: %d %s", rec.Code, rec.Body)
		}
		if got := decode[struct {
			Today string `json:"today"`
		}](t, rec).Today; got != want {
			t.Errorf("/lessons today = %s, want %s", got, want)
		}
	}
	check("2026-10-21")

	rec := c.owner.do(http.MethodPatch, centerPath(c.center, "/settings"), map[string]any{"timezone": "Asia/Tashkent"})
	if rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	check("2026-10-20")
}

func createLesson(t *testing.T, c *catalog, body map[string]any) lessonResp {
	t.Helper()
	rec := c.owner.do(http.MethodPost, centerPath(c.center, "/lessons"), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create lesson %v: %d %s", body, rec.Code, rec.Body)
	}
	return decode[lessonResp](t, rec)
}

func lessonsOn(t *testing.T, c *catalog, as *client, date civil.Date) []lessonResp {
	t.Helper()
	rec := as.do(http.MethodGet, centerPath(c.center, "/lessons?date="+date.String()), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lessons %s: %d %s", date, rec.Code, rec.Body)
	}
	return decodeItems[lessonResp](t, rec)
}

// mark — PUT /lessons/{id}/attendance; пары student_id → статус ("" — null, снять отметку).
func mark(c *catalog, as *client, lessonID uuid.UUID, pairs ...any) *httptest.ResponseRecorder {
	var marks []map[string]any
	for i := 0; i+1 < len(pairs); i += 2 {
		var status any
		if s := pairs[i+1].(string); s != "" {
			status = s
		}
		marks = append(marks, map[string]any{"student_id": pairs[i].(uuid.UUID), "status": status})
	}
	return as.do(http.MethodPut, centerPath(c.center, "/lessons/"+lessonID.String()+"/attendance"), map[string]any{"marks": marks})
}

func mustMark(t *testing.T, c *catalog, as *client, lessonID uuid.UUID, pairs ...any) lessonResp {
	t.Helper()
	rec := mark(c, as, lessonID, pairs...)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark %v: %d %s", pairs, rec.Code, rec.Body)
	}
	return decode[lessonResp](t, rec)
}

func studentOn(t *testing.T, l lessonResp, sid uuid.UUID) lessonStudentResp {
	t.Helper()
	for _, s := range l.Students {
		if s.StudentID == sid {
			return s
		}
	}
	t.Fatalf("ученика %s нет на занятии %s %s", sid, l.Date, l.StartTime)
	return lessonStudentResp{}
}

func hasStudent(l lessonResp, sid uuid.UUID) bool {
	for _, s := range l.Students {
		if s.StudentID == sid {
			return true
		}
	}
	return false
}

func lessonsLeftOf(t *testing.T, c *catalog, sid uuid.UUID) int32 {
	t.Helper()
	var n *int32
	if err := c.api.pool.QueryRow(context.Background(),
		`SELECT lessons_left FROM enrollments WHERE student_id = $1 ORDER BY start_date DESC LIMIT 1`, sid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == nil {
		t.Fatalf("lessons_left = NULL у %s", sid)
	}
	return *n
}

// packageStudent — ученик на абонементе в группе c.group (8 занятий, начало сегодня).
func packageStudent(t *testing.T, c *catalog, name, phone string) uuid.UUID {
	t.Helper()
	s := c.student(t, name, phone, nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": c.today.String()})
	return s.ID
}

// R11: занятия создаются при первом открытии даты по расписанию активных групп, идемпотентно.
func TestLessonsFromSchedule(t *testing.T) {
	c := newCatalog(t, "+996706000001")
	d, d2 := c.today.ISOWeekday(), c.today.AddDays(1).ISOWeekday()
	g := createGroup(t, c.owner, c.center, map[string]any{"name": "По расписанию", "default_tariff_id": c.monthly.ID, "schedule": []slotJSON{
		{d, "16:00", "17:30"}, {d, "18:00", "19:00"}, {d2, "10:00", "11:00"},
	}})
	archived := createGroup(t, c.owner, c.center, map[string]any{"name": "Архивная", "schedule": []slotJSON{{d, "09:00", "10:00"}}})
	c.owner.do(http.MethodDelete, centerPath(c.center, "/groups/"+archived.ID.String()), nil)
	createGroup(t, c.owner, c.center, map[string]any{"name": "Без расписания"})

	enroll := func(name, phone string, start civil.Date) uuid.UUID {
		s := c.student(t, name, phone, nil)
		c.enroll(t, s.ID, map[string]any{"group_id": g.ID, "start_date": start.String(), "first_month": "none"})
		return s.ID
	}
	now := enroll("Учится", "+996706000101", c.today)
	fromTomorrow := enroll("С завтра", "+996706000102", c.today.AddDays(1))
	endsToday := enroll("Последний день", "+996706000103", c.today)
	var eid uuid.UUID
	_ = c.api.pool.QueryRow(context.Background(), `SELECT id FROM enrollments WHERE student_id = $1`, endsToday).Scan(&eid)
	if rec := c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+endsToday.String()+"/enrollments/"+eid.String()+"/end"), nil); rec.Code != 200 {
		t.Fatalf("end: %d %s", rec.Code, rec.Body)
	}

	today := lessonsOn(t, c, c.owner, c.today)
	if len(today) != 2 || today[0].StartTime != "16:00" || today[0].EndTime == nil || *today[0].EndTime != "17:30" ||
		today[1].StartTime != "18:00" || today[0].Group.ID != g.ID || today[0].Cancelled {
		t.Fatalf("занятия сегодня = %+v", today)
	}
	l := today[0]
	if !hasStudent(l, now) || !hasStudent(l, endsToday) || hasStudent(l, fromTomorrow) || l.StudentsCount != 2 || l.MarkedCount != 0 {
		t.Errorf("ученики сегодня: %+v", l.Students)
	}
	if s := studentOn(t, l, now); s.Status != nil || s.LessonsLeft != nil || s.HasDebt == nil {
		t.Errorf("ученик на месячном тарифе: %+v", s)
	}

	tomorrow := lessonsOn(t, c, c.owner, c.today.AddDays(1))
	if len(tomorrow) != 1 || tomorrow[0].StartTime != "10:00" ||
		!hasStudent(tomorrow[0], now) || !hasStudent(tomorrow[0], fromTomorrow) || hasStudent(tomorrow[0], endsToday) {
		t.Fatalf("занятия завтра = %+v", tomorrow)
	}

	// Повторное открытие даты — те же занятия, новых строк нет.
	again := lessonsOn(t, c, c.owner, c.today)
	if len(again) != 2 || again[0].ID != today[0].ID || again[1].ID != today[1].ID {
		t.Errorf("повторно: %+v", again)
	}
	var n int
	_ = c.api.pool.QueryRow(context.Background(), `SELECT count(*) FROM lessons WHERE center_id = $1`, c.center).Scan(&n)
	if n != 3 {
		t.Errorf("строк lessons %d, want 3 (2 сегодня + 1 завтра)", n)
	}

	// Окно дат владельца — до сегодня + 30; формат даты строгий.
	lessonsOn(t, c, c.owner, c.today.AddDays(30))
	assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/lessons?date="+c.today.AddDays(31).String()), nil), http.StatusBadRequest, "date")
	assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/lessons?date=20.10.2026"), nil), http.StatusBadRequest, "date")
	assertField(t, c.owner.do(http.MethodGet, centerPath(c.center, "/lessons"), nil), http.StatusBadRequest, "date")
}

// Внеплановое занятие; отмена занятия удаляет отметки и возвращает списанные занятия (R11).
func TestUnscheduledLessonAndCancel(t *testing.T) {
	c := newCatalog(t, "+996706100001")
	pkg := packageStudent(t, c, "Абонемент", "+996706100101")
	m := c.student(t, "Месячный", "+996706100102", nil)
	c.enroll(t, m.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.String(), "first_month": "none"})

	l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "12:00"})
	if l.StartTime != "12:00" || l.EndTime != nil || l.StudentsCount != 2 {
		t.Fatalf("внеплановое = %+v", l)
	}
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/lessons"),
		map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "12:00"}), http.StatusConflict, "lesson_exists")
	other := newCatalog(t, "+996706100002")
	assertField(t, c.owner.do(http.MethodPost, centerPath(c.center, "/lessons"),
		map[string]any{"group_id": other.group.ID, "date": c.today.String(), "start_time": "12:00"}), http.StatusUnprocessableEntity, "group_id")
	assertField(t, c.owner.do(http.MethodPost, centerPath(c.center, "/lessons"),
		map[string]any{"group_id": c.group.ID, "date": c.today.AddDays(31).String(), "start_time": "12:00"}), http.StatusBadRequest, "date")
	assertError(t, c.owner.do(http.MethodPost, centerPath(c.center, "/lessons"),
		map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "12:60"}), http.StatusBadRequest, "bad_request")

	mustMark(t, c, c.owner, l.ID, pkg, "present", m.ID, "absent")
	if n := lessonsLeftOf(t, c, pkg); n != 7 {
		t.Fatalf("после отметки lessons_left = %d, want 7", n)
	}

	path := centerPath(c.center, "/lessons/"+l.ID.String())
	assertField(t, c.owner.do(http.MethodPatch, path, map[string]any{}), http.StatusBadRequest, "cancelled")
	rec := c.owner.do(http.MethodPatch, path, map[string]any{"cancelled": true})
	got := decode[lessonResp](t, rec)
	if rec.Code != http.StatusOK || !got.Cancelled || got.MarkedCount != 0 {
		t.Fatalf("cancel: %d %+v", rec.Code, got)
	}
	if n := lessonsLeftOf(t, c, pkg); n != 8 {
		t.Errorf("после отмены занятия lessons_left = %d, want 8 (занятие вернулось)", n)
	}
	assertError(t, mark(c, c.owner, l.ID, pkg, "present"), http.StatusConflict, "lesson_cancelled")

	// Вернуть занятие — отметок нет, можно отмечать снова.
	rec = c.owner.do(http.MethodPatch, path, map[string]any{"cancelled": false})
	if got := decode[lessonResp](t, rec); rec.Code != http.StatusOK || got.Cancelled || got.MarkedCount != 0 {
		t.Fatalf("restore: %d %+v", rec.Code, got)
	}
	mustMark(t, c, c.owner, l.ID, pkg, "present")

	cancels := filterAudit(auditLog(t, c.api, c.center), "lesson.")
	if got := strings.Join(auditActions(cancels), ","); got != "lesson.create,lesson.cancel,lesson.restore" {
		t.Errorf("audit = %s", got)
	}
	if a := cancels[1]; a.After["returned_lessons"] != float64(1) || a.Before["marks"].(map[string]any)[pkg.String()] != "present" {
		t.Errorf("lesson.cancel = %+v", a)
	}
}

// R9: present и absent списывают занятие абонемента, excused — по настройке центра;
// смена отметки пересчитывает остаток по разнице deducted.
func TestMarkTransitionsR9(t *testing.T) {
	for _, excusedDeducts := range []bool{false, true} {
		name := "excused не списывает"
		if excusedDeducts {
			name = "excused списывает"
		}
		t.Run(name, func(t *testing.T) {
			c := newCatalog(t, "+996706200001")
			if _, err := c.api.pool.Exec(context.Background(), `UPDATE centers SET excused_deducts = $2 WHERE id = $1`, c.center, excusedDeducts); err != nil {
				t.Fatal(err)
			}
			s := packageStudent(t, c, "Абонемент", "+996706200101")
			l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "15:00"})

			steps := []struct {
				status string
				want   int32 // lessons_left после шага
			}{
				{"present", 7},
				{"excused", map[bool]int32{false: 8, true: 7}[excusedDeducts]},
				{"absent", 7},
				{"absent", 7}, // та же отметка — без изменений
				{"", 8},       // снять отметку — занятие вернулось
			}
			for _, st := range steps {
				got := mustMark(t, c, c.owner, l.ID, s, st.status)
				if n := lessonsLeftOf(t, c, s); n != st.want {
					t.Fatalf("после %q lessons_left = %d, want %d", st.status, n, st.want)
				}
				if ll := studentOn(t, got, s).LessonsLeft; ll == nil || *ll != st.want {
					t.Errorf("в ответе lessons_left = %v, want %d", ll, st.want)
				}
			}
			marks := filterAudit(auditLog(t, c.api, c.center), "attendance.mark")
			if len(marks) != 4 || marks[0].Before[s.String()] != nil || marks[0].After[s.String()] != "present" ||
				marks[1].Before[s.String()] != "present" || marks[3].After[s.String()] != nil {
				t.Errorf("attendance.mark = %+v", marks)
			}
		})
	}
}

// deducted фиксируется в момент отметки: смена настройки центра не пересчитывает историю.
func TestDeductedSurvivesSettingChange(t *testing.T) {
	c := newCatalog(t, "+996706300001")
	ctx := context.Background()
	setExcused := func(v bool) {
		if _, err := c.api.pool.Exec(ctx, `UPDATE centers SET excused_deducts = $2 WHERE id = $1`, c.center, v); err != nil {
			t.Fatal(err)
		}
	}
	s := packageStudent(t, c, "Абонемент", "+996706300101")
	l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "15:00"})

	setExcused(true)
	mustMark(t, c, c.owner, l.ID, s, "excused")
	if n := lessonsLeftOf(t, c, s); n != 7 {
		t.Fatalf("excused при excused_deducts=true: %d, want 7", n)
	}
	setExcused(false)
	mustMark(t, c, c.owner, l.ID, s, "excused") // та же отметка — история не пересчитывается
	if n := lessonsLeftOf(t, c, s); n != 7 {
		t.Fatalf("после смены настройки: %d, want 7", n)
	}
	mustMark(t, c, c.owner, l.ID, s, "present") // было списано, и present списывает — разница 0
	if n := lessonsLeftOf(t, c, s); n != 7 {
		t.Fatalf("excused(списано) → present: %d, want 7", n)
	}
	mustMark(t, c, c.owner, l.ID, s, "excused") // теперь excused не списывает — вернуть 1
	if n := lessonsLeftOf(t, c, s); n != 8 {
		t.Fatalf("present → excused при excused_deducts=false: %d, want 8", n)
	}
}

// R10: остаток абонемента уходит в минус; месячный тариф не трогаем.
func TestPackageGoesNegativeR10(t *testing.T) {
	c := newCatalog(t, "+996706400001")
	one := createTariff(t, c.owner, c.center, map[string]any{"name": "Разовое", "kind": "package", "price": 50_000, "lessons_count": 1})
	s := c.student(t, "Одно занятие", "+996706400101", nil)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": one.ID, "start_date": c.today.AddDays(-1).String()})
	m := c.student(t, "Месячный", "+996706400102", nil)
	c.enroll(t, m.ID, map[string]any{"group_id": c.group.ID, "start_date": c.today.AddDays(-1).String(), "first_month": "none"})

	l1 := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.AddDays(-1).String(), "start_time": "15:00"})
	l2 := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "15:00"})
	mustMark(t, c, c.owner, l1.ID, s.ID, "present", m.ID, "present")
	got := mustMark(t, c, c.owner, l2.ID, s.ID, "absent", m.ID, "present")
	if n := lessonsLeftOf(t, c, s.ID); n != -1 {
		t.Errorf("lessons_left = %d, want -1 (R10: отметить можно, остаток в минус)", n)
	}
	if st := studentOn(t, got, m.ID); st.LessonsLeft != nil {
		t.Errorf("месячный: lessons_left = %v, want null", *st.LessonsLeft)
	}

	// Продление: минус вычитается из нового абонемента (R10).
	var eid uuid.UUID
	_ = c.api.pool.QueryRow(context.Background(), `SELECT id FROM enrollments WHERE student_id = $1`, s.ID).Scan(&eid)
	c.owner.do(http.MethodPost, centerPath(c.center, "/students/"+s.ID.String()+"/enrollments/"+eid.String()+"/renew"), nil)
	if n := lessonsLeftOf(t, c, s.ID); n != 0 {
		t.Errorf("после продления lessons_left = %d, want 0 (-1 + 1)", n)
	}
}

// R12 и роли: преподаватель — только свои группы, отметки за последние 7 дней, без денег.
func TestTeacherAttendanceRules(t *testing.T) {
	c := newCatalog(t, "+996706500001")
	teacher, teacherID := addStaff(t, c.api, c.owner, c.center, "teacher", "+996706500002")
	own := createGroup(t, c.owner, c.center, map[string]any{"name": "Своя", "teacher_id": teacherID, "default_tariff_id": c.monthly.ID})
	foreign := createGroup(t, c.owner, c.center, map[string]any{"name": "Чужая", "default_tariff_id": c.monthly.ID})

	debtor := c.student(t, "Должник", "+996706500101", nil) // начисление за месяц — баланс < 0
	c.enroll(t, debtor.ID, map[string]any{"group_id": own.ID, "start_date": c.today.AddDays(-8).String(), "first_month": "full"})
	c.enroll(t, debtor.ID, map[string]any{"group_id": foreign.ID, "start_date": c.today.String(), "first_month": "none"})
	outsider := c.student(t, "Не из группы", "+996706500102", nil)

	at := func(g groupResp, days int) lessonResp {
		return createLesson(t, c, map[string]any{"group_id": g.ID, "date": c.today.AddDays(days).String(), "start_time": "14:00"})
	}
	ownToday, own7, own8, ownTomorrow := at(own, 0), at(own, -7), at(own, -8), at(own, 1)
	foreignToday := at(foreign, 0)

	// Список: только свои группы; денег нет.
	rec := teacher.do(http.MethodGet, centerPath(c.center, "/lessons?date="+c.today.String()), nil)
	if strings.Contains(rec.Body.String(), "has_debt") || strings.Contains(rec.Body.String(), "balance") {
		t.Errorf("преподаватель видит деньги: %s", rec.Body)
	}
	list := decodeItems[lessonResp](t, rec)
	if len(list) != 1 || list[0].ID != ownToday.ID {
		t.Fatalf("преподаватель видит %+v, want только своё занятие", list)
	}
	if ownerView := lessonsOn(t, c, c.owner, c.today); len(ownerView) != 2 {
		t.Errorf("владелец видит %d занятий, want 2", len(ownerView))
	} else if st := studentOn(t, ownerView[0], debtor.ID); st.HasDebt == nil || !*st.HasDebt {
		t.Errorf("владелец: has_debt = %v, want true", st.HasDebt)
	}

	// Окно дат преподавателя: сегодня−7 … сегодня.
	lessonsOn(t, c, teacher, c.today.AddDays(-7))
	for _, days := range []int{-8, 1} {
		assertField(t, teacher.do(http.MethodGet, centerPath(c.center, "/lessons?date="+c.today.AddDays(days).String()), nil),
			http.StatusBadRequest, "date")
	}

	// Отметки: своё сегодня и 7 дней назад — да; 8 дней — 403 (R12); владелец — без ограничений.
	mustMark(t, c, teacher, ownToday.ID, debtor.ID, "present")
	mustMark(t, c, teacher, own7.ID, debtor.ID, "absent")
	assertError(t, mark(c, teacher, own8.ID, debtor.ID, "present"), http.StatusForbidden, "forbidden")
	mustMark(t, c, c.owner, own8.ID, debtor.ID, "present")

	// Чужая группа — как будто её нет.
	assertError(t, mark(c, teacher, foreignToday.ID, debtor.ID, "present"), http.StatusNotFound, "not_found")
	assertError(t, teacher.do(http.MethodGet, centerPath(c.center, "/lessons/"+foreignToday.ID.String()+"/attendance"), nil),
		http.StatusNotFound, "not_found")
	if rec := teacher.do(http.MethodGet, centerPath(c.center, "/lessons/"+ownToday.ID.String()+"/attendance"), nil); rec.Code != 200 ||
		strings.Contains(rec.Body.String(), "has_debt") {
		t.Errorf("своё занятие: %d %s", rec.Code, rec.Body)
	}

	// Ученик не из группы — 422 с его id; будущее занятие не отмечается.
	rec = mark(c, c.owner, ownToday.ID, outsider.ID, "present")
	assertField(t, rec, http.StatusUnprocessableEntity, "student_id")
	if !strings.Contains(rec.Body.String(), outsider.ID.String()) {
		t.Errorf("в ошибке нет student_id: %s", rec.Body)
	}
	assertField(t, mark(c, c.owner, ownTomorrow.ID, debtor.ID, "present"), http.StatusUnprocessableEntity, "date")

	// Валидация пакета отметок.
	put := func(body any) *httptest.ResponseRecorder {
		return c.owner.do(http.MethodPut, centerPath(c.center, "/lessons/"+ownToday.ID.String()+"/attendance"), body)
	}
	assertField(t, put(map[string]any{"marks": []any{}}), http.StatusBadRequest, "marks")
	assertField(t, put(map[string]any{"marks": []map[string]any{{"student_id": debtor.ID, "status": "late"}}}), http.StatusBadRequest, "status")
	assertField(t, put(map[string]any{"marks": []map[string]any{{"student_id": debtor.ID, "status": "present"}, {"student_id": debtor.ID, "status": "absent"}}}),
		http.StatusBadRequest, "student_id")

	// Управление занятиями и история ученика — только owner/admin.
	assertError(t, teacher.do(http.MethodPost, centerPath(c.center, "/lessons"),
		map[string]any{"group_id": own.ID, "date": c.today.String(), "start_time": "20:00"}), http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodPatch, centerPath(c.center, "/lessons/"+ownToday.ID.String()), map[string]any{"cancelled": true}),
		http.StatusForbidden, "forbidden")
	assertError(t, teacher.do(http.MethodGet, centerPath(c.center, "/students/"+debtor.ID.String()+"/attendance"), nil),
		http.StatusForbidden, "forbidden")
}

// Одновременные сохранения одной отметки (два телефона, двойной тап) — занятие списывается один раз.
func TestMarkConcurrent(t *testing.T) {
	c := newCatalog(t, "+996706700001")
	a := packageStudent(t, c, "Первый", "+996706700101")
	b := packageStudent(t, c, "Второй", "+996706700102")
	l := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "15:00"})

	const n = 5
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl := c.api.client(t, "10.8.0.1")
			for k, v := range c.owner.cookies {
				cl.cookies[k] = v
			}
			// Разный порядок учеников в пакете — блокировки всё равно берутся по возрастанию id.
			if i%2 == 0 {
				codes[i] = mark(c, cl, l.ID, a, "present", b, "absent").Code
			} else {
				codes[i] = mark(c, cl, l.ID, b, "absent", a, "present").Code
			}
		}()
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("запрос %d: %d", i, code)
		}
	}
	if x, y := lessonsLeftOf(t, c, a), lessonsLeftOf(t, c, b); x != 7 || y != 7 {
		t.Errorf("lessons_left = %d / %d, want 7 / 7", x, y)
	}
}

func TestStudentAttendanceHistory(t *testing.T) {
	c := newCatalog(t, "+996706600001")
	s := c.student(t, "Ученик", "+996706600101", nil)
	start := c.today.FirstOfMonth().AddDays(-3)
	c.enroll(t, s.ID, map[string]any{"group_id": c.group.ID, "tariff_id": c.pkg.ID, "start_date": start.String()})

	prev := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": start.String(), "start_time": "10:00"})
	cur := createLesson(t, c, map[string]any{"group_id": c.group.ID, "date": c.today.String(), "start_time": "10:00"})
	mustMark(t, c, c.owner, prev.ID, s.ID, "absent")
	mustMark(t, c, c.owner, cur.ID, s.ID, "present")

	type item struct {
		LessonID uuid.UUID  `json:"lesson_id"`
		Date     civil.Date `json:"date"`
		Status   string     `json:"status"`
		Deducted bool       `json:"deducted"`
		Group    refJSON    `json:"group"`
	}
	path := centerPath(c.center, "/students/"+s.ID.String()+"/attendance")
	now := decodeItems[item](t, c.owner.do(http.MethodGet, path, nil)) // по умолчанию — текущий месяц
	if len(now) != 1 || now[0].LessonID != cur.ID || now[0].Status != "present" || !now[0].Deducted || now[0].Group.ID != c.group.ID {
		t.Errorf("текущий месяц = %+v", now)
	}
	m := start.Year()*100 + int(start.Month())
	old := decodeItems[item](t, c.owner.do(http.MethodGet, path+"?month="+start.String()[:7], nil))
	if len(old) != 1 || old[0].LessonID != prev.ID || old[0].Status != "absent" {
		t.Errorf("месяц %d = %+v", m, old)
	}
	assertField(t, c.owner.do(http.MethodGet, path+"?month=2026-13", nil), http.StatusBadRequest, "month")
	assertError(t, c.owner.do(http.MethodGet, centerPath(c.center, "/students/"+uuid.NewString()+"/attendance"), nil),
		http.StatusNotFound, "not_found")
}
