-- name: ListRepos :many
SELECT r.name, r.remote, r.state, r.refusal_kind, r.refusal, r.settings_source, r.settings_read_at, r.tracked_at,
    p.name AS project, p.hue AS project_hue
FROM repos r LEFT JOIN projects p ON p.id = r.project_id ORDER BY r.name;

-- name: UpsertRepo :exec
INSERT INTO repos (name, remote, state, refusal_kind, refusal, tracked_at)
VALUES (sqlc.arg(name), sqlc.arg(remote), sqlc.arg(state), sqlc.arg(refusal_kind), sqlc.arg(refusal), sqlc.arg(tracked_at))
ON CONFLICT (name) DO UPDATE SET
    remote = EXCLUDED.remote, state = EXCLUDED.state,
    refusal_kind = EXCLUDED.refusal_kind, refusal = EXCLUDED.refusal;

-- name: SetRepoState :exec
UPDATE repos SET state = sqlc.arg(state), refusal_kind = sqlc.arg(refusal_kind), refusal = sqlc.arg(refusal),
    settings_source = sqlc.arg(settings_source), settings_read_at = sqlc.arg(settings_read_at)
WHERE name = sqlc.arg(name);
