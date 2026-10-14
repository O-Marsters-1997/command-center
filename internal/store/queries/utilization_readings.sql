-- name: RecordReadings :exec
INSERT INTO utilization_readings (at, "window", utilization, resets_at)
SELECT r.at, r."window", r.utilization, r.resets_at
FROM jsonb_to_recordset(sqlc.arg(readings)::jsonb) AS r(
    at timestamptz, "window" text, utilization double precision, resets_at timestamptz
)
ON CONFLICT (at, "window") DO NOTHING;

-- name: LatestReadingsFull :many
SELECT DISTINCT ON ("window") "window", at, utilization, resets_at
FROM utilization_readings
ORDER BY "window", at DESC;

-- name: PruneReadings :execrows
DELETE FROM utilization_readings WHERE id IN (
    SELECT r.id FROM utilization_readings r
    WHERE r.at < sqlc.arg(before)
      AND EXISTS (
          SELECT 1 FROM utilization_readings newer
          WHERE newer."window" = r."window" AND newer.at > r.at
      )
    LIMIT sqlc.arg(batch)
);
