# Plan: Postgres replaces SQLite

> Source: `docs/adr/0011-postgres-replaces-sqlite.md`. No PRD; the ADR carries the why and this
> plan carries the how.
>
> **This is hand-driven work, not fleet work.** Phase 1 touches `go.mod`, `go.sum` and
> `.github/workflows/ci.yml`, all three deny-listed for agent pushes (`CLAUDE.md` § Constraints an
> agent will hit). Phases 2 to 4 are ordinary changes, but they cannot start until phase 1 has
> landed by hand.
>
> **Depends on nothing. Blocks `plans/feature-model.md`** (ADR 12), which renames `group_key` to
> `feature_id` and adds a `features` table with a foreign key into `tickets`.

## The starting point

The store is one SQLite file, opened at `internal/cc/store.go:31` with
`busy_timeout(5000)&journal_mode(WAL)&foreign_keys(1)`, migrated by goose through three files, and
queried entirely through sqlc: 39 named queries in `internal/cc/queries/*.sql` generating
`internal/cc/ccdb`. No SQL string survives in `internal/cc`'s non-test Go.

Four callers open a store: `internal/cc/app.go:121`, `e2e/register/register.go:86`,
`internal/cc/app_test.go:102` and `internal/cc/store_test.go:16`. All four pass
`Workspace.DBPath`, a file path.

## Technical design decisions

Durable across every phase.

**Driver and sqlc mode.** `github.com/jackc/pgx/v5/stdlib` registered as a `database/sql` driver,
with sqlc left on its default `database/sql` `sql_package`. `Store` keeps `*sql.DB` (ADR 8's
Decision requires it: goose migrates through it and the transactional methods call `BeginTx` on
it), `ccdb.DBTX` is unchanged, and the generated code keeps its `sql.NullString` shape. pgx's
native interface with `pgtype` would be faster and would rewrite all 39 generated signatures for
nothing. pgx is pure Go, so the no-cgo build survives; see *A correction ADR 11 owes* below.

**Connection string, not a path.** `Workspace.DBPath` is deleted. `Config` gains
`database_url string \`toml:"database_url"\``, resolved the way `DataDir` already is
(`internal/cc/statedir.go:35`): the config's own value, else `CC_DATABASE_URL`, else
`postgres://cc:cc@localhost:5432/cc?sslmode=disable`. `OpenStore(dsn string)` keeps its signature
shape and its name; only what the string means changes.

`Workspace.LockPath` stays. The flock is on a sibling file because one app instance per workspace
is the rule, and phase 1 rewrites its comment to say that instead of the SQLite claim it makes now
(`internal/cc/statedir.go:20`, `docs/designs/command-centre-design.md:768`).

**Schema.** One migration, `0001_init.sql`, expressing the end state. The three current files are
not translated one by one: 0002's `tasks` → `tickets` rename and 0003's six `ADD COLUMN`s have no
database left to replay against once the SQLite file is discarded, and carrying them forward
preserves exactly the nullability ADR 11 objects to.

| SQLite | Postgres | Why |
|---|---|---|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY` | sqlc keeps typing it `int64` |
| `pgid INTEGER`, `exit_code INTEGER` | `bigint` | `integer` would retype these `int32` and diff `runs.go` for no gain |
| the six nullable `tickets` columns | `text NOT NULL DEFAULT ''` | phase 1; see below |
| `at`, `pushed_at`, `ended_at`, … `TEXT` | `timestamptz` | phase 2 |
| `blocked_by TEXT` | `jsonb` | phase 3 |
| no `ON DELETE` clause | `ON DELETE CASCADE` | phase 4 |

`CHECK (state IN ('active','done','cancelled'))` on `launches` carries over unchanged.
`events.ticket_id` stays nullable: `launch.go` inserts NULL there on purpose for a launch event
belonging to no single ticket, and sqlc must keep generating `sql.NullString` for it.

**Test database provisioning.** A new `internal/cctest` package, one exported function:

```go
// DSN returns a connection string for an empty, migrated-from-scratch database, dropped when
// the test ends.
func DSN(t *testing.T) string
```

It honours `CC_TEST_DATABASE_URL` when set and starts a testcontainers Postgres otherwise, once
per process behind a `sync.Once`. Each call then runs `CREATE DATABASE cc_test_<n>` against that
server and registers a `DROP DATABASE` cleanup. One container per test binary, one database per
test: the same isolation the temp file per test gives today, without a container per test.

`internal/cctest` exists because two test binaries in different trees need it. It is the only new
package this plan adds.

**Test and CI topology.** CI sets `CC_TEST_DATABASE_URL` at a `postgres:17` service container, so
CI never starts testcontainers. Local `just test` falls back to testcontainers, so a clean
checkout needs Docker but no compose invocation. `just up` runs compose for anyone who would
rather point `CC_TEST_DATABASE_URL` at it.

`test` and `e2e` drop `macos-latest`. GitHub's macOS runners have no Docker daemon, and the
alternative is a green tick over a suite that quietly skipped the store. `build` keeps both, so
the darwin build stays proven.

**e2e isolation.** Each testscript gets its own database, exactly as each gets its own
`CC_DATA_DIR` today. `setup(env *testscript.Env)` (`e2e/testscript_test.go:213`) creates it and
appends `CC_DATABASE_URL=` to `scriptEnv`'s list. Scripts run in parallel against one server.

**Placeholders.** Every `?` in the five query files becomes `$1`, `$2`, … . sqlc's postgresql
engine requires it in the source, and sqlc still derives parameter names from the target columns,
so the generated argument structs keep their current field names.

### A correction ADR 11 owes

ADR 11's last line says "the pure-Go, no-cgo property the README advertises is gone." Only half of
that is true. pgx is pure Go, so `CGO_ENABLED=0` still builds. What goes is self-containment: the
binary now needs a database to be up. `README.md:207` should say that, and ADR 11's Consequences
should be corrected when this plan is approved.

---

## Phase 1: the engine swaps

**Covers:** everything ADR 11 calls the cost. The binary stops being self-contained, `go test ./...`
starts needing Docker, and no behaviour changes.

### What to build

Everything above, landing at once, because nothing smaller compiles: a migration rewrite that
leaves `internal/cc/ccdb` on the SQLite engine fails `go build`, and `e2e/register` boots a real
`OpenStore`, so the e2e suite breaks the instant the engine moves.

- `compose.yaml` at the repo root: one `postgres:17` service, a named volume, port 5432.
- `go.mod` gains `github.com/jackc/pgx/v5` and `github.com/testcontainers/testcontainers-go`;
  `modernc.org/sqlite` goes.
- `internal/cc/migrations/` becomes a single `0001_init.sql` per the table above, at phase 1's
  column types: the six `tickets` columns `NOT NULL DEFAULT ''`, everything else still `text`.
- `goose.SetDialect("postgres")` at `internal/cc/store.go:54`.
- `sqlc.yaml` → `engine: "postgresql"`; `just sqlc`; commit the regenerated `internal/cc/ccdb`.
- The five `COALESCE(source, '') AS source` reads in `queries/store.sql:11` go: the columns are
  `NOT NULL` now, and sqlc already types a `COALESCE`d column `string`, so dropping them is a SQL
  diff with no Go diff behind it.
- `queries/refresh.sql:10`: `sqlc.slice('kinds')` becomes `= ANY($1::text[])`.
- The three `COALESCE(p.pushed_at, '')` comparisons (`refresh.sql:10`, `pushes.sql:11`,
  `pushes.sql:32`) become `(p.pushed_at IS NULL OR <lhs> > p.pushed_at)`. The empty string was
  standing in for "never pushed"; say that instead.
- `internal/cctest`, and the four `OpenStore` call sites moved onto it or onto the new config.
- `e2e/testscript_test.go` setup creates a database per script and exports `CC_DATABASE_URL`.
- `.github/workflows/ci.yml`: a `postgres:17` service on `test` and `e2e`, `CC_TEST_DATABASE_URL`
  set, `macos-latest` dropped from those two jobs.
- `README.md:91`, `:199`, `:207` and `docs/designs/command-centre-design.md:588`, `:599`, `:721`,
  `:768` stop saying SQLite. Invariant 9's parenthetical becomes the one-instance-per-workspace
  reason.

The existing SQLite database is discarded. Tickets rebuild from `GET /import`; run, event, push
and launch history does not survive, and that is accepted rather than mitigated.

### Acceptance criteria

- [ ] `docker compose up -d && just run --config docs/command-centre.sample.toml` serves the board
- [ ] `just test` passes on a clean checkout with only Docker running
- [ ] `just test-e2e` passes, scripts still running in parallel
- [ ] `CGO_ENABLED=0 go build ./cmd/cc` still succeeds
- [ ] `internal/cc/migrations` holds one file; `grep -rn "sqlite" --include=*.go .` is empty
- [ ] `just sqlc && git diff --exit-code internal/cc/ccdb` is clean
- [ ] Every golden file in `internal/cc/testdata/` is byte-identical: this phase renders nothing new

---

## Phase 2: timestamps become `timestamptz`

**Covers:** ADR 11's "every timestamp is RFC3339 `TEXT`, parsed by hand on every read".

### What to build

`events.at`, `pushes.pushed_at`, `runs.proc_started_at`, `runs.ended_at`, `launches.created_at` and
`intents.at`, `intents.consumed_at` become `timestamptz`. sqlc retypes them `time.Time` and
`sql.NullTime`, which deletes the 15 `Format(time.RFC3339Nano)` and `time.Parse` sites in
`store.go:156`, `:326`, `:347`, `runs.go:39`, `:61`, `:77`, `:110`, `:154`, `:161`, `:204`, `:245`,
`pushes.go:20`, `:83` and `launch.go:29`, `:59`.

Watch the ordering predicates. Phase 1 left three comparisons reading `e.at > p.pushed_at` on
`text`, where RFC3339Nano's lexical order happens to match chronological order. On `timestamptz`
they compare properly, and the `IS NULL` arm phase 1 introduced still carries "never pushed".

### Acceptance criteria

- [ ] No `time.RFC3339Nano` anywhere in `internal/cc`
- [ ] `notNull` loses its timestamp callers, and goes if it has no others
- [ ] Event ordering, run duration and the restack-since-push predicate behave as before
- [ ] `internal/cc/testdata/` golden files unchanged, or the diff is reviewed and explained

---

## Phase 3: `blocked_by` becomes `jsonb`

**Covers:** ADR 11's "`tickets.blocked_by` is a JSON array stored in a `TEXT` column, marshalled
and unmarshalled in Go".

### What to build

`tickets.blocked_by` becomes `jsonb NOT NULL DEFAULT '[]'::jsonb`. sqlc types it `[]byte`, so
`store.go`'s four `json.Marshal`/`json.Unmarshal` sites (`:89`, `:133`, `:158`, and the `nonNil`
guard behind them) collapse to passing bytes through. The database now rejects a malformed array
at write time instead of at the next read.

ADR 12 will query this column for the blocker closure, which is the reason it is worth typing
properly before that plan starts.

### Acceptance criteria

- [ ] `encode blocked_by` and `decode blocked_by` error paths are gone from `store.go`
- [ ] A ticket with no blockers round-trips as `[]`, not `null`
- [ ] Writing invalid JSON to the column fails at the database, and a test says so

---

## Phase 4: the cascade moves into the schema

**Covers:** ADR 11's "`DeleteTicket` hand-rolls its cascade in four statements inside a
transaction".

### What to build

`launch_members.ticket_id`, `runs.ticket_id`, `pushes.ticket_id` and `events.ticket_id` gain
`ON DELETE CASCADE` on their references to `tickets(url)`. `DeleteLaunchMembersForTicket`,
`DeleteRunsForTicket` and `DeletePushesForTicket` are deleted from `queries/store.sql`, and
`DeleteTicket`'s transaction in `store.go` becomes one call.

`events.ticket_id` is nullable and its NULL rows belong to no ticket, so a cascade never reaches
them. That is the intended behaviour and the test should pin it: deleting a ticket must not remove
the launch events that carry a NULL `ticket_id`.

### Acceptance criteria

- [ ] `DeleteTicket` is one statement, no `BeginTx`
- [ ] Deleting a ticket removes its runs, pushes, launch members and its own events
- [ ] Deleting a ticket leaves NULL-`ticket_id` launch events in place
- [ ] `internal/cc/queries/store.sql` holds 8 queries, down from 11

---

## Out of scope

- **The sqlc drift CI job.** ADR 8's Decision promises `just sqlc` plus
  `git diff --exit-code internal/cc/ccdb` in CI and `ci.yml` has no such job. Phase 1's acceptance
  criteria run it by hand. Wiring it up is ADR 8's debt, and filing it against ADR 8 keeps this
  plan's un-agentable `.github/**` change to one commit.
- **Connection pool tuning.** `SetMaxOpenConns` and friends are a real knob on a server database
  and a guess until something is measured. Only the loop goroutine writes (invariant 9), so the
  defaults have a lot of room.
- **Data migration from the SQLite file.** Settled: the file is discarded.
- **The `group` to `feature` rename.** ADR 12, `plans/feature-model.md`.
