-- name: BoardTicketSpend :many
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
)
SELECT t.url AS ticket_id, (m.merged_at IS NOT NULL)::boolean AS merged,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'agent'), 0)::double precision AS agent_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'follow_up'), 0)::double precision AS follow_up_usd
FROM tickets t
LEFT JOIN merges m ON m.ticket_id = t.url
LEFT JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL
    AND (m.merged_at IS NULL OR r.ended_at < m.merged_at)
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
GROUP BY t.url, m.merged_at;

-- name: MergedTicketFacts :many
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
)
SELECT t.url AS ticket_id, t.title, t.repo, t.feature, m.merged_at,
       MIN(r.proc_started_at)::timestamptz AS first_run_at,
       COALESCE(SUM(r.cost_usd), 0)::double precision AS weight_usd,
       COALESCE(SUM(COALESCE((r.metrics->>'input')::bigint, 0) + COALESCE((r.metrics->>'cache_write')::bigint, 0)
                    + COALESCE((r.metrics->>'output')::bigint, 0)), 0)::bigint AS tokens,
       COALESCE(SUM((SELECT COALESCE(SUM(v::int), 0) FROM jsonb_each_text(COALESCE(r.metrics->'tools', '{}'::jsonb)) AS e(k, v))), 0)::bigint AS tool_calls,
       (SELECT COUNT(*) FROM events e WHERE e.ticket_id = t.url AND e.at < m.merged_at
        AND e.kind IN ('refreshed', 'restacked', 'refresh_conflicted'))::bigint AS refreshes,
       (SELECT COUNT(*) FROM events e WHERE e.ticket_id = t.url AND e.at < m.merged_at
        AND e.kind = 'refresh_conflicted')::bigint AS conflicted
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY t.url, t.title, t.repo, t.feature, m.merged_at
HAVING MIN(r.proc_started_at) IS NOT NULL
ORDER BY m.merged_at, t.url;

-- name: MergedTicketPushes :many
WITH merges AS (
    SELECT ticket_id, MIN(at) AS merged_at FROM events WHERE kind = 'pr_merged' GROUP BY ticket_id
),
spans AS (
    SELECT p.ticket_id, p.id, p.pushed_at,
           COALESCE(LEAD(p.pushed_at) OVER (PARTITION BY p.ticket_id ORDER BY p.pushed_at, p.id), 'infinity'::timestamptz) AS next_at
    FROM pushes p
)
SELECT s.ticket_id, s.pushed_at,
       (CASE
           WHEN EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = s.ticket_id AND e.kind = 'verdict_transition'
                        AND e.detail LIKE 'review_me:%' AND e.at >= s.pushed_at AND e.at < s.next_at) THEN 'green'
           WHEN EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = s.ticket_id AND e.kind = 'verdict_transition'
                        AND e.detail LIKE 'ci_failed:%' AND e.at >= s.pushed_at AND e.at < s.next_at) THEN 'red'
           ELSE 'pending'
       END)::text AS verdict
FROM spans s
JOIN tickets t ON t.url = s.ticket_id
JOIN merges m ON m.ticket_id = s.ticket_id AND s.pushed_at < m.merged_at
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
ORDER BY s.ticket_id, s.pushed_at, s.id;

-- name: ParkEvents :many
SELECT t.url AS ticket_id, t.repo, e.at, COALESCE(e.detail, '')::text AS detail,
       COALESCE((SELECT SUM(COALESCE((r.metrics->>'input')::bigint, 0) + COALESCE((r.metrics->>'cache_write')::bigint, 0)
                            + COALESCE((r.metrics->>'output')::bigint, 0))
                 FROM runs r WHERE r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at <= e.at), 0)::bigint AS tokens
FROM events e
JOIN tickets t ON t.url = e.ticket_id
WHERE e.kind = 'parked'
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (e.at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
ORDER BY e.at, e.id;

-- name: ChecksLandedEvents :many
SELECT t.url AS ticket_id, t.repo, e.at, COALESCE(e.detail, '')::text AS detail
FROM events e
JOIN tickets t ON t.url = e.ticket_id
WHERE e.kind = 'checks_landed'
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (e.at AT TIME ZONE sqlc.arg(timezone)::text)::date <= sqlc.arg(until)::date
ORDER BY e.at, e.id;

-- name: CIFixRuns :many
SELECT t.url AS ticket_id, r.proc_started_at::timestamptz AS launched_at,
       (COALESCE((r.metrics->>'input')::bigint, 0) + COALESCE((r.metrics->>'cache_write')::bigint, 0)
        + COALESCE((r.metrics->>'output')::bigint, 0))::bigint AS tokens
FROM runs r
JOIN tickets t ON t.url = r.ticket_id
WHERE r.cause = 'ci_failed' AND r.proc_started_at IS NOT NULL
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (r.proc_started_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
ORDER BY r.proc_started_at, r.id;

-- name: AppendEventReturningID :one
INSERT INTO events (at, ticket_id, kind, detail)
VALUES (now(), sqlc.arg(ticket_id), sqlc.arg(kind), sqlc.arg(detail))
RETURNING id;

-- name: InsertConflictPaths :exec
INSERT INTO conflict_paths (event_id, path, generated)
SELECT sqlc.arg(event_id), p.path, p.generated
FROM jsonb_to_recordset(sqlc.arg(paths)::jsonb) AS p(path text, generated boolean);

-- name: TicketInsightRuns :many
SELECT r.id, r.step, r.kind, r.proc_started_at, r.ended_at,
       COALESCE(r.cost_usd, 0)::double precision AS cost_usd,
       COALESCE(r.metrics, '{}'::jsonb) AS metrics, COALESCE(r.outcome, '')::text AS outcome
FROM runs r
WHERE r.ticket_id = sqlc.arg(ticket) AND r.ended_at IS NOT NULL
  AND r.ended_at < COALESCE((SELECT MIN(e.at) FROM events e WHERE e.ticket_id = r.ticket_id AND e.kind = 'pr_merged'), 'infinity'::timestamptz)
ORDER BY r.id;

-- name: TicketInsightPushes :many
WITH spans AS (
    SELECT p.id, p.pushed_at,
           COALESCE(LEAD(p.pushed_at) OVER (ORDER BY p.pushed_at, p.id), 'infinity'::timestamptz) AS next_at
    FROM pushes p
    WHERE p.ticket_id = sqlc.arg(ticket)
)
SELECT s.pushed_at,
       (CASE
           WHEN EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = sqlc.arg(ticket) AND e.kind = 'verdict_transition'
                        AND e.detail LIKE 'review_me:%' AND e.at >= s.pushed_at AND e.at < s.next_at) THEN 'green'
           WHEN EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = sqlc.arg(ticket) AND e.kind = 'verdict_transition'
                        AND e.detail LIKE 'ci_failed:%' AND e.at >= s.pushed_at AND e.at < s.next_at) THEN 'red'
           ELSE 'pending'
       END)::text AS verdict
FROM spans s
ORDER BY s.pushed_at, s.id;

-- name: TicketMergedAt :many
SELECT at FROM events WHERE ticket_id = sqlc.arg(ticket) AND kind = 'pr_merged' ORDER BY at LIMIT 1;

-- name: ConflictPathEvents :many
SELECT e.id AS event_id, t.url AS ticket_id, t.repo, t.feature, e.at, c.path, c.generated
FROM events e
JOIN conflict_paths c ON c.event_id = e.id
JOIN tickets t ON t.url = e.ticket_id
WHERE e.kind = 'refresh_conflicted'
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (e.at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
ORDER BY e.at, e.id, c.path;

-- name: ConflictOpenSpans :many
SELECT t.url AS ticket_id, t.feature, MIN(r.proc_started_at)::timestamptz AS from_at,
       COALESCE((SELECT MIN(e.at) FROM events e WHERE e.ticket_id = t.url AND e.kind = 'pr_merged'), t.withdrawn_at) AS to_at
FROM tickets t
JOIN runs r ON r.ticket_id = t.url AND r.proc_started_at IS NOT NULL
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND EXISTS (SELECT 1 FROM events e JOIN conflict_paths c ON c.event_id = e.id
              WHERE e.ticket_id = t.url AND e.kind = 'refresh_conflicted'
                AND (e.at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date)
GROUP BY t.url, t.feature, t.withdrawn_at
ORDER BY t.url;
