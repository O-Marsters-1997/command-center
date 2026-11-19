-- name: QueueLaunchIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES (sqlc.arg(at), sqlc.arg(ticket_id), 'launch', sqlc.arg(payload));

-- name: ClaimLaunchIntents :many
UPDATE intents SET consumed_at = sqlc.arg(at)
WHERE verb = 'launch' AND consumed_at IS NULL
RETURNING id, ticket_id, payload;

-- name: InsertLaunch :one
INSERT INTO launches (created_at, state, context, model, effort, workflow)
VALUES (sqlc.arg(created_at), 'active', sqlc.arg(context), sqlc.narg(model), sqlc.narg(effort),
        COALESCE(NULLIF(sqlc.arg(workflow)::text, ''), 'v0')) RETURNING id;

-- name: InsertLaunchMembers :exec
INSERT INTO launch_members (launch_id, ticket_id, prompt_hash)
SELECT sqlc.arg(launch_id)::bigint, m.ticket_id, m.prompt_hash
FROM jsonb_to_recordset(sqlc.arg(members)::jsonb) AS m(ticket_id text, prompt_hash text);

-- name: InsertLaunchEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES (sqlc.arg(at), NULL, 'launch', sqlc.arg(detail));

-- name: ListLaunchMemberships :many
SELECT lm.ticket_id, lm.launch_id, l.state, lm.prompt_hash, l.context, l.model, l.effort, l.workflow,
       COUNT(*) OVER (PARTITION BY lm.launch_id) AS members
FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
WHERE l.state IN ('active', 'cancelled')
ORDER BY (l.state = 'active') DESC, lm.launch_id DESC, lm.ticket_id
LIMIT sqlc.arg(max_rows);

-- name: ArmedDependent :one
SELECT lm.ticket_id
FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
JOIN tickets t ON t.url = lm.ticket_id
WHERE l.state = 'active' AND t.blocked_by @> to_jsonb(sqlc.arg(ticket_id)::text)
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.ticket_id = lm.ticket_id)
ORDER BY lm.ticket_id
LIMIT 1;

-- name: DisarmMember :exec
DELETE FROM launch_members lm USING launches l
WHERE l.id = lm.launch_id AND l.state = 'active' AND lm.ticket_id = sqlc.arg(ticket_id);

-- name: CancelEmptyLaunches :exec
UPDATE launches SET state = 'cancelled'
WHERE state = 'active' AND NOT EXISTS (SELECT 1 FROM launch_members WHERE launch_id = launches.id);

-- name: FeatureFirstLaunch :one
SELECT l.created_at
FROM launches l
JOIN launch_members lm ON lm.launch_id = l.id
JOIN tickets t ON t.url = lm.ticket_id
WHERE t.feature = sqlc.arg(feature) AND l.state <> 'cancelled'
ORDER BY l.created_at
LIMIT 1;
