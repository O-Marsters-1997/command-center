-- name: QueueEditTicketIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES (sqlc.arg(at), sqlc.arg(ticket_id), 'edit_ticket', sqlc.arg(payload));

-- name: PendingEditTicketIntents :many
SELECT id, ticket_id, payload FROM intents WHERE verb = 'edit_ticket' AND consumed_at IS NULL ORDER BY id;

-- name: EditTicket :execrows
UPDATE tickets SET branch = sqlc.arg(branch), blocked_by = sqlc.arg(blocked_by) WHERE url = sqlc.arg(url);
