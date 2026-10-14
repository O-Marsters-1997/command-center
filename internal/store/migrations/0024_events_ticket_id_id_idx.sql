-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY events_ticket_id_id_idx ON events (ticket_id, id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS events_ticket_id_id_idx;
