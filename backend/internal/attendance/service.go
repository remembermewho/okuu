// Package attendance — занятия и посещаемость (F5, R9–R12, экран «Сегодня»).
// Занятия создаются при первом открытии даты по расписанию группы (R11); отметка «был»/«не был»
// списывает занятие с абонемента, «уважительная» — по настройке центра (R9); преподаватель
// отмечает только свои группы и только за последние 7 дней (R12).
package attendance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/audit"
	"okuu/internal/civil"
	"okuu/internal/db/sqlc"
	"okuu/internal/groups"
	"okuu/internal/notify"
	"okuu/internal/validate"
)

// Отметки посещаемости.
const (
	StatusPresent = "present"
	StatusAbsent  = "absent"
	StatusExcused = "excused"
)

const (
	// TeacherWindowDays — преподаватель видит и отмечает занятия не старше этого (R12).
	TeacherWindowDays = 7
	// AheadDays — владелец и админ открывают занятия до сегодня + столько дней.
	AheadDays = 30
	// MaxMarks — предел отметок в одном запросе.
	MaxMarks = 200
)

var (
	ErrNotFound = errors.New("занятие не найдено")
	// ErrLessonCancelled — занятие отменено: отмечать нельзя.
	ErrLessonCancelled = errors.New("занятие отменено")
	// ErrLessonExists — занятие этой группы в это время уже есть.
	ErrLessonExists = errors.New("занятие уже есть")
	// ErrMarkWindow — преподаватель меняет отметки только за последние 7 дней (R12).
	ErrMarkWindow = errors.New("отметки старше 7 дней меняют владелец и админ")
	// ErrStudentNotFound — ученика нет в этом центре.
	ErrStudentNotFound = errors.New("ученик не найден")
)

// Viewer — кто смотрит: преподаватель видит только свои группы и не видит денег.
type Viewer struct {
	UserID  uuid.UUID
	Teacher bool
}

func (v Viewer) teacherFilter() *uuid.UUID {
	if v.Teacher {
		return &v.UserID
	}
	return nil
}

// StudentMark — ученик на занятии.
type StudentMark struct {
	StudentID   uuid.UUID
	FullName    string
	Mark        *string // nil — не отмечен
	LessonsLeft *int32  // только для абонемента
	HasDebt     bool    // баланс < 0; преподавателю не отдаётся
}

// Lesson — занятие с учениками.
type Lesson struct {
	ID        uuid.UUID
	GroupID   uuid.UUID
	GroupName string
	Date      civil.Date
	Start     civil.TimeOfDay
	End       *civil.TimeOfDay // по расписанию группы; nil — внеплановое
	Cancelled bool
	Students  []StudentMark
}

// MarkInput — отметка ученика; Status nil снимает отметку.
type MarkInput struct {
	StudentID uuid.UUID
	Status    *string
}

// Service — занятия и отметки.
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	now  func() time.Time
}

// NewService создаёт сервис; now — часы (nil — time.Now).
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, q: sqlc.New(pool), now: now}
}

// LessonsForDate — занятия на дату. Сначала идемпотентно создаёт занятия по расписанию активных групп
// (R11), затем отдаёт все занятия даты, включая внеплановые и отменённые. Преподаватель — только свои
// группы и даты от сегодня−7 до сегодня; владелец и админ — до сегодня+30.
func (s *Service) LessonsForDate(ctx context.Context, centerID uuid.UUID, v Viewer, date civil.Date) ([]Lesson, error) {
	c, err := s.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if err := checkViewWindow(v, date, c.today); err != nil {
		return nil, err
	}

	gs, err := s.q.ListScheduleGroups(ctx, sqlc.ListScheduleGroupsParams{CenterID: centerID, TeacherID: v.teacherFilter()})
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		slots, err := parseSchedule(g.Schedule)
		if err != nil {
			return nil, fmt.Errorf("группа %s: %w", g.ID, err)
		}
		for _, sl := range slots {
			if sl.Dow != date.ISOWeekday() {
				continue
			}
			start, err := civil.ParseTimeOfDay(sl.Start)
			if err != nil {
				return nil, fmt.Errorf("группа %s: %w", g.ID, err)
			}
			if err := s.q.EnsureLesson(ctx, sqlc.EnsureLessonParams{CenterID: centerID, GroupID: g.ID, Date: date, StartTime: start}); err != nil {
				return nil, err
			}
		}
	}

	rows, err := s.q.ListLessonsForDate(ctx, sqlc.ListLessonsForDateParams{CenterID: centerID, Date: date, TeacherID: v.teacherFilter()})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		l, err := s.withStudents(ctx, s.q, centerID, sqlc.GetLessonRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// Get — занятие с учениками. Чужая группа для преподавателя — ErrNotFound.
func (s *Service) Get(ctx context.Context, centerID uuid.UUID, v Viewer, id uuid.UUID) (Lesson, error) {
	row, err := s.lesson(ctx, centerID, v, id)
	if err != nil {
		return Lesson{}, err
	}
	return s.withStudents(ctx, s.q, centerID, row)
}

// Create — внеплановое занятие группы (владелец, админ). Дата — не позже сегодня+30.
func (s *Service) Create(ctx context.Context, actor, centerID, groupID uuid.UUID, date civil.Date, start civil.TimeOfDay) (Lesson, error) {
	if groupID == uuid.Nil {
		return Lesson{}, validate.Errorf("group_id", "Группа — обязательное поле")
	}
	if date.IsZero() {
		return Lesson{}, validate.Errorf("date", "Дата — обязательное поле")
	}
	c, err := s.center(ctx, centerID)
	if err != nil {
		return Lesson{}, err
	}
	if latest := c.today.AddDays(AheadDays); date.After(latest) {
		return Lesson{}, validate.Errorf("date", "Дата — не позже %s", latest)
	}

	var id uuid.UUID
	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		if _, err := q.LockActiveGroupForLesson(ctx, sqlc.LockActiveGroupForLessonParams{CenterID: centerID, ID: groupID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return validate.Unprocessablef("group_id", "Группа не найдена в этом центре или в архиве")
			}
			return err
		}
		id, err = q.CreateLesson(ctx, sqlc.CreateLessonParams{CenterID: centerID, GroupID: groupID, Date: date, StartTime: start})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrLessonExists
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.LessonCreate, Entity: "lesson", EntityID: &id,
			After: map[string]any{"group_id": groupID, "date": date, "start_time": start},
		})
	})
	if err != nil {
		return Lesson{}, err
	}
	return s.Get(ctx, centerID, Viewer{UserID: actor}, id)
}

// SetCancelled отменяет занятие (праздник) или возвращает его. Отмена в одной транзакции удаляет
// отметки и возвращает абонементам списанные ими занятия: отменённое занятие ничего не списывает (R11).
func (s *Service) SetCancelled(ctx context.Context, actor, centerID, id uuid.UUID, cancelled bool) (Lesson, error) {
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		l, err := q.LockLesson(ctx, sqlc.LockLessonParams{CenterID: centerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if l.Cancelled == cancelled {
			return nil
		}
		if !cancelled {
			if err := q.SetLessonCancelled(ctx, sqlc.SetLessonCancelledParams{CenterID: centerID, ID: id, Cancelled: false}); err != nil {
				return err
			}
			return audit.Record(ctx, tx, audit.Entry{
				CenterID: centerID, UserID: &actor, Action: audit.LessonRestore, Entity: "lesson", EntityID: &id,
				Before: map[string]any{"cancelled": true}, After: map[string]any{"cancelled": false},
			})
		}

		marks, err := q.ListLessonAttendance(ctx, id) // по student_id — стабильный порядок блокировок
		if err != nil {
			return err
		}
		before := map[string]any{}
		returned := 0
		for _, m := range marks {
			before[m.StudentID.String()] = m.Status
			if !m.Deducted {
				continue
			}
			enr, err := q.LockEnrollmentForMark(ctx, sqlc.LockEnrollmentForMarkParams{
				CenterID: centerID, StudentID: m.StudentID, GroupID: l.GroupID, Date: l.Date,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue // записи на эту дату уже нет — возвращать некуда
			}
			if err != nil {
				return err
			}
			if err := q.AdjustLessonsLeft(ctx, sqlc.AdjustLessonsLeftParams{CenterID: centerID, ID: enr.ID, Delta: 1}); err != nil {
				return err
			}
			returned++
		}
		if err := q.DeleteLessonAttendance(ctx, id); err != nil {
			return err
		}
		if err := q.SetLessonCancelled(ctx, sqlc.SetLessonCancelledParams{CenterID: centerID, ID: id, Cancelled: true}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &actor, Action: audit.LessonCancel, Entity: "lesson", EntityID: &id,
			Before: map[string]any{"cancelled": false, "marks": before},
			After:  map[string]any{"cancelled": true, "returned_lessons": returned},
		})
	})
	if err != nil {
		return Lesson{}, err
	}
	return s.Get(ctx, centerID, Viewer{UserID: actor}, id)
}

// Mark сохраняет отметки пакетом (R9, R12). Абонемент: present и absent списывают 1 занятие,
// excused — если в центре excused_deducts. Списание фиксируется в attendance.deducted, смена
// отметки пересчитывает остаток по разнице deducted; остаток может уйти в минус (R10).
func (s *Service) Mark(ctx context.Context, centerID uuid.UUID, v Viewer, lessonID uuid.UUID, marks []MarkInput) (Lesson, error) {
	if err := validateMarks(marks); err != nil {
		return Lesson{}, err
	}
	l, err := s.lesson(ctx, centerID, v, lessonID)
	if err != nil {
		return Lesson{}, err
	}
	c, err := s.center(ctx, centerID)
	if err != nil {
		return Lesson{}, err
	}
	// Блокировки — по возрастанию student_id: два параллельных сохранения не встанут в deadlock.
	sorted := slices.Clone(marks)
	slices.SortFunc(sorted, func(a, b MarkInput) int { return compareUUID(a.StudentID, b.StudentID) })

	err = s.inTx(ctx, func(tx pgx.Tx, q *sqlc.Queries) error {
		locked, err := q.LockLessonShared(ctx, sqlc.LockLessonSharedParams{CenterID: centerID, ID: lessonID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if locked.Cancelled {
			return ErrLessonCancelled
		}
		if locked.Date.After(c.today) {
			return validate.Unprocessablef("date", "Занятие %s ещё не прошло — отметить его нельзя", locked.Date)
		}
		if v.Teacher && locked.Date.Before(c.today.AddDays(-TeacherWindowDays)) {
			return ErrMarkWindow
		}

		before, after := map[string]any{}, map[string]any{}
		for _, m := range sorted {
			enr, err := q.LockEnrollmentForMark(ctx, sqlc.LockEnrollmentForMarkParams{
				CenterID: centerID, StudentID: m.StudentID, GroupID: l.GroupID, Date: l.Date,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return validate.Unprocessablef("student_id", "Ученик %s не записан в группу «%s» на %s", m.StudentID, l.GroupName, l.Date)
			}
			if err != nil {
				return err
			}
			var curStatus *string
			var curDeducted bool
			cur, err := q.GetAttendanceForUpdate(ctx, sqlc.GetAttendanceForUpdateParams{LessonID: lessonID, StudentID: m.StudentID})
			switch {
			case err == nil:
				curStatus, curDeducted = &cur.Status, cur.Deducted
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
			if sameStatus(curStatus, m.Status) {
				continue // та же отметка — не пересчитываем, даже если настройка центра сменилась
			}

			newDeducted := m.Status != nil && enr.TariffKind == "package" &&
				(*m.Status != StatusExcused || c.excusedDeducts)
			if delta := boolInt(curDeducted) - boolInt(newDeducted); delta != 0 {
				if err := q.AdjustLessonsLeft(ctx, sqlc.AdjustLessonsLeftParams{CenterID: centerID, ID: enr.ID, Delta: int32(delta)}); err != nil {
					return err
				}
				// R10: после списания осталось 1 занятие — напомнить родителю продлить.
				if delta < 0 && enr.LessonsLeft != nil && *enr.LessonsLeft+int32(delta) == 1 {
					if err := notify.Enqueue(ctx, tx, notify.PackageLowEvent(centerID, m.StudentID, enr.ID, c.today)); err != nil {
						return err
					}
				}
			}
			// «Не был» — родителю, если центр это включил; только по свежим занятиям (вчера и сегодня),
			// чтобы поздняя отметка за прошлую неделю не приходила как новость.
			if c.notifyAbsent && m.Status != nil && *m.Status == StatusAbsent && !locked.Date.Before(c.today.AddDays(-1)) {
				if err := notify.Enqueue(ctx, tx, notify.AbsentEvent(centerID, m.StudentID, lessonID)); err != nil {
					return err
				}
			}
			if m.Status == nil {
				err = q.DeleteAttendance(ctx, sqlc.DeleteAttendanceParams{LessonID: lessonID, StudentID: m.StudentID})
			} else {
				err = q.UpsertAttendance(ctx, sqlc.UpsertAttendanceParams{
					LessonID: lessonID, StudentID: m.StudentID, Status: *m.Status, MarkedBy: v.UserID, Deducted: newDeducted,
				})
			}
			if err != nil {
				return err
			}
			before[m.StudentID.String()] = curStatus
			after[m.StudentID.String()] = m.Status
		}
		if len(after) == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Entry{
			CenterID: centerID, UserID: &v.UserID, Action: audit.AttendanceMark, Entity: "lesson", EntityID: &lessonID,
			Before: before, After: after,
		})
	})
	if err != nil {
		return Lesson{}, err
	}
	return s.Get(ctx, centerID, v, lessonID)
}

// HistoryItem — отметка ученика на занятии.
type HistoryItem struct {
	LessonID  uuid.UUID
	Date      civil.Date
	Start     civil.TimeOfDay
	GroupID   uuid.UUID
	GroupName string
	Status    string
	Deducted  bool
}

// StudentHistory — отметки ученика за месяц month (любая дата месяца).
func (s *Service) StudentHistory(ctx context.Context, centerID, studentID uuid.UUID, month civil.Date) ([]HistoryItem, error) {
	exists, err := s.q.StudentExists(ctx, sqlc.StudentExistsParams{CenterID: centerID, ID: studentID})
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrStudentNotFound
	}
	from := month.FirstOfMonth()
	rows, err := s.q.ListStudentAttendance(ctx, sqlc.ListStudentAttendanceParams{
		CenterID: centerID, StudentID: studentID, DateFrom: from, DateTo: from.AddMonths(1),
	})
	if err != nil {
		return nil, err
	}
	out := make([]HistoryItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, HistoryItem{LessonID: r.LessonID, Date: r.Date, Start: r.StartTime, GroupID: r.GroupID,
			GroupName: r.GroupName, Status: r.Status, Deducted: r.Deducted})
	}
	return out, nil
}

// Today — «сегодня» в часовом поясе центра.
func (s *Service) Today(ctx context.Context, centerID uuid.UUID) (civil.Date, error) {
	c, err := s.center(ctx, centerID)
	return c.today, err
}

func (s *Service) lesson(ctx context.Context, centerID uuid.UUID, v Viewer, id uuid.UUID) (sqlc.GetLessonRow, error) {
	row, err := s.q.GetLesson(ctx, sqlc.GetLessonParams{CenterID: centerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	if v.Teacher && (row.TeacherID == nil || *row.TeacherID != v.UserID) {
		return row, ErrNotFound // чужая группа — как будто её нет
	}
	return row, nil
}

func (s *Service) withStudents(ctx context.Context, q *sqlc.Queries, centerID uuid.UUID, r sqlc.GetLessonRow) (Lesson, error) {
	l := Lesson{ID: r.ID, GroupID: r.GroupID, GroupName: r.GroupName, Date: r.Date, Start: r.StartTime, Cancelled: r.Cancelled}
	slots, err := parseSchedule(r.Schedule)
	if err != nil {
		return Lesson{}, err
	}
	for _, sl := range slots {
		if sl.Dow == r.Date.ISOWeekday() && sl.Start == r.StartTime.String() {
			if end, err := civil.ParseTimeOfDay(sl.End); err == nil {
				l.End = &end
			}
			break
		}
	}
	rows, err := q.ListLessonStudents(ctx, sqlc.ListLessonStudentsParams{LessonID: r.ID, CenterID: centerID, GroupID: r.GroupID, Date: r.Date})
	if err != nil {
		return Lesson{}, err
	}
	l.Students = make([]StudentMark, 0, len(rows))
	for _, st := range rows {
		m := StudentMark{StudentID: st.StudentID, FullName: st.FullName, Mark: st.Mark, HasDebt: st.Balance < 0}
		if st.TariffKind == "package" {
			m.LessonsLeft = st.LessonsLeft
		}
		l.Students = append(l.Students, m)
	}
	return l, nil
}

type centerInfo struct {
	today          civil.Date
	excusedDeducts bool
	notifyAbsent   bool
}

func (s *Service) center(ctx context.Context, centerID uuid.UUID) (centerInfo, error) {
	c, err := s.q.GetCenter(ctx, centerID)
	if err != nil {
		return centerInfo{}, err
	}
	today, err := civil.Today(s.now(), c.Timezone)
	if err != nil {
		return centerInfo{}, err
	}
	return centerInfo{today: today, excusedDeducts: c.ExcusedDeducts, notifyAbsent: c.NotifyAbsent}, nil
}

func checkViewWindow(v Viewer, date, today civil.Date) error {
	if date.IsZero() {
		return validate.Errorf("date", "Дата — обязательное поле, ГГГГ-ММ-ДД")
	}
	if v.Teacher {
		if from := today.AddDays(-TeacherWindowDays); date.Before(from) || date.After(today) {
			return validate.Errorf("date", "Преподаватель открывает занятия с %s по %s", from, today)
		}
		return nil
	}
	if latest := today.AddDays(AheadDays); date.After(latest) {
		return validate.Errorf("date", "Дата — не позже %s", latest)
	}
	return nil
}

func validateMarks(marks []MarkInput) error {
	if len(marks) == 0 {
		return validate.Errorf("marks", "Передайте хотя бы одну отметку")
	}
	if len(marks) > MaxMarks {
		return validate.Errorf("marks", "Не больше %d отметок за раз", MaxMarks)
	}
	seen := map[uuid.UUID]bool{}
	for _, m := range marks {
		if m.StudentID == uuid.Nil {
			return validate.Errorf("student_id", "У каждой отметки нужен student_id")
		}
		if seen[m.StudentID] {
			return validate.Errorf("student_id", "Ученик %s указан дважды", m.StudentID)
		}
		seen[m.StudentID] = true
		if m.Status != nil {
			switch *m.Status {
			case StatusPresent, StatusAbsent, StatusExcused:
			default:
				return validate.Errorf("status", "Отметка — present, absent, excused или null")
			}
		}
	}
	return nil
}

func parseSchedule(b []byte) ([]groups.Slot, error) {
	var slots []groups.Slot
	if err := json.Unmarshal(b, &slots); err != nil {
		return nil, fmt.Errorf("расписание: %w", err)
	}
	return slots, nil
}

func sameStatus(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func compareUUID(a, b uuid.UUID) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
