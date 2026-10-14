-- name: QueueLaunchIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES ($1, $2, 'launch', $3);

-- name: ClaimLaunchIntents :many
UPDATE intents SET consumed_at = sqlc.arg(at)
WHERE verb = 'launch' AND consumed_at IS NULL
RETURNING id, ticket_id, payload;

-- name: InsertLaunch :one
INSERT INTO launches (created_at, state) VALUES ($1, 'active') RETURNING id;

-- name: InsertLaunchMember :exec
INSERT INTO launch_members (launch_id, ticket_id, prompt_hash) VALUES ($1, $2, $3);

-- name: InsertLaunchEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES ($1, NULL, 'launch', $2);

-- name: LaunchMemberships :many
SELECT lm.ticket_id, lm.launch_id, l.state, lm.prompt_hash,
       COUNT(*) OVER (PARTITION BY lm.launch_id) AS members
FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
WHERE l.state IN ('active', 'cancelled');

-- name: ActiveLaunchMemberCount :one
SELECT COUNT(*) FROM launch_members WHERE launch_id IN (
    SELECT lm.launch_id FROM launch_members lm
    JOIN launches l ON l.id = lm.launch_id
    WHERE l.state = 'active' AND lm.ticket_id = $1
);

-- name: CancelActiveLaunches :execrows
UPDATE launches SET state = 'cancelled' WHERE id IN (
    SELECT lm.launch_id FROM launch_members lm
    JOIN launches l ON l.id = lm.launch_id
    WHERE l.state = 'active' AND lm.ticket_id = $1
);

-- name: FeatureFirstLaunch :one
SELECT l.created_at
FROM launches l
JOIN launch_members lm ON lm.launch_id = l.id
JOIN tickets t ON t.url = lm.ticket_id
WHERE t.feature = $1 AND l.state <> 'cancelled'
ORDER BY l.created_at
LIMIT 1;
