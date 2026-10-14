-- name: ListLatestRefreshOutcomes :many
SELECT DISTINCT ON (e.ticket_id) e.ticket_id, e.kind, e.detail
FROM events e
LEFT JOIN (
    SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
) p ON p.ticket_id = e.ticket_id
WHERE e.ticket_id IS NOT NULL
  AND e.kind IN (SELECT jsonb_array_elements_text(sqlc.arg(kinds)::jsonb))
  AND (p.pushed_at IS NULL OR e.at > p.pushed_at)
ORDER BY e.ticket_id, e.id DESC;
