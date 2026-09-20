-- name: RunInsights :many
-- Every disposed run counts, tickets.withdrawn_at included (docs/adr/0015).
WITH days AS (
    SELECT generate_series(sqlc.arg(since)::date, sqlc.arg(until)::date, interval '1 day')::date AS day
), matched AS (
    SELECT r.id, r.tokens_in, r.tokens_out, r.metrics_settled,
           (r.ended_at AT TIME ZONE sqlc.arg(timezone)::text)::date AS day
    FROM runs r
    JOIN tickets t ON t.url = r.ticket_id
    WHERE r.ended_at IS NOT NULL
      AND (sqlc.arg(repo)::text = '' OR t.repo = sqlc.arg(repo))
      AND (sqlc.arg(feature)::text = '' OR t.feature = sqlc.arg(feature))
)
SELECT d.day AS day,
       COALESCE(SUM(m.tokens_in), 0)::bigint AS tokens_in,
       COALESCE(SUM(m.tokens_out), 0)::bigint AS tokens_out,
       COUNT(m.id) AS runs,
       COUNT(*) FILTER (WHERE m.metrics_settled = false) AS unsettled
FROM days d
LEFT JOIN matched m ON m.day = d.day
GROUP BY d.day
ORDER BY d.day;
