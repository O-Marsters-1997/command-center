-- name: InsertMemorySample :one
INSERT INTO memory_samples (sampled_at, total_bytes, used_bytes)
VALUES (sqlc.arg(sampled_at), sqlc.arg(total_bytes), sqlc.arg(used_bytes))
RETURNING id;

-- name: InsertMemoryProcesses :exec
INSERT INTO memory_processes (sample_id, run_id, pid, name, args, rss_bytes)
SELECT sqlc.arg(sample_id)::bigint, p.run_id, p.pid, p.name, p.args, p.rss_bytes
FROM jsonb_to_recordset(sqlc.arg(procs)::jsonb) AS p(
    run_id bigint, pid integer, name text, args text, rss_bytes bigint
);

-- name: PruneMemorySamples :execrows
DELETE FROM memory_samples WHERE id IN (
    SELECT m.id FROM memory_samples m
    WHERE m.sampled_at < sqlc.arg(before)
    LIMIT sqlc.arg(batch)
);

-- name: MemoryPeaks :many
SELECT DISTINCT ON (floor(extract(epoch FROM m.sampled_at - sqlc.arg(from_at)::timestamptz) / sqlc.arg(step_secs)::float8))
    m.id, m.sampled_at, m.total_bytes, m.used_bytes
FROM memory_samples m
WHERE m.sampled_at >= sqlc.arg(from_at)::timestamptz AND m.sampled_at < sqlc.arg(to_at)::timestamptz
ORDER BY floor(extract(epoch FROM m.sampled_at - sqlc.arg(from_at)::timestamptz) / sqlc.arg(step_secs)::float8),
    m.used_bytes DESC, m.id;

-- name: MemoryProcesses :many
SELECT run_id, pid, name, args, rss_bytes FROM memory_processes WHERE sample_id = sqlc.arg(sample_id) ORDER BY pid;
