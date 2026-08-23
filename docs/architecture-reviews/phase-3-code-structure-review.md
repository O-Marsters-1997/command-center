# Command Centre — code structure review (end of Phase 3)

_Date: 2026-08-23. Target: the built code at `64fb37f`,
all ~1,900 lines of non-test Go across `cmd/cc`, `internal/cc`, `internal/plan`, `internal/gh`,
`internal/tp`, read against `docs/prds/prd-command-centre.md`, `docs/designs/command-centre-design.md`,
`docs/prds/prd-operator-surface.md` and `plans/command-centre-phase-1.md`. Issues #1, #2, #4, #11 and
#12 are merged; #3, #5, #6, #7 and #8 are open. Three questions: what is the design, do the
module boundaries hold, and is the file layout carrying its weight? Piles 3 and 4 below were
fixed in the same pass that wrote this; no Go behaviour changed._

---

## Contents

- [The design](#the-design)
- [Module boundaries: intended vs actual](#module-boundaries-intended-vs-actual)
- [File structure: where too much got dumped together](#file-structure-where-too-much-got-dumped-together)
- [The audit log has no writer, and that is structural](#the-audit-log-has-no-writer-and-that-is-structural)
- [Candidates](#candidates)
- [Recommendation](#recommendation)

---

## The design

### Problem

You can split a feature into six tickets in twenty minutes. Getting six reviewable PRs out of
them takes days, and almost none of that is agents working.

The expensive part is that ticket 2 waits on ticket 1's *merge*, and merge needs a colleague's
approval you do not control (`.mergify.yml` requires `#approved-reviews-by>=1`). Six dependent
tickets means six round trips through someone else's inbox, and your fleet idles between each
one. Four smaller costs sit on top:

- A live agent and a dead one look identical from outside. Same quiet log, same clean worktree.
- "Did it actually commit anything" is a manual `cd` into the worktree.
- `gh pr checks` conflates required with advisory, reads an empty rollup as nothing-wrong, and
  shows green check-runs belonging to a commit that is no longer the tip.
- Nothing answers "what could start right now" across a dozen tickets in flight.

### Solution

An open PR is enough to build on. That one decision is the whole product. The moment a blocker
has an open PR its branch exists, so everything behind it has a base, and throughput decouples
from the reviewer's queue.

Around that sits a local control plane: a 15-second tick that re-derives everything from the
world, launches agents as dependencies unlock, pushes what they produce, opens stacked PRs,
reads CI properly, and never merges.

### How it gets there

Five mechanisms, and they are well chosen.

**Level-triggered, never latched.** The tick runs the same three jobs in the same order without
branching on why it woke. Status is a pure function of stored facts, never a column. Crash
recovery is not a code path, it is tick 1 doing what tick 4000 does.

**Truth is artifacts, not self-report.** Liveness is `ps -o stat=,etime=` plus a start-time
match. Success is commits after *this run's* baseline SHA. An agent that dies mid-tool-call
writes no status envelope, so that case is the default here rather than the unobservable one.

**Bounded autonomy.** A launch is a set of tasks you authorised, and the tick can only ever
touch those. Consent binds to content through a prompt hash, so a yes given at 9am refuses at
noon if the seam files moved.

**Two trust boundaries around push.** Agents get an app-owned settings file denying `git push`
and all of `gh`. The app then diffs against base under a path denylist before pushing itself.

**The verdict is a boolean expression, not a list of names.** Evaluated over a normalised rollup
(latest run per check name) whose head SHA equals the pushed tip, with expiry scoped to stacked
bases only.

The docs are unusually good. Three review rounds visibly deleted machinery rather than adding
it, and the code carries the reasoning forward in comments that name the invariant rather than
the line.

---

## Module boundaries: intended vs actual

`plans/command-centre-phase-1.md` § Module boundaries is the contract. Measured against the tree:

| Package | Planned | Built |
|---|---|---|
| `internal/plan` | pure, stdlib-only, import-guarded | yes, and the guard is real. `api_test.go` requires every import be stdlib, closing the transitive hole a plain `os/exec`/`database/sql`/`net/http` denylist would leave |
| `internal/verdict` | pure predicate evaluation | not built. Phase 5, issue #6 |
| `internal/gh` | the only place that knows gh's JSON | yes for `List`. `CreatePR` and `ClosePR` pending, issue #5 |
| `internal/tp` | `New` plus `Remove` | `New` only, 25 lines |
| `internal/cc` | "the imperative shell: store, git, loop, runner, http" | one flat package, 14 non-test files |

The vertical boundaries hold. `internal/plan` is pure and the guard proves it. `internal/gh` is
a real anti-corruption layer and nothing else decodes gh output. The horizontal boundary inside
`internal/cc` does not exist. The plan document lists store, git, loop, runner and http as
though they were modules; in the tree they are filenames in one package with no wall between
them.

That matters more than it sounds, because the design's most load-bearing rule lives exactly
there. Invariant 9, "only the loop writes the database", is what makes verbs race-free against a
launch and is cited in five separate comments. Its entire enforcement is those comments.
`Server` holds a `*Store`, so `s.store.RecordSpawn(...)` or `s.store.ApplyLaunchIntents(...)`
compiles today. Nothing but review discipline stops a future handler from doing the obvious
impatient thing.

---

## File structure: where too much got dumped together

Four distinct piles. The first two are architecture and feed the candidates below. The last two
were layout only, and are already fixed.

### 1. `internal/cc` is five concerns with no seam

Persistence is 570 lines across `store.go`, `runs.go` and `launch.go`, all methods on one
`*Store`, split by which phase wrote them rather than by concept. `ActiveLaunchHashes` sits in
`runs.go` and `ActiveMemberships` in `launch.go`; they read the same two tables and differ only
in return shape. Process control (`runner.go`, `runner_unix.go`), git exec (`git.go`), HTTP
(`server.go` plus `page.tmpl`), the loop, and six small support files sit alongside with no
grouping.

### 2. The `plan` to `cc` translation lives in the HTTP file

`planTask`, `prState`, `planTasksByURL` and `prsByBranch` are all in `server.go`, and `loop.go`
calls three of them. Worse, `derive()` in `server.go` and `launchEligible()` in `loop.go`
independently rebuild the same three steps: `byURL`, then `prs`, then `plan.Unlocked` per task.
Phase 4 and 5 add push facts and a verdict to `plan.Facts`, so both call sites will have to grow
identically or drift.

### 3. Two test files out of nine are misfiled (fixed)

`internal/cc` has nine test files for three main source files, which looks worse than it is.
`git_baseline_test.go`, `cancel_test.go`, `loop_launch_test.go` and `server_verb_test.go` are
all named by subject and earn their place. Two were not:

- `loop_runner_test.go` held zero test functions. It is the shared fake runner, the fake `tp`
  script and two repo fixtures, so it is now `fakes_test.go`.
- `server_running_test.go` held one test about a rendered running row, which belongs with the
  other page-render tests. Merged into `server_test.go`.

Done in this pass. Noted because the pattern (a new file per PR rather than per subject) will
recur across issues #5 to #7 unless someone is watching for it.

### 4. Docs were flat and the version numbers lied (fixed)

Before this pass: `command-centre-v1.md` contained revision 4, `command-centre-v2-buildability-review.md`
reviewed revision 2, and `command-centre-v3-phase1-foundations-review.md` reviewed revision 3
from a subfolder the other two did not share. Reading them in order meant holding a
filename-to-revision mapping in your head.

Now:

```
docs/
  command-centre-design.md            the living design, currently revision 4
  prd-command-centre.md
  prd-operator-surface.md
  command-centre.sample.toml
  architecture-reviews/
    revision-2-assessment.md
    revision-2-buildability-review.md
    revision-3-foundations-review.md
    phase-3-code-structure-review.md  this file
```

The living design has no version in its filename, because it keeps being revised. Reviews keep
theirs, because each reviews a fixed revision. Every cross-reference in the repo was updated,
including four Go source comments.

Left alone deliberately: `plans/` stays a top-level directory, because all seven GitHub issues
link into it by that path. `.claude/handoffs/` stays as an archive of past sessions.

---

## The audit log has no writer, and that is structural

`AppendEvent` has exactly one caller: `RecordTickError`. So `events` is written on tick failures
and nothing else.

The design wants an insert on every transition, authorisation, verb and refusal. Since status is
deliberately never stored, `events` is the only possible record that a transition happened. It
cannot be written today because the loop never calls `plan.Status` at all. Status is computed
only in the HTTP render path, so no code is ever in a position to notice a transition.

This is not a missing function. It is a consequence of where the boundary got drawn, and it is
why candidate 1 below matters more than its size suggests.

---

## Candidates

Each is a bounded zone of friction, not a proposed solution.

### 1. The status derivation has two independent call sites and no owner

- **Cluster:** `internal/cc/server.go` (`derive`, `runFactFor`, the four mapping helpers),
  `internal/cc/loop.go` (`launchEligible`), `internal/plan` (`Unlocked`, `Status`, `Facts`,
  `LaunchCandidate`)
- **Why coupled:** both sites assemble the same inputs from the same three store reads and call
  into `plan` with different subsets. `plan.Facts` and `plan.LaunchCandidate` are two views of
  one derivation. The `cc` to `plan` type mapping is duplicated knowledge that happens to live
  in the HTTP file.
- **Dependency category:** in-process
- **Test impact:** `server_test.go`'s golden-file assertions and `loop_launch_test.go`'s cap
  tests both exercise this derivation through a full app. A single derivation boundary would
  make it a table-driven test over facts in, rows out, leaving the golden file to cover HTML
  only.
- **Bonus:** this is where `events` gets a home. Whoever owns the derivation is the only thing
  that can compare last tick's state to this one.

### 2. `Store` is one type doing six jobs, with write access handed to the page

- **Cluster:** `store.go`, `runs.go`, `launch.go` (~570 lines, one `*Store`), consumed by `Loop`
  and `Server`
- **Why coupled:** co-ownership of one `*sql.DB` and one method set. Invariant 9 asks for
  asymmetric access (loop writes, page reads) and the type offers none, so the invariant is
  enforced by comments. The `intents` table is served by two parallel queue-and-apply mechanisms
  (launch intents with a group token and JSON payload, verb intents) that could be one.
- **Dependency category:** local-substitutable. SQLite in a temp dir is already the test setup.
- **Test impact:** `store_test.go`, `runs_test.go` and `launch_test.go` test SQL round-trips
  method by method. A reader/writer split would test at the two interfaces instead, and "the
  page cannot write" becomes a compile error rather than a review comment.

### 3. Repo configuration is re-derived on every tick and every render

- **Cluster:** `config.go` (`stackingByRepo`, `repoPathsByName`), plus callers in `loop.go`,
  `server.go`, `observe.go`, `repocheck.go`
- **Why coupled:** three or four per-repo lookups keyed off `Repo.Name`, each rebuilt from the
  slice at the point of use. Phase 4 adds the push denylist, Phase 5 the mergify hash and compat
  check, so this grows to five.
- **Dependency category:** in-process
- **Test impact:** small. Mostly this removes lookup plumbing from the loop and the server so
  their bodies read as the sequence they are documented to be.

### 4. The observe phase and the world-reading primitives are one undifferentiated pile

- **Cluster:** `observe.go`, `git.go`, `gh.List`, and `Worktrees` called twice (once in observe,
  once mid-spawn in `cutAndSpawn`)
- **Why coupled:** `Observation` is the tick's snapshot type, but `cutAndSpawn` reaches around
  it to call `Worktrees` and `BranchTip` directly mid-act. Phase 4's push and PR-create add
  three more direct git and gh calls from inside the loop. The "all I/O that reads the world
  happens in observe" rule is already half-broken and will break further.
- **Dependency category:** local-substitutable for git (temp repos, already done in
  `git_baseline_test.go`); ports and adapters for gh, where `e2e/fakegh` already exists as an
  adapter.
- **Test impact:** `WithObserver` substitutes the read half today, which is why loop tests work.
  The write half has no seam at all, so Phase 4's push logic will only be testable end to end
  unless one gets defined first.

### 5. Test and doc file layout encodes PR history rather than subject

- **Cluster:** the eight `*_test.go` files remaining in `internal/cc`; `docs/`, `plans/`,
  `.claude/handoffs/`, the README
- **Why coupled:** not code coupling. This is navigation cost. Cheap, mechanical, no behaviour
  change.
- **Dependency category:** in-process
- **Test impact:** none. Renaming and moving only.

---

## Recommendation

Candidates 1 and 2 are the real architecture, and they are closely related: the derivation owner
is also what makes the read/write split natural.

Candidate 4 is the one that gets more expensive the longer it waits. Issue #5 is next in the
queue and it adds the writes, so the seam is cheaper to define before that code exists than
after.

Candidates 3 and 5 are tidying. Worth doing, not worth a design round.

Suggested order: **1, 4, 2**, with 3 and 5 folded in opportunistically. 1 and 2 overlap on
`Store`'s method set, so doing 1 first settles what the reader interface needs before 2 draws
the line.
