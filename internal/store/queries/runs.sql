-- name: InsertRunSkeleton :one
INSERT INTO runs (ticket_id, kind, baseline_sha, prompt_hash, step, workflow, workflow_hash, session_id, message, model, effort, cause)
VALUES (
  sqlc.arg(ticket_id), sqlc.arg(kind), sqlc.arg(baseline_sha), sqlc.arg(prompt_hash),
  sqlc.arg(step), sqlc.arg(workflow), sqlc.arg(workflow_hash),
  CASE WHEN sqlc.arg(new_session)::boolean THEN gen_random_uuid() ELSE sqlc.narg(session_id)::text::uuid END,
  sqlc.narg(message), sqlc.narg(model), sqlc.narg(effort), sqlc.narg(cause)
) RETURNING id, COALESCE(session_id::text, '')::text AS session_id;

-- name: RecordSpawn :execrows
UPDATE runs SET pgid = sqlc.arg(pgid), proc_started_at = sqlc.arg(proc_started_at), log_path = sqlc.arg(log_path)
WHERE id = sqlc.arg(id);

-- name: RecordPrompt :execrows
UPDATE runs SET prompt = sqlc.arg(prompt) WHERE id = sqlc.arg(id);

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
INSERT INTO runs (ticket_id, kind, prompt_hash, step, workflow, workflow_hash, outcome, ended_at)
VALUES (sqlc.arg(ticket_id), 'agent', sqlc.arg(prompt_hash), sqlc.arg(step), sqlc.arg(workflow), sqlc.arg(workflow_hash),
  sqlc.arg(outcome), sqlc.arg(ended_at)) RETURNING id;

-- name: PendingRunsAwaitingDisposition :many
SELECT id, ticket_id, kind, pgid, proc_started_at, baseline_sha, log_path, step, workflow_hash FROM runs
WHERE pgid IS NOT NULL AND outcome IS NULL;

-- name: ListPassRuns :many
SELECT id, ticket_id::text AS ticket_id, step, outcome,
  CASE WHEN kind IN ('agent', 'follow_up') THEN COALESCE(session_id::text, '') ELSE '' END::text AS session_id
FROM runs
WHERE ticket_id IS NOT NULL AND kind <> 'verify' ORDER BY id;

-- name: RunBaseline :one
SELECT COALESCE(baseline_sha, '')::text AS baseline_sha FROM runs WHERE id = sqlc.arg(id);

-- name: ListLatestRunsByTicket :many
SELECT DISTINCT ON (ticket_id) id, ticket_id::text AS ticket_id, pgid, proc_started_at, baseline_sha, log_path,
       outcome, exit_code, ended_at, prompt_hash, kind, question, step, workflow_hash, cause,
       COALESCE(metrics, 'null'::jsonb) AS metrics
FROM runs
WHERE ticket_id IS NOT NULL AND kind <> 'verify'
ORDER BY ticket_id, id DESC;

-- name: ListLatestVerifyRuns :many
SELECT DISTINCT ON (ticket_id) id, ticket_id::text AS ticket_id, pgid, proc_started_at, baseline_sha, log_path, outcome
FROM runs
WHERE ticket_id IS NOT NULL AND kind = 'verify' AND (pgid IS NOT NULL OR outcome IS NOT NULL)
ORDER BY ticket_id, id DESC;

-- name: AutoHistories :many
SELECT r.ticket_id::text AS ticket_id, COUNT(*)::int AS rounds,
  (ARRAY_AGG(COALESCE(r.baseline_sha, '') ORDER BY r.id DESC))[1]::text AS last_tip
FROM runs r
WHERE r.cause IS NOT NULL
  AND COALESCE(r.proc_started_at, r.ended_at, 'infinity'::timestamptz) > COALESCE((
    SELECT MAX(e.at) FROM events e
    WHERE e.ticket_id = r.ticket_id AND e.kind = 'verdict_transition' AND e.detail LIKE 'review_me:%'
  ), '-infinity'::timestamptz)
GROUP BY r.ticket_id;

-- name: QueueVerbIntent :one
INSERT INTO intents (at, ticket_id, verb, payload, hold_until, queued_by)
VALUES (
    sqlc.arg(at), sqlc.arg(ticket_id), sqlc.arg(verb), sqlc.narg(payload), sqlc.narg(hold_until), sqlc.arg(queued_by)
)
RETURNING id;

-- name: WithdrawHeldIntent :execrows
UPDATE intents SET outcome = 'withdrawn', consumed_at = sqlc.arg(now), finished_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND consumed_at IS NULL AND hold_until > sqlc.arg(now);

-- name: IntentHistory :many
SELECT id, ticket_id, verb, queued_by, at, hold_until, consumed_at, finished_at,
  COALESCE(outcome, '')::text AS outcome, COALESCE(detail, '')::text AS detail, run_id,
  COALESCE(ticket_state, '')::text AS ticket_state
FROM intents
WHERE verb <> 'edit_ticket' AND (at >= sqlc.arg(since) OR finished_at IS NULL)
ORDER BY id DESC;

-- name: LatestRowIntents :many
SELECT DISTINCT ON (ticket_id) id, ticket_id, verb, queued_by, at, hold_until, consumed_at, finished_at,
  COALESCE(outcome, '')::text AS outcome, COALESCE(detail, '')::text AS detail, run_id,
  COALESCE(ticket_state, '')::text AS ticket_state
FROM intents
WHERE verb = ANY(string_to_array(sqlc.arg(verbs)::text, ','))
ORDER BY ticket_id, id DESC;

-- name: IntentConsumed :one
SELECT (consumed_at IS NOT NULL)::bool AS consumed FROM intents WHERE id = sqlc.arg(id);

-- name: PendingVerbIntents :many
SELECT id, ticket_id, COALESCE(payload, 'null'::jsonb) AS payload, hold_until FROM intents
WHERE verb = sqlc.arg(verb) AND consumed_at IS NULL ORDER BY id;

-- name: PendingIntents :many
SELECT id, ticket_id, verb, COALESCE(payload, 'null'::jsonb) AS payload, hold_until FROM intents
WHERE consumed_at IS NULL AND verb <> 'edit_ticket' ORDER BY id;

-- name: ConsumeVerbIntent :execrows
UPDATE intents SET consumed_at = sqlc.arg(consumed_at), outcome = sqlc.narg(outcome), detail = sqlc.narg(detail),
  finished_at = sqlc.narg(finished_at), run_id = sqlc.narg(run_id), ticket_state = sqlc.narg(ticket_state)
WHERE id = sqlc.arg(id) AND consumed_at IS NULL;

-- name: RunIDsForTicket :many
SELECT id FROM runs WHERE ticket_id = sqlc.arg(ticket_id) ORDER BY id;

-- name: LatestSessionRuns :many
SELECT runs.id,
  COALESCE(runs.log_path, '')::text AS log_path,
  COALESCE(runs.message, '')::text AS message,
  COALESCE(runs.question, '')::text AS question,
  COALESCE(runs.prompt, '')::text AS prompt,
  runs.step,
  COALESCE(runs.model, '')::text AS model,
  COALESCE(runs.outcome, '')::text AS outcome,
  runs.cost_usd,
  COALESCE(runs.metrics, 'null'::jsonb) AS metrics
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
