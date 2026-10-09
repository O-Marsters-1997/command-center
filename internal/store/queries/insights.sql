-- name: MergedTicketSpend :many
-- One row per ticket with a pr_merged event in [since, until], weighing every run disposed
-- before that event, plus an equal share of each explore run of a launch the ticket belongs to.
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
),
explore_shares AS (
    SELECT lm.ticket_id, r.ended_at, r.cost_usd / n.members AS share_usd
    FROM launch_members lm
    JOIN (SELECT launch_id, COUNT(*) AS members FROM launch_members GROUP BY launch_id) n
      ON n.launch_id = lm.launch_id
    JOIN runs r ON r.launch_id = lm.launch_id AND r.ended_at IS NOT NULL AND r.cost_usd IS NOT NULL
)
SELECT t.url AS ticket_id, t.title, m.merged_at,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'agent'), 0)::double precision AS agent_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'resolve'), 0)::double precision AS resolve_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'follow_up'), 0)::double precision AS follow_up_usd,
       COALESCE((SELECT SUM(e.share_usd) FROM explore_shares e
                 WHERE e.ticket_id = t.url AND e.ended_at < m.merged_at), 0)::double precision AS explore_usd
FROM tickets t
JOIN merges m ON m.ticket_id = t.url
LEFT JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL AND r.ended_at < m.merged_at
WHERE (sqlc.arg(repo)::text = '' OR t.repo = sqlc.arg(repo))
  AND (sqlc.arg(feature)::text = '' OR t.feature = sqlc.arg(feature))
  AND (m.merged_at AT TIME ZONE sqlc.arg(timezone)::text)::date BETWEEN sqlc.arg(since)::date AND sqlc.arg(until)::date
GROUP BY t.url, t.title, m.merged_at
ORDER BY m.merged_at;

-- name: BoardTicketSpend :many
-- One row per ticket in scope, weighing every run disposed before its own pr_merged event when
-- merged, or every run disposed so far when still open -- the same weighing MergedTicketSpend
-- uses, generalised past merged-only tickets and off any date window (ADR 12).
WITH merges AS (
    SELECT ticket_id, at AS merged_at FROM events WHERE kind = 'pr_merged'
),
explore_shares AS (
    SELECT lm.ticket_id, r.ended_at, r.cost_usd / n.members AS share_usd
    FROM launch_members lm
    JOIN (SELECT launch_id, COUNT(*) AS members FROM launch_members GROUP BY launch_id) n
      ON n.launch_id = lm.launch_id
    JOIN runs r ON r.launch_id = lm.launch_id AND r.ended_at IS NOT NULL AND r.cost_usd IS NOT NULL
)
SELECT t.url AS ticket_id, (m.merged_at IS NOT NULL)::boolean AS merged,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'agent'), 0)::double precision AS agent_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'resolve'), 0)::double precision AS resolve_usd,
       COALESCE(SUM(r.cost_usd) FILTER (WHERE r.kind = 'follow_up'), 0)::double precision AS follow_up_usd,
       COALESCE((SELECT SUM(e.share_usd) FROM explore_shares e
                 WHERE e.ticket_id = t.url AND (m.merged_at IS NULL OR e.ended_at < m.merged_at)), 0)::double precision AS explore_usd
FROM tickets t
LEFT JOIN merges m ON m.ticket_id = t.url
LEFT JOIN runs r ON r.ticket_id = t.url AND r.ended_at IS NOT NULL
    AND (m.merged_at IS NULL OR r.ended_at < m.merged_at)
WHERE (sqlc.arg(repo)::text = '' OR t.repo = sqlc.arg(repo))
  AND (sqlc.arg(feature)::text = '' OR t.feature = sqlc.arg(feature))
GROUP BY t.url, m.merged_at;

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
