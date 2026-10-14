-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY intents_consumed_at_idx ON intents (consumed_at) WHERE consumed_at IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS intents_consumed_at_idx;
