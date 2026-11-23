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
       COALESCE(SUM((SELECT COALESCE(SUM(v::int), 0) FROM jsonb_each_text(COALESCE(r.metrics->'tools', '{}'::jsonb)) AS e(k, v))), 0)::bigint AS tool_calls
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.narg(repo)::text IS NULL OR t.repo = sqlc.narg(repo))
  AND (sqlc.narg(feature)::text IS NULL OR t.feature = sqlc.narg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY t.url, t.title, t.repo, t.feature, m.merged_at
HAVING MIN(r.proc_started_at) IS NOT NULL
ORDER BY m.merged_at, t.url;
