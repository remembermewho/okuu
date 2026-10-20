-- token = hex(sha256) от токена из ссылки; сам токен в БД не хранится.
-- «Действующее» приглашение: kind = 'staff', не использовано, не отозвано, не просрочено (по часам БД).

-- name: CreateStaffInvite :one
-- Срок действия — 7 дней.
INSERT INTO invites (token, center_id, kind, role, name, created_by, expires_at)
VALUES (@token, @center_id, 'staff', @role, @name, @created_by, now() + interval '7 days')
RETURNING id, center_id, role, name, expires_at, created_at;

-- name: ListActiveStaffInvites :many
SELECT id, role, name, expires_at, created_at
FROM invites
WHERE center_id = $1 AND kind = 'staff'
  AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at DESC, id;

-- name: RevokeStaffInvite :one
UPDATE invites
SET revoked_at = now()
WHERE id = @id AND center_id = @center_id AND kind = 'staff'
  AND used_at IS NULL AND revoked_at IS NULL
RETURNING id, role, name, expires_at;

-- name: GetActiveStaffInvite :one
-- Для публичной страницы приглашения.
SELECT i.id, i.role, i.name, i.expires_at, c.name AS center_name
FROM invites i
JOIN centers c ON c.id = i.center_id
WHERE i.token = $1 AND i.kind = 'staff'
  AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now();

-- name: ClaimStaffInvite :one
-- Атомарно помечает приглашение использованным: из двух одновременных принятий пройдёт одно.
-- Вызывать в транзакции принятия: при ошибке дальше отметка откатится.
UPDATE invites
SET used_at = now()
WHERE token = $1 AND kind = 'staff'
  AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING id, center_id, role, name;
