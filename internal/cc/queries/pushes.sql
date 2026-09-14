-- name: RecordPush :exec
INSERT INTO pushes (ticket_id, pushed_tip, base_branch, base_sha_at_push, pushed_at)
VALUES (?, ?, ?, ?, ?);

-- name: RestackedSinceLastPush :many
SELECT DISTINCT e.ticket_id
FROM events e
LEFT JOIN (
    SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
) p ON p.ticket_id = e.ticket_id
WHERE e.kind = ? AND e.at >= COALESCE(p.pushed_at, '');

-- name: LastPushedTips :many
SELECT p.ticket_id, p.pushed_tip FROM pushes p
JOIN (SELECT ticket_id, MAX(id) AS id FROM pushes GROUP BY ticket_id) latest
  ON latest.ticket_id = p.ticket_id AND latest.id = p.id;

-- name: LatestPushes :many
SELECT p.ticket_id, p.pushed_tip, p.base_branch, p.base_sha_at_push, p.pushed_at FROM pushes p
JOIN (SELECT ticket_id, MAX(id) AS id FROM pushes GROUP BY ticket_id) latest
  ON latest.ticket_id = p.ticket_id AND latest.id = p.id;

-- name: PushFacts :many
SELECT e.ticket_id, e.kind, e.detail
FROM events e
JOIN (
    SELECT e2.ticket_id, MAX(e2.id) AS id
    FROM events e2
    LEFT JOIN (
        SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
    ) p ON p.ticket_id = e2.ticket_id
    WHERE e2.kind IN (?, ?) AND e2.at > COALESCE(p.pushed_at, '')
    GROUP BY e2.ticket_id
) latest ON latest.ticket_id = e.ticket_id AND latest.id = e.id;
