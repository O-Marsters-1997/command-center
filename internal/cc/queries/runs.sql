-- name: InsertRunSkeleton :one
INSERT INTO runs (ticket_id, kind, baseline_sha, prompt_hash) VALUES ($1, $2, $3, $4) RETURNING id;

-- name: RecordSpawn :exec
UPDATE runs SET pgid = $1, proc_started_at = $2, log_path = $3 WHERE id = $4;

-- name: RecordDisposition :exec
UPDATE runs SET outcome = $1, exit_code = $2, ended_at = $3,
  tokens_in = $4, tokens_out = $5, turns = $6, duration_ms = $7, cost_usd = $8,
  tool_calls = $9, tool_failures = $10, model = $11, metrics_settled = $12
WHERE id = $13;

-- name: RunsAwaitingMetricsBackfill :many
SELECT id, log_path FROM runs WHERE log_path IS NOT NULL AND metrics_settled IS NULL;

-- name: BackfillRunMetrics :exec
UPDATE runs SET tokens_in = $1, tokens_out = $2, turns = $3, duration_ms = $4, cost_usd = $5,
  tool_calls = $6, tool_failures = $7, model = $8, metrics_settled = $9
WHERE id = $10;

-- name: InsertCutFailedRun :one
INSERT INTO runs (ticket_id, kind, prompt_hash, outcome, ended_at)
VALUES ($1, 'agent', $2, $3, $4) RETURNING id;

-- name: PendingRunsAwaitingDisposition :many
SELECT id, ticket_id, pgid, proc_started_at, baseline_sha, log_path FROM runs
WHERE pgid IS NOT NULL AND outcome IS NULL;

-- name: LatestRunsByTicket :many
SELECT r.id, r.ticket_id, r.pgid, r.proc_started_at, r.baseline_sha, r.log_path,
       r.outcome, r.exit_code, r.ended_at, r.prompt_hash, r.kind
FROM runs r
JOIN (SELECT ticket_id, MAX(id) AS id FROM runs GROUP BY ticket_id) latest
  ON latest.ticket_id = r.ticket_id AND latest.id = r.id;

-- name: QueueVerbIntent :exec
INSERT INTO intents (at, ticket_id, verb) VALUES ($1, $2, $3);

-- name: QueueVerbIntentWithPayload :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES ($1, $2, $3, $4);

-- name: PendingVerbIntents :many
SELECT id, ticket_id, payload FROM intents WHERE verb = $1 AND consumed_at IS NULL ORDER BY id;

-- name: PendingIntentsByTicket :many
SELECT ticket_id, verb FROM intents WHERE consumed_at IS NULL ORDER BY id;

-- name: ConsumeVerbIntent :exec
UPDATE intents SET consumed_at = $1 WHERE id = $2;

-- name: ActiveLaunchHashes :many
SELECT lm.ticket_id, lm.prompt_hash FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
WHERE l.state = 'active';

-- name: RunIDsForTicket :many
SELECT id FROM runs WHERE ticket_id = $1 ORDER BY id;

-- name: LatestRunLog :one
SELECT log_path, ended_at FROM runs WHERE ticket_id = $1 ORDER BY id DESC LIMIT 1;
