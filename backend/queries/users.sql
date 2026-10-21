-- name: CreateUser :one
INSERT INTO users (phone, name, password_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByPhone :one
SELECT * FROM users
WHERE phone = $1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: UpdateUserName :exec
UPDATE users SET name = $2 WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;
