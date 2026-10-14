-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY tickets_feature_idx ON tickets (feature) WHERE withdrawn_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS tickets_feature_idx;
