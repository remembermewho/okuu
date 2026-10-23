-- name: CreateCenter :one
-- Остальные настройки (billing_day, due_days, timezone, ...) — значения по умолчанию из схемы.
INSERT INTO centers (name, phone)
VALUES ($1, $2)
RETURNING *;

-- name: SetCenterStatus :exec
-- Заявка (REGISTRATION_MODE=approval): центр создаётся сразу в статусе pending.
UPDATE centers SET status = @status WHERE id = @id;

-- name: GetCenter :one
SELECT * FROM centers
WHERE id = $1;
