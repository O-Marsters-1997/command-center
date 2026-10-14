-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY utilization_intervals_end_at_idx ON utilization_intervals (end_at);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS utilization_intervals_end_at_idx;
