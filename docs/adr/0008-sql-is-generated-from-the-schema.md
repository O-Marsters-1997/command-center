# 8. SQL is generated from the schema

**Date:** 2026-09-13 · **Status:** accepted · **Amended:** 2026-09-14 for ADR 11

## Context

The store makes 36 hand-written SQL calls: 8 in `store.go`, 13 in `runs.go`, 9 in `launch.go`, 5
in `pushes.go` and 1 in `refresh.go`. They become 39 named queries, because `DeleteTicket` runs
four statements through one call site. None of them is checked against the schema
until the statement runs. A column renamed in a migration, an argument list that drifts out of
order, a `Scan` reading six values from a five-column `SELECT`: all of it compiles, and the first
sign of trouble is a failed tick.

The schema has moved twice already, and is about to move twice more: ADR 11 rewrites the
migrations against Postgres, and ADR 12 adds `features` with a foreign key into `tickets`.
Migration 0002 renamed `tasks` to `tickets` and renamed the foreign key column in five tables.
Migration 0003 added six columns to `tickets`. That kind of change is exactly what a hand-written
statement gets wrong quietly.

Nullability is already being got wrong. `events.ticket_id` is nullable, and `launch.go` inserts
`NULL` into it on purpose for a launch event that belongs to no single ticket.
`latestRefreshOutcomes` and `RestackedTickets` both scan that column into a plain `string`. They
survive only because their `kind` filters happen to exclude launch events. Nothing in the code
says so.

sqlc reads the goose migrations, replays them, and generates typed Go for each named query. A
spike against the real migrations and the six hardest queries in the store generated correctly:
`COUNT(*) OVER (PARTITION BY lm.launch_id)` in `LaunchMemberships` types as `int64`,
`COALESCE(source, '')` types as `string` rather than `interface{}`, and `sqlc.slice()` works.

That spike ran against sqlc's SQLite engine, which is beta, and the generated package sits on that
engine today. ADR 11 moves the store to Postgres and regenerates through sqlc's stable engine,
which is when the caution the beta path calls for stops being owed.

## Decision

sqlc generates `internal/cc/ccdb` from `internal/cc/migrations` and a new
`internal/cc/queries`, one `.sql` per Go file in the store. The generated package is committed.

`Store` keeps its public API unchanged. Every method still takes and returns domain types and
still wraps every error with the context it wraps today. The generated `Queries` sits behind it as
an unexported field, and no caller outside `internal/cc` sees a null wrapper or a generated row
struct. `Store` continues to hold `*sql.DB`, which goose needs for migrations and the
transactional methods need for `BeginTx`.

`sqlc.yaml` renames three columns whose generated names disagree with the domain structs: `url`
to `URL`, `baseline_sha` to `BaselineSHA` and `base_sha_at_push` to `BaseSHAAtPush`.

Drift is caught the way the repo already catches it for `app.css` and `go.mod`. A `just sqlc`
recipe regenerates, and a CI job runs it and then `git diff --exit-code internal/cc/ccdb`. The
sqlc version is pinned in the recipe's `go run` line rather than added to `go.mod`, which would
drag wazero, the TiDB parser, cel-go and grpc into `go.sum` for a tool that never ships in the
binary.

The schema is ADR 11's to settle, not this one's: nullability, `jsonb` and `timestamptz` are
decided by the Postgres rewrite, and sqlc regenerates from whatever it produces.
The nullable columns on `runs` stay nullable either way, because NULL is load-bearing there:
`WHERE pgid IS NOT NULL AND outcome IS NULL` is the predicate for a run that is still alive.

## Consequences

A bad column name fails `go build`, not a tick at three in the morning. The two `ticket_id` scans
above stop being safe by coincidence: sqlc types the column `sql.NullString` and the mapping code
has to say what it does with a NULL.

Generation is not all-or-nothing. `Store` still holds `*sql.DB`, so a query sqlc cannot parse
stays hand-written on `s.db` next to the generated ones, and the two coexist for as long as they
need to. The cost of a parser failure is one method that does not get migrated, not a stalled
migration.

Reading the SQL for a method now means opening two files instead of one. `internal/cc/queries`
mirrors the Go files by name to keep that lookup mechanical.

The board's `ticket` and `task` columns are renamed in the same effort. `ticket` is the unit of
work at every layer, and the column showing the tracker's `#CC-1` reference is headed `ref`. sqlc
derives every generated identifier from the table and column names, so the term had to be settled
before the first `generate`, not after. `CONTEXT.md` carries the resolution.
