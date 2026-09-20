-- name: CreateUser :exec
INSERT INTO users (email, password_hash, created_at)
VALUES ($1, $2, $3);

-- name: UserForLogin :one
SELECT id, email, password_hash FROM users WHERE email = $1;
