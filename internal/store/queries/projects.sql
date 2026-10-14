-- name: ListProjects :many
SELECT name, hue FROM projects ORDER BY lower(name);

-- name: CreateProject :one
INSERT INTO projects (name, hue, created_at) VALUES (sqlc.arg(name), sqlc.arg(hue), sqlc.arg(created_at)) RETURNING id;

-- name: GetProjectID :one
SELECT id FROM projects WHERE lower(name) = lower(sqlc.arg(name));

-- name: SetRepoProjectID :execrows
UPDATE repos SET project_id = sqlc.arg(project_id) WHERE name = sqlc.arg(name);

-- name: EditProject :execrows
UPDATE projects SET name = sqlc.arg(new_name), hue = sqlc.arg(hue) WHERE lower(name) = lower(sqlc.arg(old_name));

-- name: DeleteProject :execrows
DELETE FROM projects WHERE lower(name) = lower(sqlc.arg(name));
