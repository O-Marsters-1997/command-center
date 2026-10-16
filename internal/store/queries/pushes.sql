-- name: RecordPush :exec
INSERT INTO pushes (ticket_id, pushed_tip, base_branch, base_sha_at_push, pushed_at)
VALUES (sqlc.arg(ticket_id), sqlc.arg(pushed_tip), sqlc.arg(base_branch), sqlc.arg(base_sha_at_push), sqlc.arg(pushed_at));

-- name: RestackedSinceLastPush :many
SELECT DISTINCT e.ticket_id
FROM events e
LEFT JOIN (
    SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
) p ON p.ticket_id = e.ticket_id
WHERE e.kind = sqlc.arg(kind) AND (p.pushed_at IS NULL OR e.at >= p.pushed_at);

-- name: ListLatestPushes :many
SELECT DISTINCT ON (ticket_id) ticket_id, pushed_tip, base_branch, base_sha_at_push, pushed_at
FROM pushes
ORDER BY ticket_id, id DESC;

-- name: ListPushFacts :many
SELECT DISTINCT ON (e.ticket_id) e.ticket_id, e.kind, e.detail
FROM events e
LEFT JOIN (
    SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
) p ON p.ticket_id = e.ticket_id
WHERE e.ticket_id IS NOT NULL
  AND e.kind IN (SELECT jsonb_array_elements_text(sqlc.arg(kinds)::jsonb))
  AND (p.pushed_at IS NULL OR e.at > p.pushed_at)
ORDER BY e.ticket_id, e.id DESC;
