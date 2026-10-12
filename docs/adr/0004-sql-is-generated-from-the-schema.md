# 4. SQL is generated from the schema

**Date:** 2026-09-13 · **Status:** accepted

## Context

Hand-written SQL is checked against the schema only when a tick runs it, and a nullable column scanned
into a plain Go type fails only on the row that holds NULL.

## Decision

sqlc generates `internal/store/ccdb` from `internal/store/migrations` and `internal/store/queries`
(one `.sql` per store Go file), committed. Only `internal/store` imports `ccdb`, enforced by
depguard. `Store` keeps its public API and domain types, and holds the generated `Queries` unexported
beside `*sql.DB`, which goose and `BeginTx` need. `sqlc.yaml` renames columns whose generated names
disagree with the domain structs.

`just sqlc` regenerates. The sqlc version is pinned in the recipe's `go run` line, not `go.mod`, to
keep its dependencies out of `go.sum`.

## Consequences

A bad column fails `go build`, not a tick, and a nullable scan must say what it does with NULL. A
query sqlc cannot parse stays hand-written on `s.db`. sqlc derives identifiers from column names, so a
column's name is settled in `CONTEXT.md` before its query is written.
