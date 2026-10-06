-- name: RecordReading :exec
INSERT INTO utilization_readings (at, "window", utilization, resets_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (at, "window") DO NOTHING;

-- name: LatestReadingsFull :many
SELECT DISTINCT ON ("window") "window", at, utilization, resets_at
FROM utilization_readings
ORDER BY "window", at DESC;
