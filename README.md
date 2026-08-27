# Command Centre

Local app that drives a DAG of tickets across repos to reviewable pull requests
using agents, one worktree per ticket. It launches agents, watches them, pushes
what they produced, reads CI, and shows one page. It never merges.

```
just run --config docs/command-centre.sample.toml
```

## Status

The single-repo pipeline is built and merged: config, store, tick and page;
unlock, launch preview and consent; cut, spawn, liveness, disposition and crash
recovery; push policy, push and PR create; the CI verdict; and the terminal
states with teardown.

So is the cross-repo work. A repo's `compat_check`, the draft gate that holds a
consumer's PR, the `re-check` verb and the `waiting_on_producer_deploy` state
all have code on main.

So is the operator surface. The board groups rows by blocker, polls itself with
htmx rather than a meta refresh, expands a row into a detail fragment, tails a
run's log over SSE, and puts a confirm page in front of `kill` and
`remove-worktree`.

The newest mechanism is restacking, described below. It has run against real
GitHub through four consecutive squash merges, which is what turned up the four
defects fixed in #92, #94 and #96.

The issue tracker lags the code and is not a todo list. Fourteen issues are
open, and at least seven describe work that has shipped: #74, #75 and #77
landed as #84, #86 and #88, and #53, #55, #56 and #57 all have code on main.
[#85](https://github.com/O-Marsters-1997/command-center/issues/85) is the real
open problem. Two of its four root causes are prose conflicts that no merge
strategy resolves.

One limitation stands. The Mergify app is not installed on `cc-sandbox`, so
`.mergify.yml` is committed there but inert, and the `keep-open` label's
stale-close behaviour stays inspection-only.

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

`--config` defaults to `cc/config.toml`, which is tracked: it is the config this
project runs itself on, and it names its repo by remote, so the same file works
on a machine with nothing checked out. Everything machine-specific is an
environment variable:

| Variable | What |
|---|---|
| `CC_DATA_DIR` | Where `state/` and `repos/` go. |
| `CC_AGENT_COMMAND` | A JSON array replacing `agent_command`, for a local wrapper. |

```
CC_AGENT_COMMAND='["caffeinate","-i","claude","-p","{prompt}"]' just run
```

`docs/command-centre.sample.toml` is the read-only tour instead. It points at
this repository through a relative path and renders two rows off real `gh`
output, one `ready` and one `blocked`:

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

`Loop.RunOnce` in `internal/cc/loop.go` observes, decides, then acts. Observe
runs once; every other step is conditional on observing successfully.

1. Observe: `git fetch origin --prune`, the `gh` PR snapshot, the worktree map,
   each repo's current `sha256(.mergify.yml)`.
2. Tick the checking-waits, save the observation.
3. Apply launch intents, apply cancel intents, apply kill intents.
4. Reconcile runs: liveness, then disposition. Save the observation again,
   carrying this tick's liveness reads.
5. Retarget: reparent merged branches' descendants onto the base their siblings
   landed on, preserving their own squash.
6. Apply abort intents (clear conflicted restacks), apply refresh intents
   (fast-forward and rebase if needed).
7. Apply retry-push intents, push whatever is pushable, record verdict
   transitions.
8. Apply draft gate (hold cross-repo consumers' PRs in draft).
9. Apply re-run intents, apply re-check intents, apply close-pr intents, apply
   remove-worktree intents.
10. Launch whatever is now eligible, capped at `max_agents`.

The period is 15 seconds, slept after the work rather than before, so ticks
never overlap. A failed observe records a tick error and applies nothing, which
leaves the last good facts on the page and lets the observe age keep growing
instead of resetting.

The loop is the only writer of the database. No HTTP handler ever acts on the
world: it writes one intent row, redirects back to the board, and the next
tick reads it.

### Restacking on squash-merge conflict

When GitHub squash-merges a branch, it flattens its commits into one and writes
it to the base branch with no ancestry link to the branch's own history. Merging
the base back into a child branch would therefore replay every commit the base
already carries, conflicting on every touched line.

The app detects this: the refresh phase checks whether the base's recorded
boundary commit (from the merged PR's `headRefOid`, falling back to
`base_sha_at_push`) is still an ancestor of the base's current tip. If it is
not, the app runs `git rebase --onto <base> <boundary>` instead of merging. That
replays only what the branch added after the boundary and drops everything up to
it, because the base already carries that work under new SHAs.

A restack rewrites the branch, so the next push has to force. It may only use
`--force-with-lease`, and only when a `restacked` event was recorded after the
row's last push. A restack that stops on a conflict records one too. It has
already rewritten the branch, so whoever resolves the conflict by hand still
needs the lease. `advanceOnto` and `restackBoundary` in
`internal/cc/refresh.go` are the two functions to read.

### The page

`internal/cc/server.go` serves eleven routes. The shell, the board, the launch
preview, the confirm page and a row's detail fragment are each `html/template`
over an embedded template: `page.tmpl`, `board.tmpl`, `preview.tmpl`,
`confirm.tmpl` and `detail.tmpl`.

| Route | What |
|---|---|
| `GET /` | The page: the header, the liveness banner, the launch form and the board nested inside `<div id="board">`. |
| `GET /board` | The board alone, the same `board.tmpl` fragment `GET /` nests. One row per task, with each row's verbs rendered as forms. |
| `GET /preview` | What launching `?task=...` would do, and why. Where the board's checkboxes submit to, and where `[ authorise ]` posts to `/launch` from. |
| `GET /events` | The append-only audit log, as JSON. |
| `GET /confirm` | The one question a destructive verb asks first: what `?verb=...` does to `?task=...`, and the pgid or worktree path at risk. |
| `GET /task/{task}/detail` | One row's detail as an htmx fragment: the log tail, the check list, the base SHA, elapsed and the worktree path. `{task}` is the ticket URL percent-encoded into a single path segment. |
| `GET /task/{task}/log` | The run's log tail, streamed as JSON over SSE. |
| `GET /assets/htmx.min.js` | htmx, vendored into the binary. The board needs no network. |
| `GET /assets/app.css` | The Tailwind sheet, built from `web/app.css` into `internal/cc/assets/dist/app.css` and committed. The board needs no network. |
| `POST /launch` | Queues one launch intent per task, all sharing one group token so the tick sees one authorisation. |
| `POST /verb` | Queues one verb intent against one task. |

The board polls `GET /board` every five seconds and swaps the table into
`<div id="board">`, so a tick never re-renders the shell. The liveness banner is
an `hx-swap-oob` region of the shell and so only refreshes on a full page load.
Each row's detail sits in a sibling `<tr>` carrying `hx-preserve` and a stable
id, so an expanded row survives the board's own swap.

Both POSTs are wrapped in `requireBrowserOrigin`, which rejects any request
whose `Origin` header does not match `r.Host`.

The verbs offered on the board are launch, kill, re-run, re-check, retry-push,
close-pr, remove-worktree, cancel, refresh and abort. Which verbs a row offers
is `plan.Verbs`, a decision over its state, so it is table-tested.

The states are blocked, ready, queued, running, failed, cut_failed,
push_pending, checking, needs_you, push_failed, review_me, merged,
pr_closed_unmerged, base_gone, cancelled, base_moved, refresh_conflicted and
waiting_on_producer_deploy. None of them is ever stored. Facts are stored and
the label is derived on every render.

### Where state lives

Under `data_dir`, which is the `data_dir` config key, else `CC_DATA_DIR`, else
`$UserConfigDir/command-centre`. It splits in two, so no agent's worktree is one
`../` away from the database:

```
state/command-centre.db      SQLite
state/command-centre.lock    flock, one instance per data dir
state/runs/<id>.jsonl        one per run: agent stdout and stderr, redirected not piped
state/runs/<id>.prompt       the prompt that run was given
state/settings/agent.json    the app-owned deny settings passed to every spawn
repos/<name>/                a repo's checkout, with the worktrees tp cuts beside it
```

The driver is `modernc.org/sqlite`, so no cgo. goose owns the schema from
`internal/cc/migrations/`, embedded in the binary and applied at `OpenStore`.
`0001_init.sql` creates `meta`, `tasks`, `launches`, `launch_members`, `runs`,
`pushes`, `events` and `intents`, all `IF NOT EXISTS`, so a database that
predates goose is adopted rather than rebuilt.

## Configuration

Where the config file sits decides one thing only: what a relative `[[repo]]`
path is relative to.

| Key | What |
|---|---|
| `data_dir` | Where state and checkouts live. Overridden by nothing; falls back to `CC_DATA_DIR`, then `$UserConfigDir/command-centre`. A leading `~` expands. |
| `max_agents` | How many agents may run at once. Default 1. |
| `port` | The page's port. Default 7777. |
| `agent_command` | The argv the runner spawns. Overridden wholesale by `CC_AGENT_COMMAND`, a JSON array.  `{worktree}`, `{settings}`, `{prompt}` and `{prompt_file}` are substituted into every element. |
| `[[task]]` | `ticket_url`, `repo`, `branch`, `blocked_by`. Upserted at startup only, so the tick never adds rows to its own intake table. |
| `[[repo]]` | `name`, then exactly one of `remote` and `path`, plus `stacking`, `mergify_sha`, `deny`, `checks`. |

A repo is located by `remote`, a git URL cloned to `<data_dir>/repos/<name>`, or by `path`,
a checkout that already exists, absolute or relative to the config file's own
directory. Setting both is refused at load. Startup clones when the directory is
absent, and refuses to start when it holds something whose `origin` names a
different repository. The ssh and https forms of one repository count as one.

The app never resets, pulls or checks out a checkout. It fetches and reads
remote-tracking refs; work happens in the worktrees tp cuts beside it.

Give a real agent `{prompt}`, not `{prompt_file}`. Claude Code expands a slash
command only when the prompt arrives as argv text, so a path is read back as
inert text and `/implement` never fires. `{prompt_file}` stays for anything that
would rather take a path.

`checks` is the repo's boolean predicate over its gating checks, evaluated by
`internal/verdict` against a normalised rollup. `mergify_sha` is the
`sha256(.mergify.yml)` that predicate was written against. When the file's hash
stops matching, rows hold at `checking` rather than claim green. `deny` adds
per-repo path patterns to the push policy's default refusals. A repo that
configures neither `checks` nor `mergify_sha` stops at `checking` and never
derives a verdict.

`[[repo]]` also accepts `compat_check`, the name of the check that reports
whether a consumer still builds against its producer.
`internal/cc/draftgate.go` and `internal/cc/verdict_transitions.go` read it.

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
just build       # go build -o bin/cc ./cmd/cc
just run *args   # go run ./cmd/cc
just test        # go test ./...
just test-e2e    # go test -tags=e2e ./e2e/...
just fmt         # go fmt ./...
just tidy        # go mod tidy
just assets      # rebuild the committed app.css (needs bun)
just lint        # golangci-lint in docker
just ci          # conflicts, build, lint, test, e2e
```

Lint is golangci-lint v2: errcheck, govet, ineffassign, staticcheck, unused,
misspell, exhaustive and lll at 120, plus goimports with a local prefix. It runs
with `-tags=e2e` so the harness is linted like everything else. CI runs six
jobs: test with `-race` on ubuntu and macos, e2e on both, lint, build, a
`go mod tidy` diff check, and an `assets` job that rebuilds `app.css` and
diffs it.

`web/app.css` is the Tailwind v4 source. Its `@theme` block holds the colour
tokens and `@layer components` holds hand-written classes for the state
grammar, `pill`, `ribbon`, `meter` and the rest. Nothing links them to the
templates yet. Go emits no utility class anywhere, so nothing depends on
Tailwind's scan and no class can be purged. `just assets` compiles it into
`internal/cc/assets/dist/app.css`, which is committed the same way
`htmx.min.js` is, so `go build` and `just ci` need no bun. Change `web/app.css`
and you must commit the rebuilt output, or the `assets` job fails.

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
