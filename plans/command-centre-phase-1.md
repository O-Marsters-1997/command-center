# Plan: Command Centre — Phase 1 (one repo, a slice of two)

> Source: [`docs/prd-command-centre.md`](../docs/prd-command-centre.md) § Phases → Phase 1.
> Mechanism, state list and invariants are normative in
> [`docs/command-centre-v1.md`](../docs/command-centre-v1.md) (revision 4) — cited as `§n` and
> `inv. n` below. Where this plan and the design disagree, the design wins.

Phase 1 goal: **one repo, `max_agents = 1`, `stacking = false`, a two-ticket slice with one
blocking the other, and the tick's job 3 firing exactly once — when the first ticket's PR
opens.** It is the plumbing, not the payoff. Phase 2 (fan-out, stacked bases) and Phase 3
(seams) are out of scope and named in each phase where the seam is left visible.

**Phase 1 does not touch a team repo and does not pick up real work.** It runs against a
private sandbox repo and fake agent scripts — see the next section. The PRD names `support-app`
as Phase 1's repo; that becomes the **cutover** target, and the two spikes the PRD lists as
entry criteria move to the cutover checklist at the end of this plan.

## Technical design decisions

### Target repo and agent substitution

The app is repo-agnostic by construction: a repo reaches it only as config (`path`, `deny`,
`[repo.checks]`, `compat_check`), and the agent reaches it only as a command line. Both are
substituted for Phase 1.

**The target is a private sandbox repo** (`cc-sandbox` below) that mirrors the properties the
app's mechanism depends on, so `internal/gh`, the push policy and `internal/verdict` are
exercised against real `gh` and real GitHub rather than a mock:

| Sandbox setting | Why |
|---|---|
| `.mergify.yml` copied from `support-app`, including the 14-day stale-close rule with the `keep-open` escape | the file the predicate is transcribed from, and the hash the staleness detector records (§7) |
| one workflow emitting the same gating check names — `Lint`, `Typecheck`, `Tests`, `Generated files`, `GraphQL production compatibility`, plus a job named `verify / Linear issue is linked` — each flippable green/red/skipped from a committed file | the predicate's real input, on demand, with no external app installed |
| a path-filtered job mirroring `Lint GitHub Actions / Lint` | the only way to exercise `absent_ok` for real |
| squash-only merges, delete-branch-on-merge, branch protection off | matches both real repos' server-enforced settings (§11) |
| a `deny` list mirroring `support-app`'s, plus a few files that exist in the sandbox | the push policy refuses on real paths |

Copying check names and `.mergify.yml` means *reading* `support-app`; nothing is pushed to it,
no PR is opened on it, and no agent is pointed at it.

**Agents are scripts, not `claude -p`.** Config carries `agent_command` (an argv template
receiving the worktree path, the settings path and the composed prompt). Phase 1 ships three
scripts under `testdata/agents/`: one that commits a file and exits 0, one that exits 0 having
committed nothing, one that sleeps until killed. That is what makes liveness, disposition,
`kill` and crash-recovery deterministic, free and fast; `agent_command = "claude -p …"` is the
one-line edit at cutover. The runner still builds and passes the app-owned `--settings` file
on every spawn — a fake agent doesn't obey it, so that boundary is asserted structurally in
Phase 3 and behaviourally at cutover.

### Config — `plain/.claude/command-centre.toml`

Single user-edited TOML file, path from `--config` (already flagged in `cmd/cc/main.go`).
Shape per design §8, plus Phase 1 intake:

```toml
max_agents    = 1
port          = 7777
agent_command = ["testdata/agents/commits.sh", "{worktree}", "{settings}", "{prompt_file}"]
                                      # cutover: ["claude", "-p", "--settings", "{settings}", …]

[[task]]                              # Phase 1 intake; Phase 2's to-tickets writes the table
ticket_url = "sandbox://CC-1"          # any stable string; a Linear URL at cutover
repo       = "cc-sandbox"
branch     = "cc-1-first"
blocked_by = []

[[task]]
ticket_url = "sandbox://CC-2"
repo       = "cc-sandbox"
branch     = "cc-2-second"
blocked_by = ["sandbox://CC-1"]

[[repo]]
name         = "cc-sandbox"
path         = "cc-sandbox"           # relative to the workspace root
stacking     = false                  # Phase 2 flips it
compat_check = "GraphQL production compatibility"
mergify_sha  = "sha256:…"             # hash the predicate was written against (§7)
deny         = ["scripts/**", "codegen.yml", "next.config.js",
                "vite.config.ts", "eslint-ci.config.ts"]
  [repo.checks]                       # the boolean predicate, §8's grammar
  all_of = [ … ]
```

The `[[repo]]` block is `support-app`'s, with `name`/`path` pointed at the sandbox — so cutover
is two string edits plus `agent_command`, not a new code path.

`[[task]]` rows are **upserted on `ticket_url` at startup only** — not per tick, so the tick
still never adds rows to its own table. Editing tasks means restarting the app.

### On-disk layout

```
~/Library/Application Support/command-centre/<workspace>/     mode 0700
  command-centre.db        SQLite; the flock target
  runs/<run-id>.jsonl      agent stdout, one file per run, redirected never piped
  settings/agent.json      the app-owned deny settings passed to every spawn (inv. 17)
```

Deliberately **not** under `plain/` — worktrees are siblings of `plain/.claude/`, which would
put the consent table one `../` from every agent (§8).

### Schema (SQLite, WAL, `busy_timeout=5000`)

```
tasks           ticket_url PK · repo · branch · blocked_by (JSON) · seams (JSON, unused P1)
launches        id · created_at · state (active|done|cancelled)
launch_members  launch_id · task_id · prompt_hash          ← consent bound to content (§4b)
runs            id · task_id · kind · pgid · proc_started_at · baseline_sha ·
                prompt_hash · log_path · outcome · exit_code · ended_at
pushes          id · task_id · pushed_tip · base_branch · base_sha_at_push · pushed_at
events          id · at · task_id · kind · detail          ← append-only audit
intents         id · at · task_id · verb · payload · consumed_at
meta            schema_version                              ← refuse to start on mismatch
```

**No `status` column** — status is derived (§5). `launches`, `launch_members`, `runs` and
`pushes` are the only tables holding facts the world cannot reconstruct.

### Module boundaries

| Package | Owns | Exposes | Isolated tests |
|---|---|---|---|
| `internal/plan` | unlock, status derivation, dead-run disposition, push plan + policy match, launch eligibility | pure functions over value types | **yes — the bulk of the suite** |
| `internal/verdict` | predicate evaluation over a normalised check snapshot | `Evaluate(Predicate, Input) Result` | **yes — table-driven, both repos' predicates as fixtures** |
| `internal/gh` | the only place that knows `gh`'s JSON shape; normalises the rollup before anything reads it | `Snapshot`, `PR`, `CheckState`, `List`, `CreatePR` | yes — decode + normalise from recorded JSON fixtures |
| `internal/tp` | the two treepad commands | `New(branch, baseRef)`, `Remove(branch, force)` | thin; exercised via the loop |
| `internal/cc` | the imperative shell: `store` (SQLite), `git` (exec'd git reads/writes), `loop` (observe→decide→act), `runner` (spawn/kill), `http` (page, preview, verbs) | one `App` assembled in `cmd/cc` | store and git yes; loop via a seam over the observe result |

`internal/plan` and `internal/verdict` import no `os/exec`, no `database/sql`, no `net/http`,
no `internal/cc` — enforced by an `api_test.go` that fails the build on a forbidden import,
the way treepad guards `batch/` (§8). `internal/gh` is the anti-corruption layer; nothing
else decodes gh output.

### Interface contracts (boundary level)

```go
// internal/gh — normalised, latest completed run per check name (§3.2)
type PRState int // Open, Merged, Closed, Absent
type CheckState struct { Name, Status, Conclusion, DetailsURL string; StartedAt time.Time }
type PR struct {
    Number int; HeadRef, HeadOid, BaseRef, BaseOid, AuthorLogin string
    IsDraft bool; State PRState; Checks map[string]CheckState
}
type Snapshot struct { ByBranch map[string]PR; ReadAt time.Time }

func List(ctx context.Context, repoPath string, tracked []string) (Snapshot, error)
func CreatePR(ctx context.Context, repoPath, head, base, bodySuffix string) (int, error) // adopts
func ClosePR(ctx context.Context, repoPath string, number int) error
```

```go
// internal/plan — pure
type Task struct { TicketURL, Repo, Branch string; BlockedBy []string }
type Unlock struct { Unlocked bool; BaseBranch, Reason string }
func Unlocked(t Task, byURL map[string]Task, prs map[string]gh.PRState, stacking bool) Unlock

type Facts struct { /* task, unlock, launch membership, latest run, latest push, verdict */ }
func Status(f Facts) (State, Reason)

type Outcome int // OutcomePush, OutcomeFailed
func Disposition(commitsSinceBaseline int) Outcome

type Policy struct { Deny []string }              // globs; repo deny + the default set
func Refused(changed []string, p Policy) []string // offending paths, empty = clear to push
func PushPlan(b []BranchFacts, p map[string]Policy) []PushAction
func LaunchPlan(f []Facts, maxAgents int) []string // ticket_urls to cut+spawn this tick
```

```go
// internal/verdict — pure
type Predicate struct { AllOf, AnyOf []Predicate; Not *Predicate; Success, Skipped, AbsentOK string }
type Input struct {
    Checks         map[string]gh.CheckState
    HeadOidMatch   bool          // rollup head == the tip we pushed
    StackedBase    bool          // false for every Phase 1 row
    BaseSHAMatch   bool          // only consulted when StackedBase
    ConfigHashOK   bool          // .mergify.yml hash still matches
    PushedAt, Now  time.Time     // the bounded wait
    AuthorLogin    string        // the dependabot arm of services' Linear or
}
type Result struct { Verdict int; Reason string } // ReviewMe | NeedsYou | Checking
func Evaluate(p Predicate, in Input) Result
```

### Routes (`127.0.0.1:<port>`, no auth — inv. 18)

| Route | Method | Does |
|---|---|---|
| `/` | GET | the one table: one row per task, state, reason, verbs as forms, plus worktree path, pgid, elapsed, base, tick age, last tick error |
| `/preview` | GET | the launch preview for `?task=<url>&task=<url>` — now / on-unlock / refusing, each with its base and reason; composes and hashes each prompt |
| `/launch` | POST | authorise: inserts a `launches` row + members with the previewed hashes (as an intent) |
| `/verb` | POST | one handler, form fields `verb` + `task` — `kill`, `re-run`, `retry-push`, `close-pr`, `remove-worktree`; inserts an `intents` row |

Every mutating route is POST-only and rejects a request whose `Origin` is absent or
mismatched or whose `Host` is mismatched. Handlers **only** insert intents — the loop is the
sole DB writer (inv. 9), which is what makes verbs race-free against a launch.

### Integration points

`git` CLI (fetch/worktree list/log/rev-parse/push), `gh` CLI (PR reads, create, close),
`tp` CLI (`new --base`, `remove --force`), `claude -p` (spawned with `Setpgid`, output
redirected, `--settings` pointing at the app-owned deny file, no `ANTHROPIC_API_KEY`
inherited, no `--bare`). Libraries: `modernc.org/sqlite` (pure Go, no cgo),
`github.com/BurntSushi/toml` (treepad's TOML choice — same workspace, same parser),
`golang.org/x/sys/unix` for `Flock`, `golang.org/x/sync/errgroup`, stdlib `html/template`, and
`github.com/rogpeppe/go-internal/testscript` for e2e (test-only, treepad's harness).

### Go conventions

Non-negotiable, and enforced by the linter already configured in `.golangci.yml` (`errcheck`,
`govet`, `staticcheck`, `unused`, `ineffassign`, `misspell`, `exhaustive`, `lll` at 120,
`goimports` with the local prefix). Where the linter cannot judge:

- **Layout.** `cmd/cc/main.go` is wiring only — flags, config, state dir, flock, store, then
  assemble and run. No business logic, no package-level state, no `init()` side effects, no
  `pkg/` directory. Everything else is `internal/`; fixtures and golden files live in
  `testdata/` beside the package that reads them.
- **Dependencies passed explicitly** through constructors — `NewStore(db)`, `NewLoop(...)`,
  `NewServer(...)`. No global `*sql.DB`, no ambient config. Where an argument list would exceed
  ~4, use functional options (`NewLoop(store, observer, opts ...Option)` with `WithClock`,
  `WithTickPeriod`, `WithMaxAgents`) so a Phase 2 knob is a backward-compatible addition rather
  than a positional break.
- **A `Clock` seam** (an interface or a `func() time.Time` option) on anything consulting time:
  the tick period, elapsed time, and `verdict`'s bounded wait. No test sleeps, ever.
- **Typed enums, not strings**, for `State`, `Outcome`, `PRState`, `Verdict`, `Reason`, with
  `String()` for rendering — `exhaustive` then makes an unhandled new state a build failure,
  which is the point of adding states in Phase 2.
- **Absence is a value, not a nil pointer.** `PRState` carries `Absent`; `verdict.Result` and
  `plan.Unlock` are returned by value. `*T` never means "might not be set".
- **Errors** wrap with `%w` and lowercase context (`fmt.Errorf("fetch origin: %w", err)`).
  Sentinels for what the loop branches on (`ErrObserveFailed`); a typed error where the caller
  needs the detail (`*plan.RefusedError` carrying the offending paths, matched with
  `errors.As`). No `log.Fatal` outside `main`; the loop records to `events` and continues.
- **Two goroutines only** — the loop and the HTTP server — supervised by `errgroup` under the
  `signal.NotifyContext` already in `main.go`. No per-run goroutine (§3).
- **Return early, no `else` after `return`, `switch` over `if`/`else if` chains, `range` for
  iteration.** Comments explain *why* only: an external constraint, or the invariant a piece of
  code implements (`// inv. 10: a failed read applies no transition`).

### Testing strategy

Three layers, each with a job the others cannot do.

**1 · Table-driven unit tests** — the default for every new function, `t.Run` subtests with
`t.Parallel()` where independent, helpers marked `t.Helper()`, `t.TempDir()` for filesystem
work, no `time.Sleep`. This layer carries the whole of `internal/plan` and `internal/verdict`
(pure, import-guarded), `internal/gh`'s decode-and-normalise from recorded JSON in
`internal/gh/testdata/`, the store against a temp-file SQLite DB, and the HTTP surface through
`httptest` — including the `Origin`/`Host`/POST-only rejections. The page's HTML is a **golden
file** under `testdata/` with an `-update` flag, so a render change is a reviewed diff.
`go test -race ./...` is the command that counts.

**2 · End-to-end tests — treepad's harness, mirrored exactly.** Same library, same layout,
same build tag, same commands:

```
e2e/
  testscript_test.go        //go:build e2e — TestMain builds the binaries, TestScripts runs the dir
  register/register.go      the e2e-only command registration, imported by cmd/cc/register_e2e.go
  fakegh/main.go            the fake `gh`
  faketp/main.go            the fake `tp`
  agents/{commits,empty,sleeps}.sh
  tests/*.txtar             one script per scenario
```

`TestMain` builds `cc` with `-tags=e2e` plus `fakegh` and `faketp` into one temp dir and
prepends it to `PATH` — treepad's exact move (`e2e/testscript_test.go`), and the reason the
fakes are Go programs rather than shell: they are compiled and cross-platform, matching the CI
matrix. `Setup` redirects `HOME` **and** the app's state-dir root to `env.WorkDir`, so the DB,
run logs and settings file land inside the script's sandbox. Custom `Cmds` mirror
`tp-init-repo`: `cc-init-repo` (git init, first commit, a `.treepad.toml`), `cc-config`
(write `command-centre.toml` with the script's paths), `cc-fake-gh` (stage the fixture the
fake `gh` will answer with — including a non-zero exit for the failed-read case).

Real `git` throughout, as treepad does. The fakes exist because the alternative is network and
non-determinism:

| Fake | Behaviour | What it makes assertable |
|---|---|---|
| `fakegh` | answers `pr list`/`pr create`/`pr close`/`pr edit` from the staged fixture; appends every argv to `$CC_GH_LOG`; exits non-zero when the fixture says so | inv. 10 (a failed read changes nothing), inv. 20 (an existing PR is adopted, never duplicated), `--label keep-open` and `--base` actually passed, `push failed` |
| `faketp` | `new` delegates to `git worktree add`; fails when `$CC_TP_FAIL` is set; `remove` requires `--force` on a squash-merged branch | `cut failed`, teardown, inv. 5 (the surface really is two commands) |
| `agents/*.sh` | commit-and-exit, exit-empty, sleep-with-a-child | liveness, disposition, `kill`'s `-pgid` path, crash recovery |

**The e2e build tag is also the seam that makes a daemon scriptable**, mirroring
`cmd/tp/register_e2e.go` → `e2e/register`: under `-tags=e2e` only, `cc` registers
`cc tick --count N` (run observe → decide → act exactly N times, no sleeping, exit) and
`cc request <method> <path> [k=v…]` (an HTTP client that sets a valid `Origin` and prints the
response body to stdout). Scripts then read like treepad's:

```
cc-init-repo
cc-config two-tasks
cc-fake-gh no-prs
exec cc tick --count 1
exec cc request GET /
stdout 'CC-1.*ready'
stdout 'CC-2.*blocked'
```

Neither command exists in the release binary, so the production surface stays the page plus
the loop.

**3 · Manual sandbox runs.** The handful of assertions only real GitHub can settle — a real
rollup's shape, `--label keep-open` surviving, squash-merge deleting the branch, a genuine
`review me`. These are the criteria marked *(sandbox)* below; everything else is layer 1 or 2
and runs in CI.

`just test-e2e` → `go test -tags=e2e ./e2e/...`, added to `just ci` after `just test` exactly
as treepad orders it, and to `.github/workflows/ci.yml` as its own job (plus `-race` on the
unit job).

### Key flows

- **Tick (~15s, sleep-after-work, never overlapping).** observe → decide → act, never
  branching on why it woke. **A non-zero `git fetch` or any `gh` read ends the tick before
  anything changes** (inv. 10): no transitions, no launches, the error into `events`, tick age
  and last error onto the page. An empty PR list is data only from a zero-exit call.
- **Liveness.** `kill(-pgid, 0)` **plus** a process-start-time identity match. No timing rule
  ever marks a run dead (inv. 6).
- **Disposition.** Commits after *that run's* `baseline_sha` → `outcome = push`; none →
  `outcome = failed`. Recorded as data, never inferred from missing events (inv. 7).
- **Push as reconciliation**, not an event (§3.8): every app-owned branch whose local tip
  differs from its last pushed tip and whose diff against its base clears the repo's policy.
  One code path delivers a dead run's commits, a re-run's commits and (Phase 2) a refresh's
  merge commit.
- **Launch eligibility** (inv. 8): unlocked **and** in an `active` launch **and** the
  recomposed prompt still hashes to what was authorised **and** no prior run **and** under
  `max_agents` (global, applied after all repos' unlock results are in).

### Deliberately unreachable in Phase 1

`stacking = false` and verdict expiry scoped to stacked bases make `base moved`, `refresh` and
`refresh conflicted` genuinely unreachable — do not build them. `waiting on producer deploy`
needs seams (Phase 3). `cancel launch` is Phase 2; Phase 1 ships no `cancel` verb, so a
`queued` row's only exit is its blocker's PR opening or a restart.

---

## Phase 0: The sandbox repo, the fake agents, and the treepad flag

**User stories**: PRD § Phase 1 → Entry criteria (re-scoped to the sandbox); § Dependencies
(`tp remove --force`); § Phase 1 requirement "one configured repo".

### What to build

No app code — the fixtures everything after this asserts against.

**The sandbox repo.** A private GitHub repo with `.mergify.yml` copied from `support-app`
(keep-open rule included), a `.treepad.toml` so `tp new` works in it, squash-only merges,
delete-branch-on-merge, no branch protection, and one workflow whose jobs are named exactly
like `support-app`'s gating checks. Each job's conclusion is driven by a committed control
file (e.g. `ci/expect.json` read by the workflow), so any check can be made green, red or
skipped by a commit — including the path-filtered job that must sometimes be **absent**. Some
real content to diff against: files matching the deny list (`scripts/`, a nested
`packages/*/package.json`, `.npmrc`) and ordinary application files that must push cleanly.

**The fake agents.** `testdata/agents/{commits,empty,sleeps}.sh` — commit a file and exit 0;
exit 0 having committed nothing; sleep until signalled (and, for the kill test, spawn a child
process so the `-pgid` path is real rather than incidental).

**The treepad flag.** Expose `--force` on `tp remove`. The plumbing exists behind an internal
bool (`lifecycle.go:242-247` builds `git worktree remove --force` and `git branch -D`); only
the CLI flag registration is missing (~5 lines). `merge_method: squash` means `git branch -d`
refuses *every* merged ticket, so Phase 6's teardown blocks on it.

The PRD's two entry spikes are **not** here: both measure `support-app`/`services` Mergify
behaviour, so they move to the cutover checklist at the end of this plan (the queue half is
already a pre-Phase-2 item per §13.9).

### Acceptance criteria

- [ ] `gh pr create` in the sandbox opens a PR whose rollup carries all six gating check names.
- [ ] Editing the control file makes a named check go red, then green, then skipped, and makes
      the path-filtered check absent — each verified in the rollup JSON.
- [ ] Merging a sandbox PR squashes and deletes the branch; a PR left untouched is subject to
      the stale-close rule unless labelled `keep-open`.
- [ ] `tp new <branch> --base origin/main` succeeds inside the sandbox repo.
- [ ] `tp remove --force <branch>` removes a worktree whose branch was squash-merged, and
      `tp remove` without the flag still refuses.
- [ ] Each fake agent script does what its name says when run by hand in a worktree; the
      sleeper leaves a child process in its own pgid.
- [ ] `e2e/fakegh` and `e2e/faketp` build and behave per the table above when run by hand:
      `fakegh` answers from a staged fixture and logs its argv, `faketp new` delegates to
      `git worktree add`, `faketp remove` refuses a squash-merged branch without `--force`.
- [ ] The `.env` lines are deleted from both repos' `.treepad.toml` `[sync] include`
      (§10 — a no-op today, a latent secret copy tomorrow).

---

## Phase 1: Skeleton tracer — config, store, tick, page

**User stories**: PRD § The tick (jobs 1 and 2, fail-closed); § The page ("tick age and the
last tick error"); § Concepts → A task; § The app crashes while agents are working (the flock
half).

### What to build

The end-to-end spine with only observe doing real work. `cmd/cc` reads the config, resolves
the state dir, takes the flock on the DB path (a second instance exits with a clear message),
opens SQLite, applies the schema and refuses to start on a `meta.schema_version` mismatch,
then upserts `[[task]]` rows on `ticket_url`.

The loop runs observe → decide → act on a ~15s sleep-after-work period. Observe does
`git -C <repo> fetch origin --prune`, then the PR snapshot through `internal/gh`: one bulk
`gh pr list --state open --limit 100 --json number,headRefName,headRefOid,baseRefName,baseRefOid,isDraft,state,statusCheckRollup`
per repo, plus one `gh pr list --state all --head <branch> --json number,state,baseRefName,headRefOid`
per tracked branch absent from the bulk result — that fallback is the only thing that sees
MERGED and CLOSED. `internal/gh` reduces the rollup to latest completed run per check name and
collapses `CheckRun`/`StatusContext` into one `CheckState` before anything else sees it. It
also reads `git worktree list --porcelain` as the authoritative branch → path map (inv. 5).

Decide runs only `plan.Unlocked` and `plan.Status` over the snapshot, so rows render `blocked`
or `ready`. Act does nothing yet. **A failed fetch or gh read ends the tick before decide**,
records the error in `events`, and leaves the last good snapshot in place.

The page serves the table with state, reason, branch, worktree path and — as first-class
fields, not polish — **tick age and the last tick error**.

This phase also stands up the **e2e harness** itself, because the first txtar script needs it:
`e2e/testscript_test.go` with the build-tag `TestMain`, the `register` package behind
`cmd/cc/register_e2e.go` exposing `cc tick --count N` and `cc request`, the `cc-init-repo` /
`cc-config` / `cc-fake-gh` commands, `just test-e2e`, its line in `just ci`, and the CI job.

### Acceptance criteria

- [ ] `just run` with a two-task config renders both rows at `127.0.0.1:7777`; the root shows
      `ready`, the dependent shows `blocked` with a reason naming its blocker.
- [ ] `just test-e2e` passes and is wired into `just ci` and `.github/workflows/ci.yml`; the
      unit job runs `-race`.
- [ ] `e2e/tests/page_derives_states.txtar`: one tick against a no-PRs fixture, then
      `cc request GET /` shows `ready` and `blocked` with the blocker named.
- [ ] `e2e/tests/fail_observe_stops_tick.txtar`: with the fake `gh` exiting non-zero, no row
      changes state, the page shows the error, and tick age grows — inv. 10, asserted end to
      end rather than only in a unit test.
- [ ] `e2e/tests/fail_second_instance.txtar`: a second `cc` against the same DB exits non-zero
      naming the flock.
- [ ] The page render is covered by a golden file under `testdata/`, regenerated with
      `-update`.
- [ ] A second `cc` against the same DB exits non-zero naming the flock holder's situation.
- [ ] A `meta.schema_version` mismatch refuses to start rather than migrating silently.
- [ ] `plan` and `verdict` unit tests run with `t.Parallel()` and no `time.Sleep`; the clock is
      injected.
- [ ] Re-running with an edited `[[task]]` block updates the existing row — no duplicate
      `tasks` row for the same `ticket_url`.
- [ ] With `gh` forced to fail (PATH shim or a bad repo path), the tick records the error,
      derives no new state, and the page shows a growing tick age plus the message.
- [ ] `internal/gh` normalisation is table-tested from recorded JSON: one check name appearing
      five times at one head SHA with mixed `CANCELLED`/`SUCCESS` reduces to the latest run; a
      `StatusContext` entry with no `name` does not panic; an empty rollup is empty, not green.
- [ ] `internal/plan`'s `api_test.go` fails the build if `os/exec`, `database/sql` or
      `net/http` is imported.

---

## Phase 2: Unlock, the launch preview, and consent

**User stories**: PRD § Unlock; § A launch ("The app shows the plan first", "Authorising binds
consent to content"); § The states (the unlocked × authorised 2×2); § The page ("Why a
`queued` row is queued").

### What to build

`plan.Unlocked` in full, over **stacking edges only** (same-repo blockers; a cross-repo edge
never affects unlock or the base): no blockers → unlocked, base `main`; one blocker with an
OPEN PR (drafts count — the branch exists) → unlocked, base = the blocker's branch when the
repo's `stacking` is on and `main` when it is off; one blocker MERGED → unlocked, base `main`
(the branch is deleted on merge); two or more blockers → unlocked only when every one is
MERGED, base `main`. It returns `{unlocked, base_branch, reason}` and the reason is what the
page renders. `base_branch` is a **branch name**; `tp new` gets `origin/<base_branch>` and
`gh pr create --base` gets the bare name.

`GET /preview?task=…` composes each task's prompt — `/implement <ticket-url>` plus (Phase 3)
its seam files — renders what starts now, what starts on unlock and off what base, and what is
refused and why, and carries `hash(composed)` per row. `POST /launch` queues an intent; the
next tick inserts the `launches` row and its `launch_members` with those hashes and writes an
`events` entry. Rows now derive `queued`, and a queued row says whether it waits for its
**base** or for a **slot** — hours-or-forever versus seconds must not render alike.

Nothing spawns in this phase.

### Acceptance criteria

- [ ] `plan.Unlocked` is table-tested across all five arms above, with `stacking` on and off,
      and with a cross-repo edge present that changes neither the unlock nor the base.
- [ ] The preview for the two-ticket slice reads "now" for the root and "on unlock" for the
      dependent, both bases `origin/main` (stacking off), and refuses a task whose blocker is
      outside the slice and has no PR, naming that blocker.
- [ ] Authorising stores one `launches` row and two `launch_members` with non-empty distinct
      `prompt_hash`es; both rows then read `queued` with distinct reasons ("waiting on PLA-12's
      PR" vs "waiting for a slot").
- [ ] A `POST /launch` with a missing or foreign `Origin` is rejected, as is a `GET` to it —
      table-driven through `httptest`, one case per rejection reason.
- [ ] `e2e/tests/launch_preview_authorise.txtar`: `cc request GET /preview?task=…&task=…` shows
      "now" and "on unlock"; `cc request POST /launch` then one tick leaves both rows `queued`
      with distinct reasons.
- [ ] `e2e/tests/fail_launch_bad_origin.txtar`: a `POST` with a foreign `Origin` is rejected
      and no `launches` row appears.
- [ ] Editing the composed prompt's input after authorisation makes the recomposed hash differ
      from the stored one — asserted directly, since Phase 3 is what makes it reachable in
      practice.

---

## Phase 3: Cut, spawn, liveness, disposition, crash recovery

**User stories**: PRD § A run; § The agent dies having written nothing; § The agent edits the
CI config (boundary 1); § The app crashes while agents are working; § The states (`running`,
`failed`, `cut failed`).

### What to build

The tick's job 3, for one agent. `plan.LaunchPlan` selects tasks that are unlocked, in an
active launch, whose recomposed hash matches, with no prior run, under `max_agents` (global).
Act then writes the intent, cuts with `tp new <branch> --base origin/<base_branch>` — a
failure is `cut failed`, not a crash — and spawns.

The runner writes the app-owned settings file once (`permissions.deny` covering `git push`,
all of `gh`, and network fetch tools — deny beats the repos' tracked and synced allows), then
spawns `agent_command` with `Setpgid`, stdout and stderr **redirected to
`runs/<run-id>.jsonl`**, never piped, `ANTHROPIC_API_KEY` stripped from the environment, and
no `--bare` in the cutover template. In Phase 1 `agent_command` is a fake script; the settings
file is still written and passed on every spawn, so cutover changes the argv and nothing else.
It records the `runs` row — `pgid`, `proc_started_at`, `baseline_sha` (the branch tip at launch), the
`prompt_hash` it actually ran, and `log_path` — before the process is the only record of
itself.

Observe gains liveness per running task: `kill(-pgid, 0)` plus the process-start-time match.
Alive is `running` whatever the log says. Decide gains `plan.Disposition` over commits after
*this run's* baseline, written to `runs.outcome` with `exit_code` and `ended_at`. No commits →
`failed`, with the log path on the row as copy-pasteable text; commits → `outcome = push`,
which Phase 4 consumes.

The `kill` verb signals `-pgid` SIGTERM then SIGKILL via a custom `Cancel`, because
`exec.CommandContext` signals one pid and `claude -p` spawns tool subprocesses that would
orphan holding the worktree — which is why the sleeper fixture spawns a child, so the pgid
path is genuinely exercised. Restart recovery: take the flock, re-read `runs`, re-check each
with the same pid+start-time test, and carry on mid-run — a re-attached process is not a
child, so the tick polls it exactly as it polls a fresh one. Worktree existence comes from
`git worktree list`, never inferred from run rows (inv. 20).

### Acceptance criteria

- [ ] Authorising the two-ticket slice launches only the root; the dependent stays `queued`.
- [ ] The row shows `running` with elapsed time, pgid and worktree path as plain text; the
      agent's output lands in `runs/<run-id>.jsonl` and the file grows.
- [ ] Every spawn is passed `--settings <path>` and that file's `permissions.deny` covers
      `git push`, all of `gh` and the network fetch tools; the spawn's environment carries no
      `ANTHROPIC_API_KEY`. Structural in Phase 1 (a fake agent doesn't obey the file) — the
      behavioural check is on the cutover list.
- [ ] The `empty.sh` agent derives `failed`, and its `runs` row holds
      `outcome`, `exit_code` and `ended_at`; the dependent still reads `queued` with the
      blocker named, not `blocked`.
- [ ] The `commits.sh` agent derives `outcome = push` against **its own** baseline — a previous
      run's commits on the same branch do not make a second run look successful.
- [ ] `kill` on a live `sleeps.sh` row terminates it *and* its child (no process in the pgid
      survives), and the row moves to `failed` or a push-pending state per its commits.
- [ ] Killing the app with agents live leaves them running; on restart the rows are `running`
      again with the same pgids, and no second process is spawned for them.
- [ ] `tp new` failing (unresolvable base) derives `cut failed` with `re-run` offered, and no
      `runs` row claims a pgid.
- [ ] `e2e/tests/spawn_and_dispose.txtar`: three runs of the same script sequence with
      `agent_command` pointed at `commits.sh`, `empty.sh` and `sleeps.sh` — the first derives a
      push-pending state, the second `failed`, the third `running` until `cc request POST /verb
      verb=kill` empties the pgid.
- [ ] `e2e/tests/fail_cut.txtar`: with `$CC_TP_FAIL` set, the row derives `cut failed` and the
      fake `tp` log shows exactly one `new` attempt.
- [ ] `e2e/tests/crash_recovery.txtar`: a live `sleeps.sh` run, the app stopped and restarted,
      the row still `running` with the same pgid and the fake agent log showing one spawn.

---

## Phase 4: Push policy, push, PR create

**User stories**: PRD § The agent edits the CI config (boundary 2); § The last twelve steps
are the same every time; § The states (`checking`, `push failed`, `needs you` for a refused
push).

### What to build

`plan.PushPlan` over every app-owned branch whose local tip differs from its last `pushes`
row's tip. For each, the changed paths against the branch's base are matched by
`plan.Refused` against the repo's policy — the default set for every repo (`.github/**`,
`.mergify.yml` at the repo root, `CODEOWNERS`, `**/package.json`, lockfiles,
`pnpm-workspace.yaml`, `.npmrc`, `.env*`) plus the per-repo `deny` additions. **A hit refuses
the push outright**: the row reads `needs you` with the offending paths listed and the refusal
goes into `events`. Nothing is force-pushed and nothing partial is pushed.

A clear diff pushes, records `pushes` (`pushed_tip`, `base_branch`, `base_sha_at_push`,
`pushed_at`), then opens the PR with
`gh pr create --base <base_branch> --fill --label keep-open` — `keep-open` because both repos
auto-close after 14 days of inactivity, which would otherwise close a parked row and cascade
its dependents. **If an open PR already exists for the branch it is adopted, never
duplicated** (inv. 20), which makes a crash between create and record a non-event. The body
gets the "Merge after #N" line only when the base is not `main` — unreachable in Phase 1 with
`stacking = false`, wired now so Phase 2 does not retrofit it.

A push or `gh pr create` failure derives `push failed`, whose verb is **`retry push`** — the
push step alone, no agent, because a push failure is auth or a non-fast-forward and spending
the wall on an agent to retry a network call is waste. Rows with an open PR derive `checking`.

### Acceptance criteria

- [ ] `plan.Refused` is table-tested: a diff touching `.github/workflows/ci.yml`,
      `.mergify.yml`, a nested `packages/*/package.json`, `pnpm-lock.yaml`, `.npmrc`,
      `.env.development.local`, and `scripts/gen.sh` each refuse and name the path; an
      application-only diff clears. Fixture paths are `support-app`'s policy, evaluated as
      pure strings — no repo is read.
- [ ] `e2e/tests/push_refused.txtar`: a fake agent that commits `.github/workflows/x.yml` leaves
      the branch unpushed against a local bare remote, the row `needs you` with the path listed,
      and an `events` row recording the refusal.
- [ ] `e2e/tests/push_and_open_pr.txtar`: a clean run pushes once, inserts one `pushes` row, and
      the fake `gh` log shows exactly one `pr create` carrying `--fill`, `--label keep-open` and
      the right `--base`.
- [ ] `e2e/tests/pr_adopted_not_duplicated.txtar`: with a fixture already reporting an open PR
      for the branch, a tick opens nothing new — one `pr create` in the log across the run, or
      none — and `pushes` is not re-inserted for an unchanged tip (inv. 20).
- [ ] `e2e/tests/fail_push_retry.txtar`: with the fake `gh` failing `pr create`, the row reads
      `push failed`; `cc request POST /verb verb=retry-push` completes it and the agent log
      shows no second spawn.
- [ ] `e2e/tests/unlock_on_pr_open.txtar`: swapping the fixture to "root PR open" flips the
      dependent's `queued` reason from base to slot on the next tick.
- [ ] *(sandbox)* A real push opens a real PR carrying the `keep-open` label, and a second tick
      adopts it rather than duplicating.

---

## Phase 5: The verdict

**User stories**: PRD § The verdict; § A rollup is not a verdict; § The six must-fixes
(must-fix 1 — the only live one); § The states (`review me`, `needs you`, `checking`).

### What to build

`internal/verdict` as a pure package over the normalised snapshot — never over gh's JSON. The
predicate grammar is `all_of` / `any_of` / `not` / `success` / `skipped` / `absent_ok`, parsed
from `[repo.checks]`. Inputs, honestly enumerated: the reduced check map, `headRefOid` vs the
tip the app pushed, whether the base is the default branch, the recorded base SHA vs
`origin/<base_branch>`'s tip **consulted only for a stacked base** (unscoped, `main`'s 130–187
moves a fortnight would make `base moved` every root row's steady state), `pushed_at` vs now
for the bounded wait, the `.mergify.yml` hash match, and the PR's author login for the
dependabot arm of `services`' Linear branch.

Semantics that have to hold: an empty rollup, an absent rollup, or one belonging to another
commit is **never green**. An `absent_ok` check that is absent reads `pending` until the
bounded wait elapses, then `passing` — never green from a young snapshot. `checking` exits to
`needs you` if no matching rollup appears within the bounded wait, clocked from `pushed_at`
and **counted only over ticks whose observe phase succeeded**, so a GitHub outage cannot walk
every in-flight row to `needs you` at once. A green verdict is suppressed to
`checking (check config changed)` while the repo's recorded `.mergify.yml` hash no longer
matches the file — staleness as a visible row instead of a silent lie.

Both real repos' predicates ship as fixtures — transcribed from their `.mergify.yml` files by
reading them — even though Phase 1 evaluates the sandbox's copy of `support-app`'s:
`services`' correct names (`Unit Test`, `Generated files up-to-date`, the two itest checks),
its three-way deployment `any_of`, its four-way Linear `or` with the dependabot arm, and the
path-filtered `Lint GitHub Actions / Lint` that passes when absent.

Mergify's own merge-protection check-run is **not** read: it folds in
`#approved-reviews-by>=1`, `-draft` and `-label=WIP`, each false at exactly the moment the app
wants to say `review me`.

### Acceptance criteria

- [ ] `verdict.Evaluate` is table-tested over both repos' predicates: all green → `review me`;
      one gating check red → `needs you`; a green rollup at a **different** head SHA → not
      green; an empty rollup → not green; an absent `absent_ok` check → `checking` young and
      `review me` after the wait; `services`' skipped-`Deploy` arm and its dependabot arm both
      resolving without a matching check-run.
- [ ] The bounded wait does not advance across ticks whose observe phase failed — asserted with
      an injected clock and a forced-failure sequence, never by wall clock.
- [ ] `internal/verdict`'s import guard holds.
- [ ] `e2e/tests/verdict_green_then_red.txtar`: an all-green rollup fixture derives `review me`;
      swapping one check to failure derives `needs you` on the next tick; a rollup at a foreign
      head SHA derives `checking`, not green.
- [ ] `e2e/tests/verdict_config_changed.txtar`: editing the repo's `.mergify.yml` in the script's
      fixture repo suppresses a green row to `checking (check config changed)`, and restoring
      the recorded hash brings `review me` back.
- [ ] *(sandbox)* A real sandbox PR reaches `review me` with every mirrored gating check green,
      and flipping one red via the control file moves it to `needs you` on the next tick.

---

## Phase 6: The two-ticket slice end to end, terminal states, teardown

**User stories**: PRD § Authorising a feature with a fan-out in it (the two-ticket form); §
Goal ("zero `cd`s and zero `ps`"); § The states (`merged`, `pr closed unmerged`, `base gone`,
`cancelled`); PRD § Phase 1 requirement "job 3 firing exactly once".

### What to build

The acceptance run, plus what it needs to finish. Authorise both tickets once; the root cuts
and runs; its PR opens; **job 3 fires exactly once more** and the dependent cuts off
`origin/main` (stacking off) and runs; both PRs reach a verdict. Nothing is `cd`-ed into and
`ps` is never run.

Terminal handling: the per-tracked-branch fallback read already sees MERGED and CLOSED, so
rows derive `merged` and `pr closed unmerged`. A row that **has run** whose blocker's PR was
closed unmerged derives `base gone`, never `blocked` — its worktree and commits are real
(inv. 19); a member that never launched re-derives `blocked`/`queued` instead. `remove
worktree` calls `tp remove --force`, and only with MERGED PR state or on a `base gone` row the
user clears; it never removes a worktree that is dirty or holds unpushed commits (inv. 3), and
it prunes that task's run logs.

Remaining verbs: `re-run` (relaunch in the same worktree, incremental — a second `runs` row
against the same task, which is what the run/push split exists for), `close PR`
(`gh pr close` — the app can open a PR, so it needs a sanctioned way to unopen one), and
`remove worktree`. `cancel` is Phase 2 and is not built.

### Acceptance criteria

- [ ] `e2e/tests/slice_of_two.txtar` — the plan's headline scenario, hermetic: authorise both
      tasks, tick, the root runs and pushes, swap the fixture to "root PR open", tick, the
      dependent runs and pushes. `events` shows exactly two launches and the fake agent log
      exactly two spawns.
- [ ] `e2e/tests/teardown_and_terminal_states.txtar`: a merged fixture derives `merged` and
      `remove worktree` calls `faketp remove --force`; a closed-unmerged fixture on a row that
      has run derives `base gone`, not `blocked`; `remove worktree` refuses a dirty worktree.
- [ ] *(sandbox)* One authorisation of the two-ticket slice produces two open PRs with no
      further human action, and job 3 fires exactly twice — asserted from `events`.
- [ ] The `events` table reconstructs the whole run: authorisation, each launch, each
      disposition, each push, each verdict transition, every refusal.
- [ ] `re-run` on a `failed` row creates a second `runs` row in the same worktree and pushes its
      commits through the same push step; `close PR` derives `pr closed unmerged`. Both driven
      by `cc request POST /verb` in the e2e scripts above.
- [ ] `remove worktree` refuses a worktree holding unpushed commits as well as a dirty one
      (inv. 3), and prunes that task's run logs when it does remove.
- [ ] *(sandbox)* Merging the root's PR derives `merged`, and `remove worktree` then succeeds on
      the squash-merged branch via the real `tp remove --force`.
- [ ] *(sandbox)* Killing the app mid-slice and restarting resumes without relaunching anything
      and without duplicating a PR.

---

## Cutover checklist — not part of Phase 1

Phase 1 is done when the six phases above pass against the sandbox and fake agents. Pointing
the app at a team repo and a real agent is a separate, deliberate act; these are its
preconditions, and the first two are the PRD's original Phase 1 entry spikes.

- [ ] **`--fill` × the required Linear check.** One `--fill`'d PR in `support-app` from an
      agent-shaped branch name. If it fails, every real PR parks in `needs you` and the
      branch-naming convention has to be settled first.
- [ ] **Retarget × Mergify's queue.** One hand-made two-PR stack in `support-app`: merge the
      parent, record retarget timing and the child's resulting `baseRefName`, then queue the
      retargeted child. Pre-Phase-2 by §13.9; do it before `stacking = true`, not before
      Phase 1's code.
- [ ] **Boundary 1, behaviourally.** One real `claude -p` run with the app-owned `--settings`
      file, on a throwaway ticket, that attempts `git push` and a `gh` command — both refused
      in the run log. This is the assertion the fake agents cannot make.
- [ ] `agent_command` switched to the real `claude -p` template; `[[repo]]` `name`/`path`
      pointed at `support-app`; `mergify_sha` recomputed against the real file.
- [ ] The push policy re-diffed against `support-app`'s actual tree — the sandbox's mirrored
      deny list is a copy, and a copy can drift.
