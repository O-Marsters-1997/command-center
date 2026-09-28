-- name: MergedTicketSpend :many
-- One row per ticket with a pr_merged event in [since, until], weighing every run disposed
-- before that event.
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
)
SELECT t.url AS ticket_id, t.title, m.merged_at,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'agent'), 0)::double precision AS agent_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'resolve'), 0)::double precision AS resolve_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'follow_up'), 0)::double precision AS follow_up_usd
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
LEFT JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.arg(repo)::text = '' OR t.repo = sqlc.arg(repo))
  AND (sqlc.arg(feature)::text = '' OR t.feature = sqlc.arg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY t.url, t.title, m.merged_at
ORDER BY m.merged_at;

-- name: WithdrawnTicketWaste :one
-- Every disposed run belonging to a ticket withdrawn without ever merging.
SELECT COALESCE(SUM(r.cost_usd), 0)::double precision AS waste_usd
FROM tickets t
JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL
WHERE t.withdrawn_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM events e WHERE e.ticket_id = t.url AND e.kind = 'pr_merged')
  AND (sqlc.arg(repo)::text = '' OR t.repo = sqlc.arg(repo))
  AND (sqlc.arg(feature)::text = '' OR t.feature = sqlc.arg(feature))
  AND (t.withdrawn_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date;
