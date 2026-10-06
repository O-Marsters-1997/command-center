# 5. SQL is generated from the schema

**Date:** 2026-09-13 · **Status:** accepted

## Context

The store made 36 hand-written SQL calls, none checked against the schema until a tick ran a bad one.
Nullability was already wrong: `events.ticket_id` is nullable (a launch event belongs to no ticket)
yet two queries scanned it into a plain `string`, surviving only because their `kind` filters happened
to exclude those rows.

## Decision

sqlc generates `internal/cc/ccdb` from `internal/cc/migrations` and `internal/cc/queries` (one `.sql`
per store Go file), committed. `Store` keeps its public API and domain types, and holds the generated
`Queries` unexported beside `*sql.DB`, which goose and `BeginTx` still need. `sqlc.yaml` renames
columns whose generated names disagree with the domain structs.

`just sqlc` regenerates and CI runs `git diff --exit-code internal/cc/ccdb`. The sqlc version is
pinned in the recipe's `go run` line, not `go.mod`, to keep its dependencies out of `go.sum`.

The schema is unchanged: `COALESCE` columns stay, and nullable `runs` columns stay nullable because
`pgid IS NOT NULL AND outcome IS NULL` is the predicate for a live run.

## Consequences

A bad column fails `go build`, not a tick, and nullable scans must say what they do with NULL. A
query sqlc cannot parse stays hand-written on `s.db`. `ticket` is the unit of work at every layer and
the tracker's reference column is `ref` (`CONTEXT.md`), settled first because sqlc derives
identifiers from names.
