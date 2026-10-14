-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY runs_ticket_id_id_idx ON runs (ticket_id, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS runs_ticket_id_id_idx;
