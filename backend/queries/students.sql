-- balance — сумма неотменённых операций ledger (R13); нигде не хранится.

-- name: CreateStudent :one
INSERT INTO students (center_id, parent_id, full_name, birth_date, discount_kind, discount_value)
VALUES (@center_id, @parent_id, @full_name, @birth_date, @discount_kind, @discount_value)
RETURNING *;

-- name: GetStudent :one
SELECT s.id, s.full_name, s.birth_date, s.status, s.discount_kind, s.discount_value, s.created_at,
       p.id AS parent_id, p.name AS parent_name, p.phone AS parent_phone, p.tg_status AS parent_tg_status,
       (SELECT COALESCE(sum(l.amount), 0) FROM ledger l
        WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL)::bigint AS balance
FROM students s
LEFT JOIN parents p ON p.id = s.parent_id
WHERE s.center_id = @center_id AND s.id = @id;

-- name: GetStudentForUpdate :one
SELECT * FROM students
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: UpdateStudent :one
UPDATE students
SET full_name = @full_name, birth_date = @birth_date, status = @status,
    discount_kind = @discount_kind, discount_value = @discount_value, parent_id = @parent_id
WHERE center_id = @center_id AND id = @id
RETURNING *;

-- name: ListStudents :many
-- Фильтры: NULL — не фильтровать. name_q/phone_q — готовые шаблоны LIKE (экранированы в коде).
-- Пагинация по ключу (full_name, id): after_* — последняя строка предыдущей страницы.
SELECT s.id, s.full_name, s.birth_date, s.status, s.discount_kind, s.discount_value,
       p.id AS parent_id, p.name AS parent_name, p.phone AS parent_phone, p.tg_status AS parent_tg_status,
       (SELECT COALESCE(sum(l.amount), 0) FROM ledger l
        WHERE l.center_id = s.center_id AND l.student_id = s.id AND l.cancelled_at IS NULL)::bigint AS balance,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', g.id, 'name', g.name) ORDER BY g.name, g.id)
                 FROM enrollments e JOIN groups g ON g.id = e.group_id
                 WHERE e.student_id = s.id AND e.end_date IS NULL), '[]'::jsonb)::jsonb AS groups
FROM students s
LEFT JOIN parents p ON p.id = s.parent_id
WHERE s.center_id = @center_id
  AND (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text)
  AND (sqlc.narg(tg)::text IS NULL OR COALESCE(p.tg_status, 'none') = sqlc.narg(tg)::text)
  AND (sqlc.narg(group_id)::uuid IS NULL OR EXISTS (
        SELECT 1 FROM enrollments e
        WHERE e.student_id = s.id AND e.group_id = sqlc.narg(group_id)::uuid AND e.end_date IS NULL))
  AND (sqlc.narg(name_q)::text IS NULL
       OR s.full_name ILIKE sqlc.narg(name_q)::text
       OR (sqlc.narg(phone_q)::text IS NOT NULL AND p.phone LIKE sqlc.narg(phone_q)::text))
  AND (sqlc.narg(after_name)::text IS NULL
       OR (s.full_name, s.id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY s.full_name, s.id
LIMIT @lim;
