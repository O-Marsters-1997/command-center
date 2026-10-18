-- name: MergedTicketSpend :many
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
)
SELECT t.url AS ticket_id, t.title, t.feature, m.merged_at,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'agent'), 0)::double precision AS agent_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'follow_up'), 0)::double precision AS follow_up_usd
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
LEFT JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY t.url, t.title, t.feature, m.merged_at
ORDER BY m.merged_at;

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

-- name: WithdrawnTicketWaste :one
SELECT COALESCE(SUM(r.cost_usd), 0)::double precision AS waste_usd
FROM tickets t
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL
WHERE t.withdrawn_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = t.url AND e.kind = 'pr_merged')
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (t.withdrawn_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date;

-- name: MergedTicketRuns :many
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
)
SELECT t.url AS ticket_id, m.merged_at, r.step, r.workflow, r.workflow_hash,
       COALESCE(r.cost_usd, 0)::double precision AS cost_usd,
       (r.metrics IS NOT NULL)::boolean AS measured, COALESCE(r.metrics, '{}'::jsonb) AS metrics
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
ORDER BY m.merged_at, t.url, r.id;

-- name: MergedTicketRework :many
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
), opens AS (
    SELECT ticket_id, MIN(at) AS opened_at FROM events WHERE kind = 'pr_opened' GROUP BY ticket_id
)
SELECT t.url AS ticket_id,
       (SELECT COUNT(*) FROM runs r WHERE r.ticket_id = t.url AND r.kind = 'follow_up'
            AND r.ended_at > o.opened_at AND r.ended_at < m.merged_at)::bigint AS follow_ups,
       (SELECT COUNT(*) FROM runs r WHERE r.ticket_id = t.url AND r.kind = 'agent'
            AND r.ended_at > o.opened_at AND r.ended_at < m.merged_at)::bigint AS re_runs,
       (SELECT COUNT(*) FROM events e WHERE e.ticket_id = t.url AND e.kind = 'verdict_transition'
            AND e.detail LIKE 'ci_failed:%' AND e.at > o.opened_at AND e.at < m.merged_at)::bigint AS red_ci
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
LEFT JOIN opens o ON o.ticket_id = t.url
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date;

-- name: WithdrawnWorkflowTickets :many
SELECT r.workflow_hash, COUNT(DISTINCT t.url)::bigint AS withdrawn
FROM tickets t
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL
WHERE t.withdrawn_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = t.url AND e.kind = 'pr_merged')
  AND r.id = (SELECT MIN(f.id) FROM runs f WHERE f.ticket_id = t.url)
  AND (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (t.withdrawn_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY r.workflow_hash;
