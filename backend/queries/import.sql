-- Импорт из Excel/CSV (F4).

-- name: CreateImportSession :one
-- Превью живёт час.
INSERT INTO import_sessions (center_id, created_by, filename, rows, expires_at)
VALUES (@center_id, @created_by, @filename, @rows, now() + interval '1 hour')
RETURNING id, expires_at;

-- name: LockImportSession :one
SELECT id, rows, expires_at, committed_at, expires_at <= now() AS expired
FROM import_sessions
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: CommitImportSession :exec
UPDATE import_sessions SET committed_at = now(), result = @result
WHERE center_id = @center_id AND id = @id;

-- name: ListExistingStudentKeys :many
-- Ученики центра с телефоном родителя — для поиска дубликатов (ФИО + телефон).
SELECT s.full_name, p.phone
FROM students s
JOIN parents p ON p.id = s.parent_id
WHERE s.center_id = @center_id;

-- name: ListParentsByCenter :many
SELECT phone, name FROM parents WHERE center_id = @center_id;

-- name: ListActiveGroupNames :many
SELECT id, name FROM groups WHERE center_id = @center_id AND NOT archived ORDER BY name;
