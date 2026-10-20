-- name: InsertParentIfAbsent :one
-- Нет строки в ответе — родитель с этим телефоном в центре уже есть (берите GetParentByPhone).
INSERT INTO parents (center_id, name, phone)
VALUES (@center_id, @name, @phone)
ON CONFLICT (center_id, phone) DO NOTHING
RETURNING *;

-- name: GetParentByPhone :one
SELECT * FROM parents
WHERE center_id = @center_id AND phone = @phone;

-- name: GetParentForUpdate :one
SELECT * FROM parents
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: UpdateParent :one
UPDATE parents
SET name = @name, phone = @phone
WHERE center_id = @center_id AND id = @id
RETURNING *;
