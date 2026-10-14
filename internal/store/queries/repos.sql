-- name: Repos :many
SELECT r.name, r.remote, r.state, r.refusal_kind, r.refusal, r.settings_source, r.settings_read_at, r.tracked_at,
    p.name AS project, p.hue AS project_hue
FROM repos r LEFT JOIN projects p ON p.id = r.project_id ORDER BY r.name;

-- name: UpsertRepo :exec
INSERT INTO repos (name, remote, state, refusal_kind, refusal, tracked_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (name) DO UPDATE SET
    remote = EXCLUDED.remote, state = EXCLUDED.state,
    refusal_kind = EXCLUDED.refusal_kind, refusal = EXCLUDED.refusal;

-- name: SetRepoState :execrows
UPDATE repos SET state = $2, refusal_kind = $3, refusal = $4, settings_source = $5, settings_read_at = $6
WHERE name = $1;
