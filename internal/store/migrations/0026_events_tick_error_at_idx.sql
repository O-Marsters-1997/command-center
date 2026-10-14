-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY events_tick_error_at_idx ON events (at) WHERE kind = 'tick_error';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS events_tick_error_at_idx;
