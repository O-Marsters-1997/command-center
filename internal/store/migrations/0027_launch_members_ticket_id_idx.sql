-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY launch_members_ticket_id_idx ON launch_members (ticket_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS launch_members_ticket_id_idx;
