-- name: UpsertTicket :exec
INSERT INTO tickets (url, repo, branch, blocked_by, source, title, body, status, group_key, synced_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, branch = excluded.branch, blocked_by = excluded.blocked_by,
    source = excluded.source, title = excluded.title, body = excluded.body,
    status = excluded.status, group_key = excluded.group_key, synced_at = excluded.synced_at;

-- name: Tickets :many
SELECT url, repo, branch, blocked_by,
       COALESCE(source, '') AS source, COALESCE(title, '') AS title, COALESCE(body, '') AS body,
       COALESCE(status, '') AS status, COALESCE(group_key, '') AS group_key, COALESCE(synced_at, '') AS synced_at
FROM tickets ORDER BY url;

-- name: ImportTicket :exec
INSERT INTO tickets (url, repo, source, group_key, title, body, status, synced_at, branch, blocked_by)
VALUES (?, ?, 'github', ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, source = excluded.source, group_key = excluded.group_key,
    title = excluded.title, body = excluded.body, status = excluded.status,
    synced_at = excluded.synced_at;

-- name: DeleteLaunchMembersForTicket :exec
DELETE FROM launch_members WHERE ticket_id = ?;

-- name: DeleteRunsForTicket :exec
DELETE FROM runs WHERE ticket_id = ?;

-- name: DeletePushesForTicket :exec
DELETE FROM pushes WHERE ticket_id = ?;

-- name: DeleteTicket :exec
DELETE FROM tickets WHERE url = ?;

-- name: AppendEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES (?, ?, ?, ?);

-- name: Events :many
SELECT at, ticket_id, kind, detail FROM events ORDER BY id;

-- name: PutMeta :exec
INSERT INTO meta (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: GetMeta :one
SELECT value FROM meta WHERE key = ?;
