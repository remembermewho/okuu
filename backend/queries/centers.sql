-- name: CreateCenter :one
-- Остальные настройки (billing_day, due_days, timezone, ...) — значения по умолчанию из схемы.
INSERT INTO centers (name, phone)
VALUES ($1, $2)
RETURNING *;

-- name: GetCenter :one
SELECT * FROM centers
WHERE id = $1;
