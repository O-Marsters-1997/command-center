-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY intents_run_id_idx ON intents (run_id) WHERE run_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS intents_run_id_idx;
