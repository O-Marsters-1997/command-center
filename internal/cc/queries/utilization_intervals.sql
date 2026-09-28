-- name: RecordInterval :exec
INSERT INTO utilization_intervals (
    "window", start_at, end_at, utilization_start, utilization_end, weight_usd
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT ("window", start_at) DO NOTHING;

-- name: IntervalsSince :many
SELECT "window", start_at, end_at, utilization_start, utilization_end, weight_usd
FROM utilization_intervals
WHERE end_at >= $1
ORDER BY "window", start_at;

-- name: CCCostSince :one
SELECT
    COALESCE(SUM(cost_usd) FILTER (WHERE ended_at >= sqlc.arg(five_hour_since)), 0)::double precision AS five_hour_usd,
    COALESCE(SUM(cost_usd) FILTER (WHERE ended_at >= sqlc.arg(seven_day_since)), 0)::double precision AS seven_day_usd
FROM runs
WHERE cost_usd IS NOT NULL;
