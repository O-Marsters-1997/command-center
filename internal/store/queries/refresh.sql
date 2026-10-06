-- name: LatestRefreshOutcomes :many
SELECT e.ticket_id, e.kind, e.detail
FROM events e
JOIN (
    SELECT e2.ticket_id, MAX(e2.id) AS id
    FROM events e2
    LEFT JOIN (
        SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
    ) p ON p.ticket_id = e2.ticket_id
    WHERE e2.kind IN ($1, $2, $3, $4, $5) AND (p.pushed_at IS NULL OR e2.at > p.pushed_at)
    GROUP BY e2.ticket_id
) latest ON latest.ticket_id = e.ticket_id AND latest.id = e.id;
