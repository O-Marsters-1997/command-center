-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY events_kind_ticket_id_id_idx ON events (kind, ticket_id, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS events_kind_ticket_id_id_idx;
