-- name: QueueLaunchIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES ($1, $2, 'launch', $3);

-- name: PendingLaunchIntents :many
SELECT id, ticket_id, payload FROM intents WHERE verb = 'launch' AND consumed_at IS NULL ORDER BY id;

-- name: InsertLaunch :one
INSERT INTO launches (created_at, state) VALUES ($1, 'active') RETURNING id;

-- name: InsertLaunchMember :exec
INSERT INTO launch_members (launch_id, ticket_id, prompt_hash) VALUES ($1, $2, $3);

-- name: ConsumeLaunchIntent :exec
UPDATE intents SET consumed_at = $1 WHERE id = $2;

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

-- name: CancelActiveLaunches :exec
UPDATE launches SET state = 'cancelled' WHERE id IN (
    SELECT lm.launch_id FROM launch_members lm
    JOIN launches l ON l.id = lm.launch_id
    WHERE l.state = 'active' AND lm.ticket_id = $1
);
