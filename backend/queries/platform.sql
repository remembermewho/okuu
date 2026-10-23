-- Платформа: ссылки регистрации центров, панель суперадмина, журнал платформы.
-- registration_links.token = hex(sha256) от токена из ссылки {APP_ORIGIN}/register/{токен}.
-- «Действующая» ссылка: не использована, не отозвана, не просрочена (по часам БД).

-- name: CreateRegistrationLink :one
-- Срок действия — 14 дней.
INSERT INTO registration_links (token, note, created_by, expires_at)
VALUES (@token, @note, @created_by, now() + interval '14 days')
RETURNING id, expires_at;

-- name: IsRegistrationLinkActive :one
SELECT EXISTS (
  SELECT 1 FROM registration_links
  WHERE token = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
)::bool;

-- name: ClaimRegistrationLink :one
-- Атомарно помечает ссылку использованной: из двух одновременных регистраций пройдёт одна.
UPDATE registration_links
SET used_at = now()
WHERE token = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING id;

-- name: SetRegistrationLinkCenter :exec
UPDATE registration_links SET center_id = @center_id WHERE id = @id;

-- name: ListRegistrationLinks :many
SELECT l.id, l.note, l.created_at, l.expires_at, l.used_at, l.revoked_at, l.center_id,
       c.name AS center_name, u.name AS created_by_name
FROM registration_links l
LEFT JOIN centers c ON c.id = l.center_id
LEFT JOIN users u ON u.id = l.created_by
ORDER BY l.created_at DESC, l.id
LIMIT @lim;

-- name: RevokeRegistrationLink :one
-- Отозвать можно только неиспользованную ссылку.
UPDATE registration_links
SET revoked_at = now()
WHERE id = @id AND used_at IS NULL AND revoked_at IS NULL
RETURNING id, note;

-- name: GetRegistrationLink :one
SELECT id, note, used_at, revoked_at, expires_at FROM registration_links WHERE id = @id;

-- name: ListAdminCenters :many
-- Только агрегаты: ученики, родители, операции и журнал центра суперадмину не видны.
-- Владелец — первый по дате (у центра он один, кроме ручных правок в БД).
-- last_activity — последний вход любого сотрудника центра.
SELECT c.id, c.name, c.status, c.blocked_reason, c.plan, c.paid_until, c.is_demo, c.created_at,
       COALESCE(o.name, '')::text AS owner_name, COALESCE(o.phone, '')::text AS owner_phone,
       (SELECT count(*) FROM students s WHERE s.center_id = c.id AND s.status = 'active')::bigint AS active_students,
       (SELECT u.last_login_at FROM memberships m JOIN users u ON u.id = m.user_id
        WHERE m.center_id = c.id ORDER BY u.last_login_at DESC NULLS LAST LIMIT 1) AS last_activity
FROM centers c
LEFT JOIN LATERAL (
  SELECT u.name, u.phone FROM memberships m JOIN users u ON u.id = m.user_id
  WHERE m.center_id = c.id AND m.role = 'owner'
  ORDER BY u.created_at, u.id LIMIT 1
) o ON true
WHERE (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (sqlc.narg(q)::text IS NULL
       OR c.name ILIKE '%' || sqlc.narg(q)::text || '%'
       OR o.name ILIKE '%' || sqlc.narg(q)::text || '%'
       OR o.phone LIKE '%' || sqlc.narg(q_digits)::text || '%')
ORDER BY (c.status = 'pending') DESC, c.created_at DESC, c.id
LIMIT @lim;

-- name: GetAdminCenter :one
SELECT c.id, c.name, c.status, c.blocked_reason, c.plan, c.paid_until, c.is_demo, c.created_at, c.timezone,
       COALESCE(o.name, '')::text AS owner_name, COALESCE(o.phone, '')::text AS owner_phone,
       (SELECT count(*) FROM students s WHERE s.center_id = c.id AND s.status = 'active')::bigint AS active_students,
       (SELECT count(*) FROM students s WHERE s.center_id = c.id AND s.status = 'paused')::bigint AS paused_students,
       (SELECT count(*) FROM students s WHERE s.center_id = c.id AND s.status = 'left')::bigint AS left_students,
       (SELECT count(*) FROM groups g WHERE g.center_id = c.id AND NOT g.archived)::bigint AS groups_count,
       (SELECT count(*) FROM memberships m WHERE m.center_id = c.id)::bigint AS staff_count,
       (SELECT count(*) FROM parents p WHERE p.center_id = c.id AND p.tg_status = 'active')::bigint AS telegram_parents,
       (SELECT u.last_login_at FROM memberships m JOIN users u ON u.id = m.user_id
        WHERE m.center_id = c.id ORDER BY u.last_login_at DESC NULLS LAST LIMIT 1) AS last_activity
FROM centers c
LEFT JOIN LATERAL (
  SELECT u.name, u.phone FROM memberships m JOIN users u ON u.id = m.user_id
  WHERE m.center_id = c.id AND m.role = 'owner'
  ORDER BY u.created_at, u.id LIMIT 1
) o ON true
WHERE c.id = @id;

-- name: ApproveCenter :one
UPDATE centers SET status = 'active', blocked_reason = NULL
WHERE id = @id AND status = 'pending'
RETURNING id;

-- name: BlockCenter :one
UPDATE centers SET status = 'blocked', blocked_reason = @reason
WHERE id = @id AND status <> 'blocked'
RETURNING id;

-- name: UnblockCenter :one
UPDATE centers SET status = 'active', blocked_reason = NULL
WHERE id = @id AND status = 'blocked'
RETURNING id;

-- name: SetCenterPlan :one
UPDATE centers SET plan = @plan, paid_until = sqlc.narg(paid_until)::date
WHERE id = @id
RETURNING id;

-- name: CenterExists :one
SELECT EXISTS (SELECT 1 FROM centers WHERE id = $1)::bool;

-- name: SetPlatformAdmin :one
UPDATE users SET is_platform_admin = @is_admin WHERE phone = @phone
RETURNING id, name;

-- name: InsertPlatformAudit :exec
INSERT INTO platform_audit_log (admin_id, action, center_id, details)
VALUES (@admin_id, @action, @center_id, @details);

-- name: ListPlatformAudit :many
SELECT a.id, a.at, a.action, a.center_id, a.details, u.name AS admin_name, c.name AS center_name
FROM platform_audit_log a
LEFT JOIN users u ON u.id = a.admin_id
LEFT JOIN centers c ON c.id = a.center_id
WHERE (sqlc.narg(center_id)::uuid IS NULL OR a.center_id = sqlc.narg(center_id)::uuid)
ORDER BY a.id DESC
LIMIT @lim;
