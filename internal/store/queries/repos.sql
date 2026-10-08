-- name: Repos :many
SELECT name, remote, state, refusal_kind, refusal, settings_source, settings_read_at, tracked_at
FROM repos ORDER BY name;

-- name: UpsertRepo :exec
INSERT INTO repos (name, remote, state, refusal_kind, refusal, tracked_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (name) DO UPDATE SET
    remote = EXCLUDED.remote, state = EXCLUDED.state,
    refusal_kind = EXCLUDED.refusal_kind, refusal = EXCLUDED.refusal;

-- name: SetRepoState :exec
UPDATE repos SET state = $2, refusal_kind = $3, refusal = $4 WHERE name = $1;

-- name: RenameTicketRepo :exec
UPDATE tickets SET repo = sqlc.arg(new_name) WHERE repo = sqlc.arg(old_name);
