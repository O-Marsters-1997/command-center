-- name: AddReviewComment :one
INSERT INTO review_comments (ticket_id, tip, path, line, side, hunk, body, created_at)
VALUES (sqlc.arg(ticket_id), sqlc.arg(tip), sqlc.arg(path), sqlc.arg(line), sqlc.arg(side), sqlc.arg(hunk), sqlc.arg(body), sqlc.arg(created_at))
RETURNING id;

-- name: DeleteReviewComment :exec
DELETE FROM review_comments WHERE ticket_id = sqlc.arg(ticket_id) AND id = sqlc.arg(id);

-- name: ListReviewComments :many
SELECT id, tip, path, line, side, hunk, body
FROM review_comments
WHERE ticket_id = sqlc.arg(ticket_id)
ORDER BY id
LIMIT 1000;
