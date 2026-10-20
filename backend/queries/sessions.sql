-- token_hash = sha256 от токена из cookie; сам токен в БД не хранится.

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, user_agent)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- Просроченная сессия не возвращается, даже если ещё не удалена.
SELECT * FROM sessions
WHERE token_hash = $1 AND expires_at > now();

-- name: TouchSession :exec
-- Продление при активности; решение «не чаще раза в сутки» принимает вызывающий код.
UPDATE sessions
SET last_seen_at = now(), expires_at = $2
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at <= now();
