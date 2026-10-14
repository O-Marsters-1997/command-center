-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY pushes_ticket_id_id_idx ON pushes (ticket_id, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS pushes_ticket_id_id_idx;
