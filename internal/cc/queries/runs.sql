-- name: InsertRunSkeleton :execresult
INSERT INTO runs (ticket_id, kind, baseline_sha, prompt_hash) VALUES (?, ?, ?, ?);

-- name: RecordSpawn :exec
UPDATE runs SET pgid = ?, proc_started_at = ?, log_path = ? WHERE id = ?;

-- name: RecordDisposition :exec
UPDATE runs SET outcome = ?, exit_code = ?, ended_at = ? WHERE id = ?;

-- name: InsertCutFailedRun :execresult
INSERT INTO runs (ticket_id, kind, prompt_hash, outcome, ended_at) VALUES (?, 'agent', ?, ?, ?);

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
INSERT INTO intents (at, ticket_id, verb) VALUES (?, ?, ?);

-- name: PendingVerbIntents :many
SELECT id, ticket_id FROM intents WHERE verb = ? AND consumed_at IS NULL ORDER BY id;

-- name: PendingIntentsByTicket :many
SELECT ticket_id, verb FROM intents WHERE consumed_at IS NULL ORDER BY id;

-- name: ConsumeVerbIntent :exec
UPDATE intents SET consumed_at = ? WHERE id = ?;

-- name: ActiveLaunchHashes :many
SELECT lm.ticket_id, lm.prompt_hash FROM launch_members lm
JOIN launches l ON l.id = lm.launch_id
WHERE l.state = 'active';

-- name: RunIDsForTicket :many
SELECT id FROM runs WHERE ticket_id = ? ORDER BY id;

-- name: LatestRunLog :one
SELECT log_path, ended_at FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1;
