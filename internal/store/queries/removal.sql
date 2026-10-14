-- name: ListLatestRemovalRefusals :many
SELECT DISTINCT ON (ticket_id) ticket_id, detail
FROM events
WHERE ticket_id IS NOT NULL AND kind = sqlc.arg(kind)
ORDER BY ticket_id, id DESC;
