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
