-- name: Projects :many
SELECT name, hue FROM projects ORDER BY lower(name);

-- name: CreateProject :one
INSERT INTO projects (name, hue, created_at) VALUES ($1, $2, $3) RETURNING id;

-- name: ProjectID :one
SELECT id FROM projects WHERE lower(name) = lower($1);

-- name: SetRepoProjectID :execrows
UPDATE repos SET project_id = $2 WHERE name = $1;

-- name: EditProject :execrows
UPDATE projects SET name = sqlc.arg(new_name), hue = sqlc.arg(hue) WHERE lower(name) = lower(sqlc.arg(old_name));

-- name: DeleteProject :execrows
DELETE FROM projects WHERE lower(name) = lower($1);
