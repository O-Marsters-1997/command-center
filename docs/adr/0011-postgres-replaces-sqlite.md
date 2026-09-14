# 11. Postgres replaces SQLite

**Date:** 2026-09-14 · **Status:** proposed · **Revised:** 2026-09-14, sqlc had already landed

## Context

The store is a SQLite file opened with `busy_timeout(5000)`, `journal_mode(WAL)` and
`foreign_keys(1)`, driven by goose through three migrations. The driver is pure Go, so the binary
needs no cgo and the test suite opens a temp file per test. The README sells that
self-containment, and for a local control plane it was the right call.

Three things have accumulated against it.

**The schema is awkward to move.** Migration 0003 added six columns to `tickets` and every one is
nullable, because SQLite refuses `NOT NULL` on `ALTER TABLE ADD COLUMN` without a default. Five
`COALESCE`s read them back as empty strings. That is a small cost on its own — five lines — but it
is the shape of every future change, and the feature work (ADR 12) adds a table with a foreign key
into `tickets`.

**The types are doing work the database should do.** `tickets.blocked_by` is a JSON array stored in
a `TEXT` column, marshalled and unmarshalled in Go. Every timestamp is RFC3339 `TEXT`, parsed by
hand on every read. `DeleteTicket` hand-rolls its cascade in four statements inside a transaction,
because the referencing tables were declared without `ON DELETE CASCADE`.

**ADR 8 has landed, and it landed on a beta code path.** All 39 queries are generated, and
`sqlc.yaml` reads `engine: "sqlite"`. ADR 8 said what that costs: "Its SQLite engine is beta, which
is the reason to be careful rather than the reason not to do this." sqlc's Postgres engine is the
stable, first-class one.

Unpinning is cheaper than it sounds, and cheaper now than later. The five query files are close to
portable already. `ON CONFLICT ... DO UPDATE SET ... excluded.` is Postgres syntax that SQLite
copied, and nothing reaches for `strftime`, `INSERT OR REPLACE`, `julianday` or `group_concat`.
Two things genuinely break: `sqlc.slice('kinds')` in `refresh.sql` becomes a Postgres array, and
the three `COALESCE(pushed_at, '')` comparisons stop making sense once the column is `timestamptz`.
Concentrating every SQL string into five files is what made the move this small, and the bill only
grows as queries accumulate.

## Decision

Postgres replaces SQLite. The three migrations are rewritten against
the Postgres dialect rather than replayed: `INTEGER PRIMARY KEY AUTOINCREMENT` becomes an identity
column, `TEXT` timestamps become `timestamptz`, `blocked_by` becomes `jsonb`, and every table
referencing `tickets` declares `ON DELETE CASCADE`. `Store` keeps its public API; `DeleteTicket`
becomes a single statement. `sqlc.yaml` moves to `engine: "postgresql"` and the 39 queries
regenerate in the same change, because a migration rewrite that leaves the generated package
behind does not compile.

The database runs under docker compose in development. Tests use testcontainers, sharing one
container across as many packages as the harness allows rather than starting one per package.

Work order is Postgres, then the feature model (ADR 12). The Postgres step changes no behaviour,
which is what makes it reviewable on its own.

**Verbs-as-intents survives unchanged.** It was introduced to close a launch/cancel race and is
what makes every verb idempotent (`docs/designs/command-centre-design.md` § 8); it is not a
SQLITE_BUSY workaround and does not go away with the engine. Only invariant 9's parenthetical
changes: the flock sits on a sibling file because one app instance per workspace is the rule, not
because locking the DB file deadlocks modernc.org/sqlite.

## Consequences

The binary stops being self-contained. `go test ./...` requires Docker, and running the app
requires a database that is up. That is the whole cost of this decision, and it is paid on every
clean checkout.

sqlc regenerates against its stable engine. ADR 8 is `accepted` and built, so it is amended rather
than superseded. Its Decision stands. Its Context loses both the `ALTER TABLE` argument and the
beta-engine caveat.

`go.mod` gains a Postgres driver and `.github/workflows/ci.yml` gains a service or a Docker step.
Both are deny-listed for agent pushes (`CLAUDE.md` § Constraints an agent will hit), so this
migration is hand-driven. Isolating it as the first of two steps keeps the un-agentable part
small; the feature work that follows is ordinary fleet work.

The pure-Go, no-cgo property the README advertises is gone. The README says so.
