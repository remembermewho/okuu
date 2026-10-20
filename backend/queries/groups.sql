-- name: ListGroups :many
-- teacher_id = NULL — все группы центра; иначе только группы этого преподавателя.
-- students_count — ученики с незавершённой записью в группу.
SELECT g.id, g.name, g.teacher_id, u.name AS teacher_name, g.schedule, g.default_tariff_id, g.archived,
       (SELECT count(DISTINCT e.student_id) FROM enrollments e
        WHERE e.group_id = g.id AND e.end_date IS NULL) AS students_count
FROM groups g
LEFT JOIN users u ON u.id = g.teacher_id
WHERE g.center_id = @center_id AND g.archived = @archived
  AND (sqlc.narg(teacher_id)::uuid IS NULL OR g.teacher_id = sqlc.narg(teacher_id)::uuid)
ORDER BY g.name, g.id;

-- name: GetGroup :one
SELECT g.id, g.name, g.teacher_id, u.name AS teacher_name, g.schedule, g.default_tariff_id, g.archived,
       (SELECT count(DISTINCT e.student_id) FROM enrollments e
        WHERE e.group_id = g.id AND e.end_date IS NULL) AS students_count
FROM groups g
LEFT JOIN users u ON u.id = g.teacher_id
WHERE g.center_id = @center_id AND g.id = @id;

-- name: GetGroupForUpdate :one
SELECT * FROM groups
WHERE center_id = @center_id AND id = @id
FOR UPDATE;

-- name: CreateGroup :one
INSERT INTO groups (center_id, name, teacher_id, schedule, default_tariff_id)
VALUES (@center_id, @name, @teacher_id, @schedule, @default_tariff_id)
RETURNING id;

-- name: UpdateGroup :exec
UPDATE groups
SET name = @name, teacher_id = @teacher_id, schedule = @schedule, default_tariff_id = @default_tariff_id
WHERE center_id = @center_id AND id = @id;

-- name: ArchiveGroup :exec
UPDATE groups SET archived = true
WHERE center_id = @center_id AND id = @id;

-- name: LockCenterMember :one
-- Сотрудник этого центра (любая роль). FOR SHARE: удаление сотрудника (staff.Remove берёт
-- FOR UPDATE) дождётся конца транзакции и увидит новую группу — или наоборот, мы не увидим
-- уже удалённого сотрудника.
SELECT user_id FROM memberships
WHERE center_id = @center_id AND user_id = @user_id
FOR SHARE;

-- name: LockActiveGroup :one
-- Группа этого центра и не архивная; FOR SHARE до конца транзакции записи ученика.
SELECT id, name, default_tariff_id FROM groups
WHERE center_id = @center_id AND id = @id AND NOT archived
FOR SHARE;
