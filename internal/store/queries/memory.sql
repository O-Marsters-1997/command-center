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
