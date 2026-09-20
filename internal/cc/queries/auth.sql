-- name: CreateUser :exec
INSERT INTO users (email, password_hash, created_at)
VALUES ($1, $2, $3);

-- name: UserForLogin :one
SELECT id, email, password_hash FROM users WHERE email = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= $1;

-- name: UpdatePasswordByEmail :one
UPDATE users SET password_hash = $2 WHERE email = $1 RETURNING id;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;
