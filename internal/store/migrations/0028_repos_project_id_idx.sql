-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY repos_project_id_idx ON repos (project_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS repos_project_id_idx;
