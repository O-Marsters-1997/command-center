-- name: CreateUser :exec
INSERT INTO users (email, password_hash, created_at)
VALUES ($1, $2, $3);

-- name: UserForLogin :one
SELECT id, email, password_hash FROM users WHERE email = $1;

-- name: IssueSession :exec
INSERT INTO sessions (user_id, token_sha, created_at, expires_at)
VALUES ($1, $2, $3, $4);

-- name: SessionOwner :one
SELECT user_id FROM sessions WHERE token_sha = $1 AND expires_at > $2;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: UpdatePasswordByEmail :one
UPDATE users SET password_hash = $2 WHERE email = $1 RETURNING id;
