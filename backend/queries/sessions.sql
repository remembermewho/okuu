-- token_hash = sha256 от токена из cookie; сам токен в БД не хранится.

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, user_agent)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: TouchLastLogin :exec
-- Каждая новая сессия — вход: «последняя активность» центра в панели суперадмина.
UPDATE users SET last_login_at = now() WHERE id = $1;

-- name: GetSessionByTokenHash :one
-- Просроченная сессия не возвращается, даже если ещё не удалена.
SELECT * FROM sessions
WHERE token_hash = $1 AND expires_at > now();

-- name: TouchSession :exec
-- Продление при активности; решение «не чаще раза в сутки» принимает вызывающий код.
-- last_seen_at передаётся из приложения: решение о продлении принимается по тем же часам.
UPDATE sessions
SET last_seen_at = @last_seen_at, expires_at = @expires_at
WHERE token_hash = @token_hash;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteUserSessions :execrows
-- «Выйти на всех устройствах», сброс пароля: все сессии пользователя.
DELETE FROM sessions
WHERE user_id = $1;

-- name: DeleteUserSessionsExcept :execrows
-- Смена пароля: все сессии пользователя, кроме текущей.
DELETE FROM sessions
WHERE user_id = @user_id AND token_hash <> @keep_token_hash;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at <= now();
