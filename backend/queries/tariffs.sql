-- name: ListTariffs :many
SELECT * FROM tariffs
WHERE center_id = @center_id AND archived = @archived
ORDER BY name, id;

-- name: GetTariff :one
SELECT * FROM tariffs
WHERE center_id = @center_id AND id = @id;

-- name: GetTariffForUpdate :one
-- Блокирует тариф на время изменения: запись ученика (шаг 7) берёт FOR SHARE и ждёт.
SELECT * FROM tariffs
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: LockActiveTariff :one
-- Тариф этого центра и не архивный; FOR SHARE не даёт архивировать его до конца транзакции.
SELECT id FROM tariffs
WHERE center_id = @center_id AND id = @id AND NOT archived
FOR SHARE;

-- name: CreateTariff :one
INSERT INTO tariffs (center_id, name, kind, price, lessons_count)
VALUES (@center_id, @name, @kind, @price, @lessons_count)
RETURNING *;

-- name: UpdateTariff :one
UPDATE tariffs
SET name = @name, kind = @kind, price = @price, lessons_count = @lessons_count
WHERE center_id = @center_id AND id = @id
RETURNING *;

-- name: ArchiveTariff :exec
UPDATE tariffs SET archived = true
WHERE center_id = @center_id AND id = @id;

-- name: CountEnrollmentsByTariff :one
-- Любые записи, включая завершённые: смена kind сломала бы историю начислений.
SELECT count(*) FROM enrollments
WHERE center_id = @center_id AND tariff_id = @tariff_id;
