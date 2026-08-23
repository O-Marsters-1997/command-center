# Command Centre

Local app that drives a DAG of tickets across repos to reviewable pull requests
using agents, one worktree per ticket. It launches agents, watches them, pushes
what they produced, reads CI, and shows one page. It never merges.

```
just run --config docs/command-centre.sample.toml
```

## Status

Phases 0 to 6 are built and merged to main. That is config, store, tick and
page; unlock, launch preview and consent; cut, spawn, liveness, disposition and
crash recovery; push policy, push and PR create; the CI verdict; and the
two-ticket slice with terminal states and teardown. Issues #1 to #8 and #10 to
#12 are all closed. `tp remove --force` landed upstream and is what teardown
calls.

One issue is open, and it is a verification run rather than unbuilt code.
[#25](https://github.com/O-Marsters-1997/command-center/issues/25) gathers five
acceptance criteria deferred out of Phases 4, 5 and 6 because none of them can
run in CI or against fixtures. Each needs `cc` running as a real daemon against
the real `O-Marsters-1997/cc-sandbox` repo:

- A real push opens a real PR carrying the `keep-open` label, and the next tick
  adopts it rather than duplicating it.
- A real check rollup goes green, then red when one check is flipped.
- One authorisation produces two open PRs with no further human action.
- A real squash-merge derives `pr_merged`, and `remove worktree` then succeeds.
- Killing the app mid-slice and restarting relaunches nothing and duplicates
  no PR.

The issue records one known limitation. The Mergify app is not installed on the
sandbox, so `.mergify.yml` is committed there but inert, and the `keep-open`
label's stale-close behaviour stays inspection-only.

Two verbs named in the design are deliberately absent. `cancel` is Phase 2.
`refresh` is unreachable while every repo sets `stacking = false`.

## Requirements

- Go 1.26.6, the toolchain pinned in `go.mod`.
- `git`.
- `gh`, authenticated. Every PR read and write shells out to it.
- `tp`, the treepad CLI. It cuts and removes the per-ticket worktrees.

Every configured repo must be squash-only on GitHub. Startup reads
`gh api repos/{owner}/{repo}` and refuses to run if a repo allows merge commits
or rebase merges, because the merge-don't-rebase design rests on GitHub
flattening every merge.

## Run it

`--config` defaults to `.claude/command-centre.toml`, which `.gitignore`
excludes. The committed sample is the copy anyone can run. It points at this
repository and renders two rows off real `gh` output, one `ready` and one
`blocked`:

```
just run --config docs/command-centre.sample.toml
```

The page is then on http://127.0.0.1:7777. The sample sets no `agent_command`,
so it renders the page but cannot spawn anything.

## How it works

`cmd/cc` is a thin main: flags, signal handling, `cc.New`, `app.Run`. Run starts
two goroutines. One is the reconcile loop. One is an HTTP server bound to
127.0.0.1.

### Packages

| Package | What |
|---|---|
| `internal/cc` | The imperative shell. Config, workspace and state dir, SQLite store, the tick, the page, push, verbs, observe, the process runner, the flock. |
| `internal/plan` | The decisions, as pure functions over value types. Unlock, Status, Verbs, LaunchPlan, Preview, Compose and Hash, Disposition, push policy. Stdlib-only, enforced by `api_test.go`. |
| `internal/verdict` | The CI verdict predicate engine. Also pure, also import-checked. |
| `internal/gh` | The only place that knows the `gh` CLI's JSON shape. It normalises the status check rollup before anything else sees it. |
| `internal/tp` | The two `tp` commands the app uses, `new` and `remove --force`. |

### The tick

`Loop.RunOnce` in `internal/cc/loop.go` observes, decides, then acts, in this
order:

1. Observe: `git fetch origin --prune`, the `gh` PR snapshot, the worktree map, each
   repo's current `sha256(.mergify.yml)`.
2. Tick the checking-waits, then save the observation.
3. Apply launch intents, then kill intents.
4. Reconcile runs: liveness, then disposition. Save the observation again, now
   carrying this tick's liveness reads.
5. Apply retry-push intents, push whatever is pushable, record verdict
   transitions.
6. Apply re-run intents, then close-pr, then remove-worktree.
7. Launch whatever is now eligible, capped at `max_agents`.

The period is 15 seconds, slept after the work rather than before, so ticks
never overlap. A failed observe records a tick error and applies nothing, which
leaves the last good facts on the page and lets the observe age keep growing
instead of resetting.

The loop is the only writer of the database. No HTTP handler ever acts on the
world: it writes one intent row, returns 202, and the next tick reads it.

### The page

`internal/cc/server.go` serves five routes. The page itself is `html/template`
over an embedded `page.tmpl`.

| Route | What |
|---|---|
| `GET /` | The page. One row per task, with each row's verbs rendered as forms. |
| `GET /preview` | What launching `?task=...` would do, and why. |
| `GET /events` | The append-only audit log, as JSON. |
| `POST /launch` | Queues one launch intent per task, all sharing one group token so the tick sees one authorisation. |
| `POST /verb` | Queues one verb intent against one task. |

Both POSTs are wrapped in `requireBrowserOrigin`, which rejects any request
whose `Origin` header does not match `r.Host`.

The verbs are launch, kill, re-run, retry-push, close-pr and remove-worktree.
Which of them a row offers is `plan.Verbs`, a decision, so it is table-tested.

The states are blocked, ready, queued, running, failed, cut_failed,
push_pending, checking, needs_you, push_failed, review_me, pr_merged,
pr_closed_unmerged and base_gone. None of them is ever stored. Facts are stored
and the label is derived on every render.

### Where state lives

Outside the workspace, so no agent's worktree is one `../` away from the
database. The state dir is `$UserConfigDir/command-centre/<workspace-name>/`:

```
command-centre.db      SQLite, schema version 1
command-centre.lock    flock, one instance per workspace
runs/<id>.jsonl        one per run: agent stdout and stderr, redirected not piped
runs/<id>.prompt       the prompt that run was given
settings/agent.json    the app-owned deny settings passed to every spawn
```

The driver is `modernc.org/sqlite`, so no cgo. `internal/cc/schema.sql` creates
every table Phases 1 to 6 need in one go: `meta`, `tasks`, `launches`,
`launch_members`, `runs`, `pushes`, `events`, `intents`. There is no migration
code by design, and `OpenStore` refuses a version mismatch.

## Configuration

The workspace root is the config file's grandparent directory, so
`plain/.claude/command-centre.toml` is the workspace named `plain`.

| Key | What |
|---|---|
| `max_agents` | How many agents may run at once. Default 1. |
| `port` | The page's port. Default 7777. |
| `agent_command` | The argv the runner spawns. `{worktree}`, `{settings}` and `{prompt_file}` are substituted into every element. |
| `[[task]]` | `ticket_url`, `repo`, `branch`, `blocked_by`. Upserted at startup only, so the tick never adds rows to its own intake table. |
| `[[repo]]` | `name`, `path` relative to the workspace root, `stacking`, `mergify_sha`, `deny`, `checks`. |

`checks` is the repo's boolean predicate over its gating checks, evaluated by
`internal/verdict` against a normalised rollup. `mergify_sha` is the
`sha256(.mergify.yml)` that predicate was written against. When the file's hash
stops matching, rows hold at `checking` rather than claim green. `deny` adds
per-repo path patterns to the push policy's default refusals. A repo that
configures neither `checks` nor `mergify_sha` stops at `checking` and never
derives a verdict.

`[[repo]]` also accepts `compat_check`, which the config decodes but nothing
reads yet.

## Testing

`go test ./...` is the unit suite. Determinism comes from four seams injected on
`cc.New`: `WithClock`, `WithObserver`, `WithRepoCheck` and `WithRunner`. No unit
test sleeps.

`e2e/` is a `rogpeppe/go-internal` testscript harness behind `-tags=e2e`,
driving the real `cc` binary against a fake `gh`, a fake `tp` and fake agent
scripts. `cc tick --count N` and `cc request` are its two e2e-only subcommands,
kept out of the release binary by the build tag. See
[e2e/README.md](e2e/README.md).

## Commands

```
just build       # go build -o cc ./cmd/cc
just run *args   # go run ./cmd/cc
just test        # go test ./...
just test-e2e    # go test -tags=e2e ./e2e/...
just fmt         # go fmt ./...
just tidy        # go mod tidy
just lint        # golangci-lint in docker
just ci          # conflicts, build, lint, test, e2e
```

Lint is golangci-lint v2: errcheck, govet, ineffassign, staticcheck, unused,
misspell, exhaustive and lll at 120, plus goimports with a local prefix. It runs
with `-tags=e2e` so the harness is linted like everything else. CI runs five
jobs: test with `-race` on ubuntu and macos, e2e on both, lint, build, and a
`go mod tidy` diff check.

## Docs

| File | What |
|---|---|
| [docs/prds/prd-command-centre.md](docs/prds/prd-command-centre.md) | What we're building and why. Start here. |
| [docs/designs/command-centre-design.md](docs/designs/command-centre-design.md) | The design, revision 4. Normative for mechanism, states and invariants. |
| [docs/prds/prd-operator-surface.md](docs/prds/prd-operator-surface.md) | The page and the operator surface. Part A lands inside Phase 1. |
| [plans/command-centre-phase-1.md](plans/command-centre-phase-1.md) | The Phase 1 build plan: module boundaries, schema, testing strategy. |
| [docs/command-centre.sample.toml](docs/command-centre.sample.toml) | A runnable config, pointed at this repository. |
| [e2e/README.md](e2e/README.md) | The end-to-end harness: scripts, fakes, fixtures, and how to add one. |

### Reviews

| File | What |
|---|---|
| [phase-3-code-structure-review.md](docs/architecture-reviews/phase-3-code-structure-review.md) | Review of the built code at end of Phase 3. Module boundaries and file structure. Its docs tree is the pre-reorganisation one. |
| [revision-3-foundations-review.md](docs/architecture-reviews/revision-3-foundations-review.md) | Staff review of revision 3. Drove revision 4. Nine must-fixes. |
| [revision-2-buildability-review.md](docs/architecture-reviews/revision-2-buildability-review.md) | Staff review of revision 2. Four of its six must-fixes are dissolved by revision 3, see §15. |
| [revision-2-assessment.md](docs/architecture-reviews/revision-2-assessment.md) | Verdict summary, genre comparison, adaptability. Written against revision 2. |
