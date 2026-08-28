# Plan: repo and ticket model

> Source: the grilling session of 2026-08-27 on branch `cc-repo-model-location-independence`, which
> settled the design. Supersedes `docs/designs/location-independence.md` and
> `docs/adr/0003-a-repo-is-a-remote-not-a-path.md`, both written before the session and both stale.
> Origin: `.claude/handoffs/command-center__remodel-repos-for-location-independence.md`.

Two pieces of work sharing one design. Phases 1 to 5 remove the app's dependence on a
pre-existing directory tree. Phases 6 to 8 move the fleet from a TOML import into a data model
the app owns and syncs from the tracker. Land them on two branches, 1 to 5 then 6 to 8.

Neither piece dockerises anything. No Dockerfile, no credentials from the environment beyond the
two variables named below, no decision about where agents run.

## Technical design decisions

### Routes

Existing, unchanged: `GET /{$}`, `GET /board`, `GET /assets/`, `GET /preview`, `GET /events`,
`GET /confirm`, `POST /launch`, `POST /verb`.

Renamed in phase 6, following the table: `GET /task/{task}/detail` and `GET /task/{task}/log`
become `GET /ticket/{ticket}/detail` and `GET /ticket/{ticket}/log`.

New:

- `GET /import` renders the import page: the tracker groups available, and which tickets each
  would bring in.
- `POST /import` triggers a sync for one group. Writes an intent row and redirects, like every
  other write handler. Body: `group=project:fleet-view`.
- `POST /ticket` edits one ticket's app-owned fields. Body: `ticket=<url>`, plus `branch` and
  `blocked_by`. Writes an intent row and redirects.

Both new POSTs go behind `requireBrowserOrigin`, as `POST /launch` and `POST /verb` already do.

There is no `POST /tickets` and no per-ticket create endpoint. Labelling an issue and importing
its group is how a ticket enters the fleet.

### Schema

goose owns the schema from phase 2. `internal/cc/schema.sql` becomes `migrations/0001_init.sql`
verbatim, so an existing database recognises every table and goose records 0001 as applied
without touching data. `SchemaVersion`, `metaSchemaVersion` and the version-mismatch refusal in
`Store.init` all go.

Later migrations:

- `0002_tickets.sql` — `ALTER TABLE tasks RENAME TO tickets`, `ALTER TABLE tickets RENAME COLUMN
  ticket_url TO url`, and `RENAME COLUMN task_id TO ticket_id` on `runs`, `pushes`,
  `launch_members`, `events` and `intents`. SQLite 3.25+ rewrites the foreign key clauses that
  reference the renamed table and columns, so no table rebuild is needed.
- `0003_ticket_fields.sql` — `ALTER TABLE tickets ADD COLUMN` for `source`, `title`, `body`,
  `status`, `group_key`, `synced_at`. All nullable TEXT, so all O(1) with no row rewrite.
- `0004_withdrawal.sql` — `ALTER TABLE tickets ADD COLUMN withdrawn_at TEXT`.

`tickets` after phase 8:

| column | owner | notes |
|---|---|---|
| `url` | tracker | primary key, the issue URL |
| `source` | derived | `github`; the adapter that fetched it |
| `repo` | config | which `[[repo]]` block, matched on the URL's owner/name |
| `group_key` | import | the `project:` label it came in under |
| `title`, `body`, `status` | tracker | refreshed on every import |
| `synced_at` | import | when the tracker last answered for this row |
| `branch` | app | generated `cc-<number>-<slug>`, editable |
| `blocked_by` | app | JSON array of URLs, seeded from GitHub dependencies, editable |
| `withdrawn_at` | app | set when an import no longer returns it |

`seams` is dropped in phase 1, before the rename.

### Key models

- `cc.Ticket` replaces `cc.Task`. Fields as the table above.
- `plan.Ticket` replaces `plan.Task`. Stays pure: URL, Repo, Branch, BlockedBy. It never learns
  about title, body, status or the tracker, so `internal/plan/api_test.go` keeps passing.
- `cc.Repo` gains `Remote`, loses nothing. `Path` survives as the alternative.
- `cc.Workspace` loses `Name` and `Root`, gains `ReposDir`. `DBPath`, `LockPath`, `RunsDir` and
  `SettingsPath` all move under `state/`.
- `tracker.Ticket` is the adapter's return shape: URL, Number, Title, Body, Status, BlockedBy.
  Deliberately not `cc.Ticket`, so the tracker package never learns the app's columns.

### Module boundaries

**`internal/tracker`** (new, deep). Owns everything about reading an issue tracker. Exposes three
functions and one interface, and nothing above it knows GitHub exists:

```go
type Source interface {
    Groups(ctx context.Context) ([]Group, error)
    Tickets(ctx context.Context, group string) ([]Ticket, error)
}
func For(ticketURL string) (Source, error)   // dispatch on host
```

The GitHub implementation shells `gh` exactly as `internal/gh` does. Worth isolated tests: the
group listing, the issue listing, the dependency read and the branch slug are all pure given
recorded `gh` output.

**`internal/gh`** stays as it is: pull requests and the checkout. `IssueBody` moves to
`internal/tracker` in phase 7, since the body is now a column rather than a per-spawn fetch.

**`internal/cc`** keeps the shell: config, workspace, store, loop, page. It gains a `checkout`
concern (resolve, clone, verify) which is small enough to live in `statedir.go` and `config.go`
rather than earn a package.

**`internal/plan`** is untouched except for the rename and the seam deletion. Still imports
nothing outside the standard library.

### Interface contracts

```go
// config.go — one resolution, both forms, at load
func (r Repo) CheckoutPath(dataDir, configDir string) (string, error)

// statedir.go — no inference left
func ResolveWorkspace(dataDir string) (Workspace, error)

// checkout.go — startup ensure, once per repo
func EnsureCheckout(ctx context.Context, repo Repo, path string) error

// tracker — see above
```

`repoPathsByName(root, repos)` dies. Each `Repo` carries its resolved `checkoutPath` after
`LoadConfig`, and the nine call sites read the field.

### Integration points

- `gh` for pull requests, the squash-only check, and now issues and labels. Needs only the `repo`
  scope, verified: `GET /repos/{o}/{r}/issues/{n}/dependencies/blocked_by` answers today.
- `git` for clone, fetch, rev-parse, show and worktree listing.
- `tp` for `new` and `remove`. The app never passes `--open` or `--ticket`, so it depends on no
  `.treepad.toml` key, which is why that file stays gitignored.
- goose as a library, called from `OpenStore`, not as a CLI in the deploy path.

### Configuration

`cc/config.toml`, tracked. Holds `port`, `max_agents`, a container-appropriate `agent_command`,
and the `[[repo]]` blocks. `[[task]]` and `[[seam]]` are both gone by phase 7.

`CC_DATA_DIR` and `CC_AGENT_COMMAND` carry everything machine-specific. `data_dir` resolves from
the config key, then the environment variable, then `os.UserConfigDir()/command-centre`.

```
$CC_DATA_DIR/
  state/    command-centre.db, command-centre.lock, runs/, settings/
  repos/    <name>/ checkouts, and the worktrees tp cuts beside them
```

---

## Phase 1: Bin seams

**Covers**: removing a cross-repo feature that has never been configured, so every later phase
moves less code.

### What to build

Delete `[[seam]]`, `Seam`, `Task.Seams`, `retirement`, `retirementsByName`, `allProducersMerged`
and the retirement branch of `composePrompt`, plus `internal/plan/seam.go` and its tests. A
prompt becomes the composed task text and the ticket body, with no seam resolution and no
refusal path for unreadable seam content. `NewServer` loses its `seams` and `seamsRoot`
parameters. `plan.Compose` keeps its signature but is always called with an empty content slice,
so simplify it to match rather than leaving a parameter nothing fills.

Write `docs/adr/0004-seams-are-removed.md` recording what seams solved: shared prompt text
describing an interface that does not exist yet, retiring itself the moment the producing PR
merges and the real file appears at `lands_at`. Record why it goes now: built for a second repo
that has not arrived, never configured once, and the app cannot key branches per repo anyway
(`NewObserver`), so it could not have carried a real second repo regardless. Record what to
rebuild it from if a second repo lands.

### Acceptance criteria

- [ ] No occurrence of `seam` remains in `internal/cc`, `internal/plan`, the templates or the config
- [ ] `go build ./... && go vet ./... && go test ./...` passes, and `go test -tags=e2e ./e2e/...` passes
- [ ] A spawned run's prompt is the composed text plus the ticket body, verified against a golden file
- [ ] `docs/adr/0004-seams-are-removed.md` records the problem, the rejection and the rebuild path
- [ ] `internal/plan/api_test.go` still passes, so the pure package took no new imports

---

## Phase 2: goose replaces the frozen schema

**Covers**: making the database migratable before it becomes the thing you cannot rebuild.

### What to build

Add goose as a library dependency. Move `internal/cc/schema.sql` to `migrations/0001_init.sql`
byte for byte, keeping every `CREATE TABLE IF NOT EXISTS`, and embed the directory. `OpenStore`
calls `goose.SetDialect("sqlite3")` and `goose.Up` in place of executing the schema and comparing
`meta.schema_version`. Delete `SchemaVersion`, `metaSchemaVersion`, the mismatch refusal and
`Store.SchemaVersion`.

The live database is the test that matters. It already holds every table 0001 creates, so 0001
must apply as a no-op and goose must record it, leaving 532K of run history untouched. Take a copy
first and diff the tables after.

### Acceptance criteria

- [ ] A copy of the live database opens, goose records 0001 applied, and every existing row survives
- [ ] A fresh empty database gets the full schema from 0001 alone
- [ ] Opening twice is a no-op the second time
- [ ] `SchemaVersion` and the version-mismatch error are gone from `internal/cc`
- [ ] Migrations are embedded, so the binary needs no migrations directory beside it at runtime

---

## Phase 3: Explicit data_dir

**Covers**: killing the grandparent rule, which has already produced two databases on one laptop.

### What to build

`ResolveWorkspace` takes a resolved data directory rather than a config path, and derives nothing.
Resolution order is the `data_dir` config key, then `CC_DATA_DIR`, then
`os.UserConfigDir()/command-centre`, with `~` expanded. Create `state/` and `repos/` at 0700 and
put the database, lock, `runs/` and `settings/` under `state/`. Delete `Workspace.Name` and
`Workspace.Root`.

`Repo.Path` stops being relative to an inferred root and becomes absolute or relative to the
config file's own directory. `LoadConfig` resolves each repo's checkout path once and stores it on
the `Repo`. `repoPathsByName` goes, and its nine call sites plus `observe.go:63` and
`repocheck.go:58` read the field instead. Seam resolution is already gone, so nothing else reads
a root.

Rewrite `docs/command-centre.sample.toml`, which currently leans on the grandparent rule twice
over by sitting in `docs/` and setting `path = "."`.

### Acceptance criteria

- [ ] Pointing `data_dir` at the existing state directory runs against the existing database with no file moved
- [ ] Running from any working directory with any `--config` path produces the same workspace
- [ ] No function in `internal/cc` calls `filepath.Dir` on a config path
- [ ] `just run --config docs/command-centre.sample.toml` works from a clean checkout
- [ ] The second, accidental state directory under `command-centre/command-center/` can no longer be created

---

## Phase 4: A repo is a remote the app clones

**Covers**: a fresh machine producing a running system from the config alone.

### What to build

`[[repo]]` gains `remote`. Exactly one of `remote` and `path` is required, and setting both is
refused at load with an error naming the repo. A `remote` repo's checkout is
`<data_dir>/repos/<name>`, with `name` validated as a single safe path segment.

`EnsureCheckout` runs once per repo at startup, before the flock and beside
`AssertReposSquashOnly`. Absent means clone. Present with a matching origin means fetch. Matching
compares host and owner/name with the scheme, any `.git` suffix and a trailing slash stripped, so
the ssh and https forms of one repository are one repository. A mismatch, or a directory that is
not a git repository, refuses to start with an error naming both remotes.

Also fix `mergifyHash`, which reads `.mergify.yml` from the working tree while every other read in
the app goes through a remote-tracking ref. Read it out of `origin`'s default branch.

### Acceptance criteria

- [ ] An empty `data_dir` and a remote-only config clones and reaches a serving state unattended
- [ ] A second start against the same `data_dir` fetches and does not re-clone
- [ ] `git@github.com:o/r.git` and `https://github.com/o/r` are accepted as the same remote
- [ ] A checkout whose origin names a different repository refuses to start, naming both
- [ ] A repo naming both `remote` and `path`, or neither, is refused at config load
- [ ] `mergifyHash` returns the same hash whether the working tree is clean, dirty or on another branch

---

## Phase 5: Config and procedure files move into the repo

**Covers**: a clone carrying everything a human authors, with the machine supplied by two
environment variables.

### What to build

Move the config to `cc/config.toml` and track it. `.gitignore` currently excludes all of
`.claude/`, so the new home sits outside it rather than fighting it. The tracked
`agent_command` is the plain container-appropriate one; `CC_AGENT_COMMAND` overrides it, which is
where the laptop's `caffeinate` and `safehouse` wrapper goes.

Move the resolve-merge-conflict skill to `cc/skills/resolve-merge-conflict/SKILL.md`, tracked, and
have `resolveSkillPath` resolve it against the repo checkout rather than a workspace root. That
code arrives with `cc-agent-resolves-refresh-conflict`; if that branch has not landed, this step
is the file move and the `.gitignore` change alone.

`.treepad.toml` stays gitignored. The app calls only `tp new` and `tp remove` and passes neither
`--open` nor `--ticket`, so it depends on no key in that file. Record in the ADR that a worktree
therefore loses `.claude/settings.local.json`, and that in `-p` mode an unpermitted tool is denied
rather than prompted, so agents may quietly lose capabilities in a container. Unverified, and it
belongs to the container task.

### Acceptance criteria

- [ ] A clean clone plus `CC_DATA_DIR` reaches a serving state with no file written by hand
- [ ] `CC_AGENT_COMMAND` overrides the tracked `agent_command`, and its absence uses the tracked one
- [ ] The resolve procedure resolves from the checkout, and its absence is still not an error
- [ ] `.treepad.toml` is still ignored, and `tp new` still cuts a worktree the app finds via `git worktree list`
- [ ] The settings-sync risk is written down in an ADR, marked unverified

---

## Phase 6: `tasks` becomes `tickets`

**Covers**: naming the unit of work after what it is, before the tracker starts filling it.

### What to build

Migration `0002_tickets.sql` renames the table, renames `ticket_url` to `url`, and renames
`task_id` to `ticket_id` on `runs`, `pushes`, `launch_members`, `events` and `intents`. Nothing is
rebuilt: SQLite rewrites the foreign key clauses that reference the renamed table and columns.

Go-side, `cc.Task` becomes `cc.Ticket` and `plan.Task` becomes `plan.Ticket`, rippling through
`internal/plan` and every derived name that reads `task`. Routes `GET /task/{task}/detail` and
`GET /task/{task}/log` become `/ticket/{ticket}/...`, and the templates follow. Behaviour does not
change anywhere.

### Acceptance criteria

- [ ] The live database migrates in place, and every run, push, event and launch membership still resolves to its ticket
- [ ] `go build ./... && go vet ./... && go test ./...` and the e2e suite pass with no behaviour change
- [ ] No identifier in `internal/cc` or `internal/plan` names a task
- [ ] The rendered page is byte-identical to before the rename except for the two route paths
- [ ] `internal/plan/api_test.go` still passes

---

## Phase 7: Tracker adapter and ticket import

**Covers**: `/to-tickets` going back to only creating issues, and the app learning its fleet from
the tracker.

### What to build

`internal/tracker` with `For(url)` dispatching on host and a GitHub implementation. `Groups`
lists `project:`-prefixed labels. `Tickets(group)` lists open issues carrying both that label and
`status:ready` or beyond, and for each one reads title, body, status and the native dependency
list from `GET /repos/{o}/{r}/issues/{n}/dependencies/blocked_by`. The filter is the definition of
in flight, so a `status:backlog` issue never enters the table.

Migration `0003_ticket_fields.sql` adds the tracker-owned columns. Import upserts on `url`,
refreshing tracker-owned fields and leaving app-owned ones alone, except that a row seen for the
first time gets `blocked_by` from the dependency read and `branch` generated as `cc-<number>-<slug>`.
`repo` matches on the URL's owner and name against each `[[repo]]`'s remote.

`GET /import` renders the groups and what each would bring in. `POST /import` writes an intent
row; the next tick performs the sync, so the loop stays the only writer. The spawn path stops
calling `gh issue view` and reads `tickets.body`.

`[[task]]` leaves the config. Your thirteen existing rows are already in the table after phase 6
and keep working; they gain tracker-owned fields the first time an import returns them, which
means labelling those issues `project:<something>`.

### Acceptance criteria

- [ ] `GET /import` lists the `project:` labels across every configured repo
- [ ] Importing a group creates one row per matching issue with url, title, body, status, repo and group set
- [ ] A ticket with GitHub dependencies arrives with `blocked_by` populated, and issue 105's four edges reproduce the current config
- [ ] A `status:backlog` issue in the same group is not imported
- [ ] Re-importing refreshes title, body and status without overwriting an edited branch or blockers
- [ ] Spawning reads the body from the database, and `gh issue view` is gone from the spawn path
- [ ] The config no longer accepts `[[task]]`, and the thirteen existing rows still reconcile

---

## Phase 8: Withdrawal and editing

**Covers**: retracting a ticket without opening sqlite3, and correcting what the tracker cannot
tell you.

### What to build

Migration `0004_withdrawal.sql` adds `withdrawn_at`. An import that no longer returns a row
previously in its group sets it, which is what moving an issue to `status:backlog` or closing it
now does. The loop and the page read only rows where it is null, so runs, pushes and events stay
attached and no foreign key ever fires. A later import returning the row again clears it.

`POST /ticket` edits `branch` and `blocked_by` on the page, writing an intent row the next tick
applies. Editing a branch on a ticket that already has a worktree is refused rather than applied,
since the worktree and the row would disagree.

### Acceptance criteria

- [ ] Relabelling an issue to `status:backlog` and re-importing withdraws it, and its run history survives
- [ ] A withdrawn ticket disappears from the board and stops being reconciled
- [ ] Relabelling back and re-importing restores it with its history intact
- [ ] Editing blockers on the page changes the unlock decision on the next tick
- [ ] Editing the branch of a ticket that already has a worktree is refused, naming the worktree
- [ ] Nothing in the codebase deletes a row from `tickets`
