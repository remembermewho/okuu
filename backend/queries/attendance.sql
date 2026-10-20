-- Занятия и посещаемость (F5, R9–R12). attendance не хранит center_id — изоляция через lessons.

-- name: ListScheduleGroups :many
-- Активные группы с расписанием: из них на дату создаются занятия (R11).
-- teacher_id = NULL — все группы центра; иначе только группы этого преподавателя.
SELECT id, schedule FROM groups
WHERE center_id = @center_id AND NOT archived
  AND (sqlc.narg(teacher_id)::uuid IS NULL OR teacher_id = sqlc.narg(teacher_id)::uuid);

-- name: EnsureLesson :exec
-- Идемпотентно: занятие уникально по группе + дате + времени.
INSERT INTO lessons (center_id, group_id, date, start_time)
VALUES (@center_id, @group_id, @date, @start_time)
ON CONFLICT (group_id, date, start_time) DO NOTHING;

-- name: CreateLesson :one
-- Внеплановое занятие; совпадение с существующим — unique violation.
INSERT INTO lessons (center_id, group_id, date, start_time)
VALUES (@center_id, @group_id, @date, @start_time)
RETURNING id;

-- name: ListLessonsForDate :many
SELECT l.id, l.group_id, l.date, l.start_time, l.cancelled, g.name AS group_name, g.teacher_id, g.schedule
FROM lessons l
JOIN groups g ON g.id = l.group_id
WHERE l.center_id = @center_id AND l.date = @date AND NOT g.archived
  AND (sqlc.narg(teacher_id)::uuid IS NULL OR g.teacher_id = sqlc.narg(teacher_id)::uuid)
ORDER BY l.start_time, g.name, l.id;

-- name: GetLesson :one
SELECT l.id, l.group_id, l.date, l.start_time, l.cancelled, g.name AS group_name, g.teacher_id, g.schedule
FROM lessons l
JOIN groups g ON g.id = l.group_id
WHERE l.center_id = @center_id AND l.id = @id;

-- name: LockLesson :one
-- FOR UPDATE: отмена занятия; отметки берут FOR SHARE и ждут её.
SELECT id, group_id, date, cancelled FROM lessons
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: LockLessonShared :one
SELECT id, group_id, date, cancelled FROM lessons
WHERE center_id = @center_id AND id = @id
FOR SHARE;

-- name: SetLessonCancelled :exec
UPDATE lessons SET cancelled = @cancelled
WHERE center_id = @center_id AND id = @id;

-- name: ListLessonStudents :many
-- Ученики занятия: запись в группу активна на дату занятия (start_date <= date, end_date NULL или >= date).
-- Если записей на дату несколько (закрыли и открыли в тот же день) — берём последнюю.
-- balance — только для owner/admin (has_debt); преподавателю наружу не отдаётся.
SELECT DISTINCT ON (s.full_name, s.id)
       s.id AS student_id, s.full_name, e.id AS enrollment_id, t.kind AS tariff_kind, e.lessons_left,
       a.status AS mark, a.deducted,
       (SELECT COALESCE(sum(lg.amount), 0) FROM ledger lg
        WHERE lg.center_id = s.center_id AND lg.student_id = s.id AND lg.cancelled_at IS NULL)::bigint AS balance
FROM enrollments e
JOIN students s ON s.id = e.student_id
JOIN tariffs t ON t.id = e.tariff_id
LEFT JOIN attendance a ON a.lesson_id = @lesson_id AND a.student_id = s.id
WHERE e.center_id = @center_id AND e.group_id = @group_id
  AND e.start_date <= @date::date AND (e.end_date IS NULL OR e.end_date >= @date::date)
ORDER BY s.full_name, s.id, e.start_date DESC;

-- name: LockEnrollmentForMark :one
-- Запись ученика в группу, активная на дату занятия; FOR UPDATE — остаток абонемента меняется.
SELECT e.id, t.kind AS tariff_kind, e.lessons_left
FROM enrollments e
JOIN tariffs t ON t.id = e.tariff_id
WHERE e.center_id = @center_id AND e.student_id = @student_id AND e.group_id = @group_id
  AND e.start_date <= @date::date AND (e.end_date IS NULL OR e.end_date >= @date::date)
ORDER BY e.start_date DESC
LIMIT 1
FOR UPDATE OF e;

-- name: GetAttendanceForUpdate :one
SELECT status, deducted FROM attendance
WHERE lesson_id = @lesson_id AND student_id = @student_id
FOR UPDATE;

-- name: UpsertAttendance :exec
INSERT INTO attendance (lesson_id, student_id, status, marked_by, deducted)
VALUES (@lesson_id, @student_id, @status, @marked_by, @deducted)
ON CONFLICT (lesson_id, student_id)
DO UPDATE SET status = EXCLUDED.status, marked_by = EXCLUDED.marked_by, marked_at = now(), deducted = EXCLUDED.deducted;

-- name: DeleteAttendance :exec
DELETE FROM attendance
WHERE lesson_id = @lesson_id AND student_id = @student_id;

-- name: ListLessonAttendance :many
SELECT student_id, status, deducted FROM attendance
WHERE lesson_id = @lesson_id
ORDER BY student_id;

-- name: DeleteLessonAttendance :exec
DELETE FROM attendance WHERE lesson_id = @lesson_id;

-- name: AdjustLessonsLeft :exec
-- delta > 0 — вернуть занятия абонементу, < 0 — списать (может уйти в минус, R10).
UPDATE enrollments SET lessons_left = lessons_left + @delta::int
WHERE center_id = @center_id AND id = @id AND lessons_left IS NOT NULL;

-- name: ListStudentAttendance :many
-- История посещений ученика за период [from, to).
SELECT l.id AS lesson_id, l.date, l.start_time, g.id AS group_id, g.name AS group_name, a.status, a.deducted
FROM attendance a
JOIN lessons l ON l.id = a.lesson_id
JOIN groups g ON g.id = l.group_id
WHERE l.center_id = @center_id AND a.student_id = @student_id
  AND l.date >= @date_from::date AND l.date < @date_to::date
ORDER BY l.date, l.start_time, l.id;

-- name: LockActiveGroupForLesson :one
SELECT id FROM groups
WHERE center_id = @center_id AND id = @id AND NOT archived
FOR SHARE;
