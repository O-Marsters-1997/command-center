-- name: RecordIntervals :exec
INSERT INTO utilization_intervals (
    "window", start_at, end_at, utilization_start, utilization_end, weight_usd
)
SELECT i."window", i.start_at, i.end_at, i.utilization_start, i.utilization_end, i.weight_usd
FROM jsonb_to_recordset(sqlc.arg(intervals)::jsonb) AS i(
    "window" text, start_at timestamptz, end_at timestamptz,
    utilization_start double precision, utilization_end double precision, weight_usd double precision
)
ON CONFLICT ("window", start_at) DO NOTHING;

-- name: ListIntervalsSince :many
SELECT "window", start_at, end_at, utilization_start, utilization_end, weight_usd
FROM utilization_intervals
WHERE end_at >= sqlc.arg(since)
ORDER BY "window", start_at;

-- name: CCCostSince :one
SELECT
    COALESCE(SUM(cost_usd) FILTER (WHERE ended_at >= sqlc.arg(five_hour_since)), 0)::double precision AS five_hour_usd,
    COALESCE(SUM(cost_usd) FILTER (WHERE ended_at >= sqlc.arg(seven_day_since)), 0)::double precision AS seven_day_usd
FROM runs
WHERE cost_usd IS NOT NULL;

-- name: PruneIntervals :execrows
DELETE FROM utilization_intervals WHERE id IN (
    SELECT u.id FROM utilization_intervals u WHERE u.end_at < sqlc.arg(before) LIMIT sqlc.arg(batch)
);
