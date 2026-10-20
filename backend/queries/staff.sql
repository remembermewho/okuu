-- name: ListStaff :many
-- Сотрудники центра: сначала владелец, потом админы, потом преподаватели.
SELECT u.id AS user_id, u.name, u.phone, m.role
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.center_id = $1
ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, u.name, u.id;

-- name: GetStaffMemberForUpdate :one
-- Блокирует строку членства до конца транзакции (смена роли, удаление).
SELECT u.id AS user_id, u.name, u.phone, m.role
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.center_id = @center_id AND m.user_id = @user_id
FOR UPDATE OF m;

-- name: UpdateMembershipRole :exec
UPDATE memberships
SET role = @role
WHERE center_id = @center_id AND user_id = @user_id;

-- name: DeleteMembership :exec
DELETE FROM memberships
WHERE center_id = @center_id AND user_id = @user_id;

-- name: CountActiveGroupsOfTeacher :one
SELECT count(*)
FROM groups
WHERE center_id = @center_id AND teacher_id = @teacher_id AND NOT archived;
