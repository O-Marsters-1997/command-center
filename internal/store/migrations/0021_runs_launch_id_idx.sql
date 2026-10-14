-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY runs_launch_id_idx ON runs (launch_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS runs_launch_id_idx;
