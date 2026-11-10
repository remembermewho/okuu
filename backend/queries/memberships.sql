-- name: CreateMembership :one
INSERT INTO memberships (user_id, center_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetMembership :one
-- Проверка членства и роли для /c/{centerId}/... в middleware.
SELECT * FROM memberships
WHERE user_id = $1 AND center_id = $2;

-- name: GetMembershipAccess :one
-- Middleware /c/{centerId}/...: членство, роль и статус центра (pending и blocked — без доступа).
SELECT m.user_id, m.center_id, m.role, c.status AS center_status
FROM memberships m
JOIN centers c ON c.id = m.center_id
WHERE m.user_id = $1 AND m.center_id = $2;

-- name: ListMembershipsByUser :many
-- Центры пользователя для GET /me.
SELECT m.center_id, m.role, c.name AS center_name, c.timezone, c.status AS center_status, c.is_demo
FROM memberships m
JOIN centers c ON c.id = m.center_id
WHERE m.user_id = $1
ORDER BY c.name, c.id;
