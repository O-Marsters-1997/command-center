-- name: UpsertTicket :exec
INSERT INTO tickets (url, repo, branch, blocked_by, source, title, body, status, group_key, synced_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, branch = excluded.branch, blocked_by = excluded.blocked_by,
    source = excluded.source, title = excluded.title, body = excluded.body,
    status = excluded.status, group_key = excluded.group_key, synced_at = excluded.synced_at;

-- name: Tickets :many
SELECT url, repo, branch, blocked_by, source, title, body, status, group_key, synced_at
FROM tickets WHERE withdrawn_at IS NULL ORDER BY url;

-- name: ImportTicket :exec
INSERT INTO tickets (url, repo, source, group_key, title, body, status, synced_at, branch, blocked_by)
VALUES ($1, $2, 'github', $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, source = excluded.source, group_key = excluded.group_key,
    title = excluded.title, body = excluded.body, status = excluded.status,
    synced_at = excluded.synced_at, withdrawn_at = NULL;

-- name: TicketURLsInGroup :many
SELECT url FROM tickets WHERE group_key = $1;

-- name: WithdrawTicket :exec
UPDATE tickets SET withdrawn_at = $1 WHERE url = $2 AND withdrawn_at IS NULL;

-- name: AppendEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES ($1, $2, $3, $4);

-- name: Events :many
SELECT at, ticket_id, kind, detail FROM events ORDER BY id;

-- name: PutMeta :exec
INSERT INTO meta (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: GetMeta :one
SELECT value FROM meta WHERE key = $1;
