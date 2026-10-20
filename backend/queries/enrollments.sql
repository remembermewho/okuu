-- Активная запись — end_date IS NULL.

-- name: CreateEnrollment :one
INSERT INTO enrollments (center_id, student_id, group_id, tariff_id, start_date, lessons_left, billed_through)
VALUES (@center_id, @student_id, @group_id, @tariff_id, @start_date, @lessons_left, @billed_through)
RETURNING *;

-- name: HasActiveEnrollment :one
SELECT EXISTS (
  SELECT 1 FROM enrollments
  WHERE center_id = @center_id AND student_id = @student_id AND group_id = @group_id AND end_date IS NULL
);

-- name: ListStudentEnrollments :many
-- Для карточки: сначала активные, потом завершённые, свежие выше.
SELECT e.id, e.group_id, g.name AS group_name, e.tariff_id, t.name AS tariff_name, t.kind AS tariff_kind,
       t.price AS tariff_price, e.start_date, e.end_date, e.lessons_left, e.billed_through
FROM enrollments e
JOIN groups g ON g.id = e.group_id
JOIN tariffs t ON t.id = e.tariff_id
WHERE e.center_id = @center_id AND e.student_id = @student_id
ORDER BY (e.end_date IS NULL) DESC, e.start_date DESC, e.id;

-- name: GetEnrollmentForUpdate :one
SELECT * FROM enrollments
WHERE center_id = @center_id AND student_id = @student_id AND id = @id
FOR UPDATE;

-- name: EndEnrollment :exec
UPDATE enrollments SET end_date = @end_date
WHERE center_id = @center_id AND id = @id;

-- name: EndActiveEnrollments :many
-- R7: ученик ушёл — закрываем все активные записи. Запись с будущей датой начала
-- закрывается датой начала, чтобы не получить end_date < start_date.
UPDATE enrollments
SET end_date = GREATEST(start_date, @today::date)
WHERE center_id = @center_id AND student_id = @student_id AND end_date IS NULL
RETURNING id, end_date;

-- name: SetLessonsLeft :exec
UPDATE enrollments SET lessons_left = @lessons_left
WHERE center_id = @center_id AND id = @id;
