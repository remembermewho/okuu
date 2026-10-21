-- token = hex(sha256) от токена из ссылки. «Действующая» ссылка: не использована, не отозвана,
-- не просрочена (по часам БД).

-- name: RevokeUserPasswordResets :exec
-- Новая ссылка заменяет прежние: неиспользованные ссылки пользователя перестают работать.
UPDATE password_resets
SET revoked_at = now()
WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL;

-- name: CreatePasswordReset :one
-- Срок действия — 24 часа.
INSERT INTO password_resets (token, user_id, center_id, created_by, expires_at)
VALUES (@token, @user_id, @center_id, @created_by, now() + interval '24 hours')
RETURNING id, expires_at;

-- name: GetActivePasswordReset :one
-- Для публичной страницы сброса: имя пользователя и центр, выдавший ссылку (NULL — поддержка).
SELECT r.id, u.name AS user_name, c.name AS center_name
FROM password_resets r
JOIN users u ON u.id = r.user_id
LEFT JOIN centers c ON c.id = r.center_id
WHERE r.token = $1 AND r.used_at IS NULL AND r.revoked_at IS NULL AND r.expires_at > now();

-- name: ClaimPasswordReset :one
-- Атомарно помечает ссылку использованной: из двух одновременных сбросов пройдёт один.
UPDATE password_resets
SET used_at = now()
WHERE token = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING id, user_id, center_id;
