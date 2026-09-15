-- name: QueueEditTicketIntent :exec
INSERT INTO intents (at, ticket_id, verb, payload) VALUES ($1, $2, 'edit_ticket', $3);

-- name: PendingEditTicketIntents :many
SELECT id, ticket_id, payload FROM intents WHERE verb = 'edit_ticket' AND consumed_at IS NULL ORDER BY id;

-- name: EditTicket :exec
UPDATE tickets SET branch = $1, blocked_by = $2 WHERE url = $3;
