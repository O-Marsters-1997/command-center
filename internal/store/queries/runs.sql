-- name: InsertRunSkeleton :one
INSERT INTO runs (ticket_id, kind, baseline_sha, prompt_hash, session_id, message, model, effort)
VALUES (
  sqlc.arg(ticket_id), sqlc.arg(kind), sqlc.arg(baseline_sha), sqlc.arg(prompt_hash),
  CASE WHEN sqlc.arg(new_session)::boolean THEN gen_random_uuid() ELSE sqlc.narg(session_id)::text::uuid END,
  sqlc.narg(message), sqlc.narg(model), sqlc.narg(effort)
) RETURNING id, COALESCE(session_id::text, '')::text AS session_id;

-- name: LatestSessionID :one
SELECT session_id::text AS session_id FROM runs
WHERE ticket_id = sqlc.arg(ticket_id) AND kind IN ('agent', 'follow_up') AND session_id IS NOT NULL ORDER BY id DESC LIMIT 1;

-- name: RecordSpawn :execrows
UPDATE runs SET pgid = sqlc.arg(pgid), proc_started_at = sqlc.arg(proc_started_at), log_path = sqlc.arg(log_path)
WHERE id = sqlc.arg(id);

-- name: RecordDisposition :execrows
UPDATE runs SET outcome = sqlc.arg(outcome), exit_code = sqlc.arg(exit_code), ended_at = sqlc.arg(ended_at),
    cost_usd = sqlc.arg(cost_usd), last_message = sqlc.narg(last_message),
    metrics = sqlc.narg(metrics)
WHERE id = sqlc.arg(id) AND outcome IS NULL;

-- name: TicketActivities :many
SELECT e.ticket_id::text AS ticket_url, MAX(e.at)::timestamptz AS at,
  COALESCE((
    SELECT r.last_message FROM runs r
    WHERE r.ticket_id = e.ticket_id AND r.last_message IS NOT NULL ORDER BY r.id DESC LIMIT 1
  ), '')::text AS last_message
FROM events e
WHERE e.ticket_id IS NOT NULL
GROUP BY e.ticket_id;

-- name: RecordQuestion :execrows
UPDATE runs SET question = sqlc.arg(question) WHERE id = sqlc.arg(id);

-- name: InsertCutFailedRun :one
INSERT INTO runs (ticket_id, kind, prompt_hash, outcome, ended_at)
VALUES (sqlc.arg(ticket_id), 'agent', sqlc.arg(prompt_hash), sqlc.arg(outcome), sqlc.arg(ended_at)) RETURNING id;

-- name: PendingRunsAwaitingDisposition :many
SELECT id, ticket_id, kind, pgid, proc_started_at, baseline_sha, log_path FROM runs
WHERE pgid IS NOT NULL AND outcome IS NULL;

-- name: ListLatestRunsByTicket :many
SELECT DISTINCT ON (ticket_id) id, ticket_id::text AS ticket_id, pgid, proc_started_at, baseline_sha, log_path,
       outcome, exit_code, ended_at, prompt_hash, kind, question
FROM runs
WHERE ticket_id IS NOT NULL
ORDER BY ticket_id, id DESC;

-- name: QueueVerbIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload)
VALUES (sqlc.arg(at), sqlc.arg(ticket_id), sqlc.arg(verb), sqlc.narg(payload));

-- name: PendingVerbIntents :many
SELECT id, ticket_id, COALESCE(payload, 'null'::jsonb) AS payload FROM intents
WHERE verb = sqlc.arg(verb) AND consumed_at IS NULL ORDER BY id;

-- name: PendingIntentsByTicket :many
SELECT ticket_id, verb FROM intents WHERE consumed_at IS NULL AND verb <> 'edit_ticket' ORDER BY id;

-- name: ConsumeVerbIntent :execrows
UPDATE intents SET consumed_at = sqlc.arg(consumed_at) WHERE id = sqlc.arg(id) AND consumed_at IS NULL;

-- name: ActiveLaunchHashes :many
SELECT lm.ticket_id, lm.prompt_hash FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
WHERE l.state = 'active';

-- name: RunIDsForTicket :many
SELECT id FROM runs WHERE ticket_id = sqlc.arg(ticket_id) ORDER BY id;

-- name: LatestSessionRuns :many
SELECT runs.id,
  COALESCE(runs.log_path, '')::text AS log_path,
  COALESCE(runs.message, '')::text AS message,
  COALESCE(runs.question, '')::text AS question
FROM runs
WHERE runs.ticket_id = sqlc.arg(ticket_id) AND runs.kind IN ('agent', 'follow_up') AND runs.session_id = (
  SELECT latest.session_id FROM runs latest
  WHERE latest.ticket_id = sqlc.arg(ticket_id) AND latest.kind IN ('agent', 'follow_up')
  ORDER BY latest.id DESC LIMIT 1
)
ORDER BY runs.id;

-- name: LatestRunLog :one
SELECT log_path, ended_at FROM runs WHERE ticket_id = sqlc.arg(ticket_id) ORDER BY id DESC LIMIT 1;

-- name: RunDetails :one
SELECT COALESCE(session_id::text, '')::text AS session_id,
       COALESCE(message, '')::text AS message,
       COALESCE(outcome, '')::text AS outcome
FROM runs WHERE id = sqlc.arg(id);
