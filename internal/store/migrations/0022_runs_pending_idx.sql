-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY runs_pending_idx ON runs (id) WHERE pgid IS NOT NULL AND outcome IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS runs_pending_idx;
