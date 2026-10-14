-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY intents_pending_idx ON intents (verb, id) WHERE consumed_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS intents_pending_idx;
