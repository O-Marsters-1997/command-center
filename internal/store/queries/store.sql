-- name: UpsertTickets :exec
INSERT INTO tickets (url, repo, branch, blocked_by, source, title, body, status, feature, synced_at)
SELECT t.url, t.repo, t.branch, t.blocked_by, t.source, t.title, t.body, t.status, t.feature, t.synced_at
FROM jsonb_to_recordset(sqlc.arg(tickets)::jsonb) AS t(
    url text, repo text, branch text, blocked_by jsonb, source text, title text, body text,
    status text, feature text, synced_at timestamptz
)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, branch = excluded.branch, blocked_by = excluded.blocked_by,
    source = excluded.source, title = excluded.title, body = excluded.body,
    status = excluded.status, feature = excluded.feature, synced_at = excluded.synced_at;

-- name: ListTickets :many
SELECT url, repo, branch, blocked_by, source, title, body, status, feature, synced_at
FROM tickets WHERE withdrawn_at IS NULL ORDER BY url;

-- name: GetTicketFeature :one
SELECT feature FROM tickets WHERE url = sqlc.arg(url) AND withdrawn_at IS NULL;

-- name: ListActiveTicketFeatures :many
SELECT url, feature FROM tickets
WHERE url IN (SELECT jsonb_array_elements_text(sqlc.arg(urls)::jsonb)) AND withdrawn_at IS NULL;

-- name: ListTicketFeatures :many
SELECT url, feature FROM tickets WHERE url IN (SELECT jsonb_array_elements_text(sqlc.arg(urls)::jsonb));

-- name: ImportTickets :exec
INSERT INTO tickets (url, repo, source, feature, title, body, status, synced_at, branch, blocked_by)
SELECT t.url, t.repo, t.source, t.feature, t.title, t.body, t.status, t.synced_at, t.branch, t.blocked_by
FROM jsonb_to_recordset(sqlc.arg(tickets)::jsonb) AS t(
    url text, repo text, source text, feature text, title text, body text,
    status text, synced_at timestamptz, branch text, blocked_by jsonb
)
ON CONFLICT (url) DO UPDATE SET
    repo = excluded.repo, source = excluded.source, feature = excluded.feature,
    title = excluded.title, body = excluded.body, status = excluded.status,
    synced_at = excluded.synced_at, withdrawn_at = NULL;

-- name: ListTicketsInFeature :many
SELECT url, repo, branch, blocked_by FROM tickets WHERE feature = sqlc.arg(feature) AND withdrawn_at IS NULL;

-- name: ListTicketBranches :many
SELECT url, repo, branch FROM tickets WHERE url IN (SELECT jsonb_array_elements_text(sqlc.arg(urls)::jsonb));

-- name: WithdrawTicket :exec
UPDATE tickets SET withdrawn_at = sqlc.arg(withdrawn_at) WHERE url = sqlc.arg(url) AND withdrawn_at IS NULL;

-- name: WithdrawTickets :exec
UPDATE tickets SET withdrawn_at = sqlc.arg(withdrawn_at)
WHERE url IN (SELECT jsonb_array_elements_text(sqlc.arg(urls)::jsonb)) AND withdrawn_at IS NULL;

-- name: ListWithdrawnTickets :many
SELECT url, repo, branch FROM tickets WHERE withdrawn_at IS NOT NULL;

-- name: ListTicketsWithBlockers :many
SELECT url, blocked_by FROM tickets WHERE withdrawn_at IS NULL;

-- name: SetBlockedBys :exec
UPDATE tickets t SET blocked_by = u.blocked_by
FROM jsonb_to_recordset(sqlc.arg(updates)::jsonb) AS u(url text, blocked_by jsonb)
WHERE t.url = u.url;

-- name: AppendEvent :exec
INSERT INTO events (at, ticket_id, kind, detail) VALUES (sqlc.arg(at), sqlc.arg(ticket_id), sqlc.arg(kind), sqlc.arg(detail));

-- name: ListEvents :many
SELECT at, ticket_id, kind, detail FROM (
    SELECT id, at, ticket_id, kind, detail FROM events ORDER BY id DESC LIMIT sqlc.arg(max_rows)
) newest
ORDER BY id;

-- name: ListTicketEventsAfter :many
SELECT id, at, ticket_id, kind, detail FROM events
WHERE ticket_id = sqlc.arg(ticket_id) AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(max_rows);

-- name: HasEvent :one
SELECT EXISTS (
    SELECT 1 FROM events WHERE ticket_id = sqlc.arg(ticket_id) AND kind = sqlc.arg(kind)
) AS found;

-- name: PutMeta :exec
INSERT INTO meta (key, value) VALUES (sqlc.arg(key), sqlc.arg(value))
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: GetMeta :one
SELECT value FROM meta WHERE key = sqlc.arg(key);

-- name: PruneConsumedIntents :execrows
DELETE FROM intents WHERE id IN (
    SELECT i.id FROM intents i WHERE i.consumed_at < sqlc.arg(before) LIMIT sqlc.arg(batch)
);

-- name: PruneTickErrors :execrows
DELETE FROM events WHERE id IN (
    SELECT e.id FROM events e WHERE e.kind = 'tick_error' AND e.at < sqlc.arg(before) LIMIT sqlc.arg(batch)
);
