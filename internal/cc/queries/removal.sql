-- name: LatestRemovalRefusals :many
SELECT e.ticket_id, e.detail
FROM events e
JOIN (
    SELECT e2.ticket_id, MAX(e2.id) AS id FROM events e2 WHERE e2.kind = $1 GROUP BY e2.ticket_id
) latest ON latest.ticket_id = e.ticket_id AND latest.id = e.id;
