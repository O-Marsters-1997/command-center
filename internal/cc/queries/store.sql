-- name: UpsertTicket :exec
INSERT INTO tickets (url, repo, branch, blocked_by, source, title, body, status, feature, synced_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, branch = excluded.branch, blocked_by = excluded.blocked_by,
    source = excluded.source, title = excluded.title, body = excluded.body,
    status = excluded.status, feature = excluded.feature, synced_at = excluded.synced_at;

-- name: Tickets :many
SELECT url, repo, branch, blocked_by, source, title, body, status, feature, synced_at
FROM tickets WHERE withdrawn_at IS NULL ORDER BY url;

-- name: TicketFeature :one
SELECT feature FROM tickets WHERE url = $1 AND withdrawn_at IS NULL;

-- name: TicketFeatureAny :one
SELECT feature FROM tickets WHERE url = $1;

-- name: ImportTicket :exec
INSERT INTO tickets (url, repo, source, feature, title, body, status, synced_at, branch, blocked_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, source = excluded.source, feature = excluded.feature,
    title = excluded.title, body = excluded.body, status = excluded.status,
    synced_at = excluded.synced_at, withdrawn_at = NULL;

-- name: TicketsInFeature :many
SELECT url, repo, branch, blocked_by FROM tickets WHERE feature = $1 AND withdrawn_at IS NULL;

-- name: TicketBranch :one
SELECT repo, branch FROM tickets WHERE url = $1;

-- name: WithdrawTicket :exec
UPDATE tickets SET withdrawn_at = $1 WHERE url = $2 AND withdrawn_at IS NULL;

-- name: WithdrawnTickets :many
SELECT url, repo, branch FROM tickets WHERE withdrawn_at IS NOT NULL;

-- name: TicketsWithBlockers :many
SELECT url, blocked_by FROM tickets WHERE withdrawn_at IS NULL;

-- name: SetBlockedBy :exec
UPDATE tickets SET blocked_by = $1 WHERE url = $2;

-- name: AppendEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES ($1, $2, $3, $4);

-- name: Events :many
SELECT at, ticket_id, kind, detail FROM events ORDER BY id;

-- name: PutMeta :exec
INSERT INTO meta (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: GetMeta :one
SELECT value FROM meta WHERE key = $1;
