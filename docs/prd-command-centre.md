# Command Centre

**Status:** WIP · **Date:** 2026-08-22 · **Shape:** concept doc

**Reads with:** [`command-centre-design.md`](command-centre-design.md) (the design, revision 4 —
**normative** for mechanism, state list and invariants),
[`architecture-reviews/revision-3-foundations-review.md`](architecture-reviews/revision-3-foundations-review.md)
(the review of revision 3 that drove revision 4),
[`architecture-reviews/revision-2-buildability-review.md`](architecture-reviews/revision-2-buildability-review.md)
(the review of revision 2), [`architecture-reviews/revision-2-assessment.md`](architecture-reviews/revision-2-assessment.md).

## TL;DR

Command Centre turns a planned feature into a **backlog of reviewable pull requests** without you
being blocked between them. You authorise a slice of tickets once; the app launches each agent as
its dependencies unlock, pushes what they produce, opens stacked PRs and reads CI precisely. Six
tickets become six PRs waiting for review in an hour, not six round-trips through someone else's
inbox over three days.

It never merges, and it never chooses the work — autonomy exists strictly inside a slice you
authorised.

## Contents

- [Problem](#problem)
- [Why does this matter?](#why-does-this-matter)
- [Goal](#goal)
- [Concepts](#concepts)
- [Scenarios](#scenarios)
- [What we already have (reuse)](#what-we-already-have-reuse)
- [Important technical details](#important-technical-details)
- [The six must-fixes, re-sequenced](#the-six-must-fixes-re-sequenced)
- [Phases](#phases)
- [Dependencies](#dependencies)
- [Out of scope](#out-of-scope)
- [Open questions](#open-questions)

## Problem

You can break a feature into six tickets in twenty minutes. Turning those six tickets into six
reviewable pull requests takes days, and hardly any of that is agents working.

**The expensive one: your work is drip-fed through someone else's review queue.** Ticket 2 depends
on ticket 1, so you sit on ticket 1 until a colleague reviews and merges it — `.mergify.yml`
requires `#approved-reviews-by>=1`, so that latency is not yours to control. Six dependent tickets
means six round-trips through an inbox you don't own, and between each one your fleet is idle. The
review queue, not the agents, sets your throughput.

Everything else is friction on top of it:

- **Liveness is a guess.** An agent thinking inside a 40-minute tool call and an agent that died
  ten minutes ago look identical from outside — same quiet log, same clean worktree. You find out
  by `ps`, or by waiting and feeling foolish.
- **"Did it actually do anything" is a manual read.** A dead run with no commits and a dead run
  that committed and exited are the same event until you `cd` in and look.
- **A rollup is not a verdict.** `gh pr checks` conflates required with advisory, reads an empty
  rollup as nothing-wrong, and shows you green check-runs belonging to a commit that is no longer
  the tip. "Is this actually reviewable" means reading `.mergify.yml` in your head, every time.
- **The last twelve steps are the same every time.** Check the diff, check nothing touched CI
  config, push, open a PR against the right base, fill the body, watch checks. Twelve manual steps
  done slightly differently each time is what a control plane is for.
- **Nothing tells you what is unlocked.** With a dozen tickets in flight, "what could start right
  now" has no answer you can read.

**Nothing on the market does this.** T3 Code is the closest shipping thing — 20k stars, five agent
CLIs behind one surface, event-sourced, phone clients. It has no dependency model at all: no
`blockedBy`, nothing that knows one piece of work unblocks another, and it renders check rollups
for a human to look at rather than consuming them. Every turn is started by a person typing into a
composer. It owns *where you watch your agents from*; it has no answer to *what can start now*.

## Why does this matter?

**An open pull request is enough to build on, and that is the whole product.** The moment a
ticket's blocker has an open PR, that branch exists and everything stacked behind it has a base.
Waiting for *merge* caps your throughput at your reviewer's response time. Building on the open PR
decouples them — you produce a batch of PRs at agent speed and your reviewer works through them at
theirs. Without this the app is a nicer way to run one agent at a time, which is not worth
building.

**The expensive thing is the attention between agents, not the agents.** The agent's work is
already delegated. The babysitting and the twelve steps at the end of each run are not, and that
is what stops you running four.

**A pid is the only honest liveness signal, and no tool is built around one.** Everything in this
space infers state from log timing or its own session bookkeeping. A direct check of the pid plus
a process-start-time check cannot be wrong, and artifact-first classification — commits since
*this run's* baseline SHA — cannot be fooled by a previous attempt's work. (The check is
`ps -o stat=,etime=`, not `kill(-pgid, 0)`, for cross-platform reasons — see §"A run".)

**Bounded autonomy is what makes the failure modes cheap.** Every expensive failure in the
buildability review comes from the app acting on state it computed. The fix is not to remove the
autonomy — that is the point of a slice — but to bound its *scope* to work you authorised and show
you the plan first. A wrong base inside a slice you previewed is a line you read.

**Honest about Phase 1: it is the plumbing, not the payoff.** A two-ticket slice proves
launch-on-unlock actually fires, over primitives that are unproven until it runs. The backlog
argument above needs Phase 2's fan-out to pay off.

## Goal

**You authorise a feature and walk away; it comes back as a stack of PRs.** Two countable things:
the number of times you `cd` into a worktree or run `ps` to find out what an agent is doing
(target zero), and the number of PRs open for review after one authorisation (target: all of
them, in roughly the time the longest single ticket takes).

## Concepts

### A task

One ticket. The row you read and the unit you authorise.

| Field | |
|---|---|
| `ticket_url` | the ticket this implements |
| `repo` | which configured repo it lands in |
| `branch` | the branch and worktree name |
| `status` | one of the states below — derived each tick, never stored |
| `blocked_by[]` | tasks that must have a PR before this one has a base |
| `base_branch` | derived each tick, not stored by intake — see Unlock |
| `seams[]` | *Phase 3* — seams this ticket consumes |

### Unlock

A task is **unlocked** when there is something for its worktree to be cut from. Only
same-repo edges count; a cross-repo edge feeds the Phase-3 draft gate, never unlock.

- No blockers → unlocked immediately, base `main`.
- One blocker with an **open** PR (drafts included — the branch exists) → unlocked. Base:
  the blocker's branch when the repo's `stacking` flag is on; `main` when it's off (Phase 1).
- One blocker **merged** → unlocked, base `main` — both repos delete branches on merge, so
  the blocker's branch no longer exists and its change is in `main`.
- Several blockers → nothing can stack on two branches at once, so the base is `main` and it
  unlocks when **every** blocker has merged.

**Open is enough**, and that is the load-bearing decision in this design. **Merged counts too**: a
blocker that merged before you got round to its dependent must not strand it.

Unlock is recomputed from the world every tick, never stored. A blocker whose PR gets closed
sends its never-launched dependents back to `blocked`; a dependent that already ran reads
`base gone` instead — its worktree and commits are real and must not be relabelled as
"nothing to cut from".

### A launch

A launch is **a set of tasks you have authorised the app to drive**. It is the unit of your
consent and the only thing that lets the tick spawn anything.

You select one task, a slice, or a feature's worth. The app shows the plan first:

```
LAUNCH — 5 tickets, 1 repo

  now        PLA-12   cut off origin/main         (fresh)

  on unlock  PLA-13   off PLA-12   ← when PLA-12's PR opens
  on unlock  PLA-14   off PLA-12
  on unlock  PLA-15   off PLA-12
  on unlock  PLA-16   off PLA-12        4 parallel, capped at max_agents (4)

  refusing   PLA-17   blocker PLA-09 is not in this slice and has no PR

            [ authorise ]  [ cancel ]
```

On authorise the tick takes over. PLA-12 launches immediately; the rest sit `queued` until
PLA-12's PR opens, then all four launch at once.

**Authorising binds consent to content.** The preview composes each task's prompt to render
it, and the hash of that composition is stored with the member. A launch can sit `queued` for
hours; at spawn time the tick recomposes and refuses on mismatch (`seam changed`) — your yes
attaches to what you previewed, not to whatever the seam files say later.

A launch stays **active** until every member is terminal or you cancel it. **`cancel launch` stops
the tick starting anything further from that slice and leaves running agents alone** — it
withdraws consent for future work, it does not kill work in progress, and the tick still
pushes and reads CI for agents already running.

| Field | |
|---|---|
| `id`, `created_at` | |
| `members[]` | the tick's entire permitted scope — one task + the prompt hash it was authorised with |
| `state` | `active` / `done` / `cancelled` |

### The tick

Every ~15 seconds, same order, without branching on why it woke. Three jobs:

**1. Reconcile what is in flight.** `git fetch --prune`; a PR snapshot (one bulk read per
repo plus a per-tracked-branch fallback that can see merged and closed PRs — `gh pr list`'s
defaults cannot); liveness per run; classify dead runs from artifacts; push what is pushable
and open its PR (`--label keep-open`, adopting an existing PR rather than duplicating it);
read verdicts; notice merges and closures; retarget children whose parent merged.

**2. Recompute what is unlocked.** Pure, over the snapshot, per task: unlocked?, off what
base, and *why it is waiting* — the page renders the reason.

**3. Launch authorised work that has just unlocked.** For every task unlocked, in an **active
launch**, whose prompt still hashes to what was authorised, never launched, and under
`max_agents` (global, across repos): cut the worktree, spawn the agent.

**If the fetch or any GitHub read fails, the tick stops before changing anything** — no
transitions, no launches, and the page shows tick age and the last error. A failed read must
never masquerade as "no PRs".

**Job 3 only ever touches tasks inside an active launch.** The tick does not read Linear, does not
scan for new tickets, does not add rows to its own table, and will not launch an unlocked task
nobody authorised — that sits in `ready` for you. The tick executes a plan; it never writes one.

It also never *re*-launches. A failed task has no PR, so its dependents stay blocked and the slice
stalls visibly. Retrying is your verb.

`max_agents` is the only knob, and the wall it guards is the Claude subscription's rate
limit plus the team's shared CI capacity (on `services`, one parent fix can trigger every
descendant's full check set and a stage-pool claim each) — not GitHub's API, which the tick's
reads barely dent.

### A run

One agent process against one task.

| Field | |
|---|---|
| `pgid`, `started_at` | the liveness identity — pgid alone can be recycled |
| `baseline_sha` | the branch tip *when this run launched* |
| `prompt_hash` | `hash(composed prompt)`, so a changed input is detectable |
| `log_path` | a file in the app's state dir (outside the workspace — worktrees are siblings of `plain/.claude/`), redirected, never piped |
| `exit_code`, `ended_at` | recorded when the process is found dead |

A run is alive when `ps -o stat=,etime= -p <pgid>` reports a non-zombie process whose start time
matches — not `kill(-pgid, 0)` as originally specified: that call returns `EPERM`, not `ESRCH`,
against a process group whose leader has become a zombie on Darwin, which is exactly the moment
liveness must report false, and BSD `ps` has no `etimes` keyword at all, only the `etime` format
GNU and BSD `ps` both support (`internal/cc/runner_unix.go`). No timing rule ever marks a run
dead. A dead run's disposition comes from artifacts: commits after **its own** `baseline_sha`.

### The verdict

Recomputed each tick, from a normalised rollup (latest run per check name — one name can
appear five times on one commit with conflicting conclusions) whose head SHA equals the tip
the app pushed. For a **stacked** base only, the verdict also expires when the recorded base
SHA no longer matches the parent branch's tip — a root's base is `main`, which moves many
times a day and whose drift is Mergify's queue's job, not a reason to expire. An empty
rollup, an absent rollup, or one belonging to another commit is never green.

- every gating check green → `review me`
- a gating check red → `needs you`
- otherwise → `checking`

### The states

Two independent facts decide a task's state: is it **unlocked** (has a base?) and is it
**authorised** (in an active launch?). Neither alone starts anything. Status is **derived**
from stored facts by one pure function, never stored as a column — which is also what lets a
`queued` row say *why* it waits. **The design doc's §5 is the normative state list**; this
table is the product summary.

```
                    unauthorised          authorised
                 ┌────────────────┐   ┌──────────────────┐
  blocked        │    blocked     │   │      queued      │
                 └────────────────┘   └──────────────────┘
                          │                     │
  unlocked         ┌──────▼──────┐              │  tick launches
                   │    ready    │──authorise──►│  automatically
                   └─────────────┘              │
                     you launch                 ▼
                                            running ──► checking ──► review me ──► merged
                                               │            │
                                               │            ├──► base moved ─refresh─► checking
                                               ├──► failed  └──► needs you
                                               ├──► cut failed
                                               └──► push failed
```

| State | Means | Verbs |
|---|---|---|
| `blocked` | a blocker has no open or merged PR — no base to cut from, unauthorised | launch (authorises; starts on unlock) |
| `queued` | in an active launch, waiting — the row says whether for its base or for a slot | cancel |
| `ready` | unlocked, unauthorised. The row you act on | **launch** |
| `running` | agent process alive | kill |
| `checking` | pushed, PR open, no matching rollup yet or checks pending | re-run, close PR |
| `review me` | PR open, every gating check green (stacked base unmoved) | close PR |
| `needs you` | a gating check red, a refused push, or a diff that hit the push policy | re-run, kill, close PR |
| `failed` | dead run, no commits after its baseline | re-run |
| `cut failed` | `tp new` failed | re-run |
| `push failed` | `git push` or `gh pr create` failed | **retry push** (no agent), re-run |
| `base moved` | a stacked parent advanced past what this branch was cut from, or merged (base is now `main`) | **refresh**, re-run |
| `refresh conflicted` | the refresh merge conflicted; the worktree waits for a human | abort |
| `waiting on producer deploy` | cross-repo: only the compat check is red | re-check |
| `base gone` | this row ran, and its parent's PR was closed unmerged | re-run, remove |
| `pr closed unmerged` | someone closed the PR | re-run, remove |
| `cancelled` | authorised then cancelled before launching | remove worktree, launch |
| `merged` | PR merged | remove worktree |

`seam changed` is a flag on a row, not a state — it composes with any of the above.

**A wedged agent is not a state** — pid liveness cannot compute it, so the page shows each running
row's elapsed time, pgid and worktree path as copy-pasteable text and `kill` is one click.

### The page

One HTML table at `127.0.0.1:7777`, one row per task. State, verbs as buttons, and per row the
worktree path, pgid, elapsed time, base and stack depth as plain text.

Four things it must make obvious:

- **`ready` rows** — unlocked, unauthorised, yours to act on.
- **Why a `queued` row is queued** — "waiting on PLA-12's PR", or "waiting for a slot". Four
  `queued` rows behind one `failed` blocker must read as one problem, not four.
- **Stack order** — you merge a stack bottom-up, and the app never merges, so it has to show
  you the order. And because the reviewer never sees this page, each stacked PR's body also
  carries a "Merge after #N" line — `gh pr create --base` makes an ordinary PR, not a GitHub
  Stack, so without it a reviewer sees a diff against a branch name they don't recognise.
- **Tick age and the last tick error** — the only signal separating "quiet because nothing
  needs doing" from "the loop died an hour ago".

## Scenarios

### Authorising a feature with a fan-out in it

**You want:** to say "do this feature" once and come back to a reviewable stack.

**How it works:** Five tickets — PLA-12, then PLA-13 to PLA-16 all blocked by it. You select all
five; the preview says PLA-12 starts now and the other four start on unlock. You authorise once.

PLA-12 is cut off `origin/main` and launched. The other four sit `queued`, each saying "waiting on
PLA-12's PR". The agent finishes; the tick pushes and opens PLA-12's PR. Next tick, the four are
unlocked and authorised, so the tick cuts each off **PLA-12's branch** and launches all four, up to
`max_agents`. Each opens its PR with `--base` set to PLA-12's branch, so a reviewer sees only that
ticket's diff.

An hour in you have five PRs open. Your reviewer works the stack bottom-up. You were never blocked
on them.

### The parent needs a fix

**You want:** the descendants to pick it up without a rebase ceremony.

**How it works:** Review comes back on PLA-12 and you push a fix. The tick notices PLA-12's branch
has advanced past what each descendant was cut from, and those rows read `base moved` — their code
does not include the fix, so a red check on them may not be their fault, and the page shows the
parent's verdict alongside.

`refresh` fast-forwards the descendant's branch to its own upstream, then runs
`git merge origin/PLA-12` in its worktree; the tick's push step delivers the result. No
history rewrite, no force-push, no `rebase --onto` — merge commits are free here because
everything squash-merges on the way in (server-enforced), and they don't pollute the
descendant PR's review diff. If the merge conflicts, the row parks as `refresh conflicted`
for you, never for an agent. And when PLA-12 *merges*, the app re-points each descendant's
PR at `main` — GitHub deletes the merged branch, and Mergify's queue only takes `main`-based
PRs.

### The agent dies having written nothing

**You want:** to not mistake this for progress.

**How it works:** The tick finds the pgid gone and no commits after the run's baseline. Nothing is
pushed and no PR is opened — `failed`, with the run log path on the row. Its dependents stay
`queued` behind it and the page says why. The tick does not retry; that is your call.

### The agent edits the CI config

**You want:** this to never reach GitHub.

**How it works:** Two boundaries. Every agent is spawned with an app-owned settings file
that denies `git push` and all of `gh` — the repos' own tracked `.claude` settings would
otherwise hand the agent a pre-approved push of its own. And before the *app* pushes, it
diffs the branch against its base under a per-repo policy: `.github/`, **`.mergify.yml`**
(the file the app's own verdict is transcribed from, at the repo root where `.github/`
doesn't reach), lockfiles, every `package.json`, `pnpm-workspace.yaml`, `.npmrc`, `.env*`,
and per-repo additions like `scripts/` and the build configs. Any hit and the push is
refused outright — `needs you`, with the offending paths listed. On `services` a push drives
dozens of jobs and an OIDC-federated AWS stage deploy, and `pnpm install` — the first step
of every required check, with a live secret in env — executes several of those files before
any application code compiles.

### The app crashes while agents are working

**You want:** the fleet to survive your app.

**How it works:** Agent output goes to a file, never a pipe, and every run's pgid and start time
are in SQLite. A crash leaves the agents running. On restart the app takes the flock, re-reads
runs, re-checks each with `ps -o stat=,etime=` plus the start-time match (§"A run"), and carries
on mid-run. It
does not relaunch — and because every outward effect records intent first and is reconciled
from the world (an existing PR is adopted, a worktree is found via git), a crash *mid-tick*
is equally boring.

## What we already have (reuse)

| Exists | The app's use of it |
|---|---|
| **`tp new <branch> --base <ref>`** | cuts every worktree, stacked or not. `--base` takes a ref (`docs/commands.md:74`), so both `origin/main` and a parent branch work. This is the whole of stacking on the git side. |
| **`tp remove`** | teardown after merge. Takes no flags today; needs `--force` (see Dependencies). |
| **treepad's `[sync]`** | copies `.treepad.toml` and friends into each worktree. The app does no file copying. |
| **`git worktree list --porcelain`** | the authoritative branch → path map, so the app never reimplements treepad's path derivation. |
| **`/implement`** | already runs the full suite at the end (`SKILL.md:11`). Local testing exists; the app does not check its result. |
| **`gh pr list --json`** | PR state *and* the unlock computation — a bulk open read per repo plus a per-tracked-branch fallback, because the defaults (`--state open --limit 30`) can't see merged PRs. |
| **`gh pr create --base <branch> --fill --label keep-open`** | the whole of stacking on the GitHub side; the label defuses both repos' 14-day auto-close. |
| **`gh pr edit --base main`** | re-points a child when its parent merges — GitHub deletes merged branches in both repos, and Mergify's queue only accepts `main`-based PRs. |
| **Mergify's merge queue** | merges `main` in (`update_method: merge`) and re-runs CI at merge time — for `main`-based PRs only, which is why the retarget above is the app's job. |
| **`.mergify.yml`** | ground truth for what gates a merge, in both repos. |
| **`cmd/cc/main.go`, Justfile, CI, golangci** | the scaffold: flags, `signal.NotifyContext`, build/test/lint. |

**Net-new:** `internal/plan` (unlock, launch eligibility, dead-run disposition, the push
plan — pure, import-guarded, so "would this launch, off what, and why?" is testable with no
process and no network), `internal/verdict` (pure predicate evaluation), `internal/gh` (the
one place that knows gh's JSON shape; normalises the rollup before anything reads it),
`internal/tp` (the two treepad commands), and `internal/cc` — store (SQLite: `tasks`,
`runs`, `pushes`, `launches`, `launch_members`, `events`, `intents`), loop
(observe → decide → act), runner (spawn with `Setpgid`, cancel via `-pgid` SIGTERM→SIGKILL,
because `exec.CommandContext` signals one pid and `claude -p` spawns tool subprocesses that
would orphan holding the worktree), http (the page, the launch preview, the verbs — which
queue intents; only the loop writes the DB).

## Important technical details

### Stacking is two flags, and stacks are never repaired

The app does not call `tp batch sync`, `gh stack link`, or `restack`. Stacking is:

- `tp new <branch> --base <blocker's branch>` — cut the worktree off the parent
- `gh pr create --base <blocker's branch>` — open the PR against the parent

The app derives the base from `blocked_by` itself (see Unlock), so intake supplies edges and
nothing else. **A fan-out is four tasks sharing one blocker**, each cut off it — there is no chain
between them to get wrong, which is what dissolves must-fix 5.

**When a parent moves, the app merges — it does not rebase.** `refresh` fast-forwards the
branch to its own upstream first (reviewers and Mergify both push to the app's branches),
then runs `git merge origin/<parent>` in the descendant's worktree; the push step delivers
the result, and a conflict parks the row as `refresh conflicted` for a human — never an
agent. Merging is legal on a pushed branch, needs no force-push, and cannot produce the
patch-replay conflicts a rebase hits after a squash merge. The reason it costs nothing:
squash is **server-enforced** in both repos (`allow_merge_commit: false`), so every merge
commit in a stack is flattened on the way in — and the parent's merge commits do not pollute
the descendant PR's review diff (verified). **All of the rebase machinery in revision 2
existed to preserve a history that GitHub discards.** This premise is now asserted at
startup, not just verified once by hand (`internal/cc/repocheck.go`).

**When a parent merges, the app retargets.** Both repos delete the merged branch, and
Mergify's queue only accepts PRs based on `main` (`base=main` heads its conditions in both
files) — so the app re-points each child with `gh pr edit --base main` and `refresh` then
targets `origin/main`. Merge-time integration belongs to the queue from there: it merges
`main` in and re-runs CI before merging. The genuinely untested remainder — auto-retarget
timing, and the queue on a just-retargeted child — is a pre-Phase-2 spike with two hand-made
PRs.

### The verdict predicate is an expression, not a list of names

The old `required_checks = ["Lint", "Typecheck", "Tests", "Generated files"]` cannot represent
what it claims to copy. Verified directly against both files:

- **`support-app`'s four names are correct.** `Lint`, `Typecheck`, `Tests`, `Generated files`,
  plus `GraphQL production compatibility`.
- **`services`' are wrong.** The real names are `Lint`, `Typecheck`, **`Unit Test`** (not
  `Tests`), **`Generated files up-to-date`** (not `Generated files`), `Integration Tests Passed`
  and `Local Integration Tests Passed`.
- **Three conditions are not names at all.** Deployment is
  `("Deploy / Deploy SST Stage" | "Deploy / Deploy PR Stage" | ("Evaluate" && skipped "Deploy"))`.
  The Linear check is a **four**-way `or` — three renamed jobs plus `author = dependabot[bot]`,
  a PR attribute, not a check. And `Lint GitHub Actions / Lint` passes when it is **absent** —
  path-filtered, so a PR touching no workflow file legitimately has no such check-run (Mergify
  writes that as a negation, which is why the grammar carries `not`).

So config carries the *check* subtree of `common_checks` as a small boolean form —
`all_of` / `any_of` / `not` / `success` / `skipped` / `absent_ok` — and `verdict/` is a pure
package over a **normalised snapshot** (latest run per check name; one name can appear five
times on one commit with conflicting conclusions) with no I/O, which makes every case above
a table-driven test. The app also records a hash of each repo's `.mergify.yml` beside its
predicate: when the file changes, green is suppressed until the copy is re-confirmed —
staleness becomes a visible row instead of a silent lie.

**The review's own suggested shortcut does not work.** It recommends reading Mergify's
merge-protection check-run instead of transcribing names. That check folds in
`#approved-reviews-by>=1`, `#changes-requested-reviews-by=0`, `-draft` and `-label=WIP` alongside
the checks — every one of them false at exactly the moment the app wants to say `review me`, which
means "all checks green, awaiting review". The app must evaluate the checks subset itself.

**A verdict on a stacked base expires when that base moves.** Each push records the base SHA
it was cut against. Advancing or retargeting a base re-runs no test workflow (only the
Linear check listens for `edited`), so the head SHA is unchanged and stale check-runs stay
green — a descendant would read `review me` having passed CI against a base that no longer
exists. One comparison, and the row reads `base moved` instead. The comparison exists **only
for stacked bases**: `main` moves many times a day, being behind it is normal, and
integrating it is the merge queue's job — an unscoped expiry would make `base moved` every
root row's steady state.

(`services` auto-reruns failed itest shards up to three times, so a red there may self-heal —
but the rollup exposes no attempt data and the retry runs attach to `main` commits, so the
app cannot see a pending retry and does not pretend to. Red is red; `re-run` is the verb;
automating the distinction is a priced deferral.)

### The Activity file: the app doesn't touch it

Revisions 2 and 3 had the app write treepad's Activity file as a double-launch guard.
Verified against treepad's source: every reader of that file — the launch keys, the
run-state column, the log pager — is gated on **Batch/manifest membership**, and this design
writes no manifests. So the file the app wrote was read by nobody, while forcing the app to
reimplement two of treepad's `internal/` derivations across a repo boundary. **Revision 4
deletes it** — the double-launch guard is the app's own DB (one launch per task, ever) plus
the flock. If the app ever starts writing manifests, that is the trigger to revisit.

The upstream `link`/`restack` veto revision 2 required is likewise not needed — not because
the app cannot trigger them, but because with no manifests there are no members for treepad's
`Reconcile` to act on.

### Storage

SQLite in the app's own state dir (mode 0700, **not** under `plain/` — the worktrees are
siblings of `plain/.claude/`, which would have put the consent table one `../` away from
every agent), one instance per workspace enforced by flock on a sibling lock file (locking
the DB file itself deadlocks modernc.org/sqlite under SQLITE_BUSY). WAL, and only the
loop writes — page verbs queue intents the next tick applies, which is what makes `cancel`
race-free against a launch. Status is derived from the stored facts by one pure function,
not stored; `events` is append-only audit, one insert on every transition, authorisation,
verb and refusal.

The world is re-read every tick and truth is DB ∪ worktree ∪ the PR snapshot. What is *not*
re-derivable is history: `launches` and its members (decisions you made, with the prompt
hash each authorisation was bound to), `runs` (baselines, pids), and `pushes` (what tip went
up, against which base SHA, when).

## The six must-fixes, re-sequenced

The review's six were written against revision 2, where the app launched whatever it found ready
and repaired stacks with `tp batch sync`. Bounding the tick to an authorised slice and never
repairing stacks dissolved three of them; revision 4 dissolved a fourth (the Activity file —
revision 3 "fixed" a guard that, verified, no longer had a reader).

| # | Must-fix | Now |
|---|---|---|
| 1 | CI verdict: predicate shape, wrong `services` names, no expiry | **Live, Phase 1.** The only gate. Predicate + normalised snapshot + a stacked-base-only expiry (rev 4 scoped it — unscoped, `main`'s daily drift made `base moved` the steady state). |
| 2 | Activity-file ownership | **Dissolved (rev 4).** Every treepad reader is manifest-gated and the app writes no manifests — the file had no reader, so the app stopped writing it. |
| 3 | `origin/main`, not `main` | **Dissolved.** Verified `--base` takes a ref; every cut passes `origin/main` or a parent branch explicitly, so the stale-root case cannot arise. |
| 4 | Batch scoping and teardown | **Dissolved.** No `tp batch sync`, so no manifests, so nothing to scope and no teardown loop. |
| 5 | One chain rule in `to-tickets` covering fan-in and fan-out | **Dissolved.** The app derives bases from `blocked_by` edges; a fan-out is four tasks sharing a blocker with no chain between them. `to-tickets` emits edges and nothing else. |
| 6 | `waiting on producer deploy`'s verb is re-check, not re-run | **Phase 3.** The state does not exist until seams do. (Rev 4: the mechanism is `gh run rerun <id>` — no gating workflow has `workflow_dispatch`, so rev 3's "re-dispatch" named a thing that doesn't exist.) |

**One live must-fix — the CI verdict — and it is about reading the world correctly.** The
four that dissolved were all about the app acting on state it computed, which is exactly the
class that bounding scope and deleting machinery removes. The verdict cannot dissolve: it is
the only gate, and revisions 1–3 each got a different part of its input wrong, which is why
revision 4 specifies the query, the normalisation and the expiry scope rather than trusting
a one-line description of them.

## Phases

### Phase 1 — one repo, a slice of two

Enough graph that the tick's third job actually fires, over primitives that are unproven until it
runs.

**Requirements.**

- One configured repo, one running agent at a time, `stacking = false`. A slice of two
  tickets, one blocking the other, with the preview shown before anything is cut.
- The tick's three jobs, observe → decide → act, with job 3 firing exactly once — when the
  first ticket's PR opens. A failed GitHub read ends the tick before anything changes.
- `tp new --base origin/main` for the root; **with `stacking` off the second cuts off
  `origin/main` too**, so Phase 1 proves launch-on-unlock without proving stacked bases —
  using the same unlock function Phase 2 flips the flag on, not a different rule.
- `gh pr create --fill --label keep-open` (adopting an existing PR, never duplicating);
  `tp remove --force` on teardown.
- Spawn `claude -p` with `Setpgid` and an app-owned `--settings` file denying `git push` and
  `gh`; output redirected to a run log; no `ANTHROPIC_API_KEY` inherited, no `--bare`.
- Liveness by `ps -o stat=,etime=` + start-time match (§"A run"); disposition from commits after the
  baseline, recorded as data.
- Per-repo push policy: `.github/`, `.mergify.yml`, `CODEOWNERS`, lockfiles, every
  `package.json`, `pnpm-workspace.yaml`, `.npmrc`, `.env*`, plus `support-app`'s scripts and
  build configs.
- `verdict/` as a pure package over the normalised snapshot, with both repos' predicates as
  test fixtures.
- The states above on one page bound to `127.0.0.1`; mutating verbs POST-only; requests with
  an absent or mismatched `Origin` rejected — `kill` and `remove worktree` sit behind it on a
  machine that also runs a browser. Tick age and last tick error on the page.
- Crash recovery: flock, re-read runs, re-check liveness, resume; intent recorded before
  every outward effect.

**Entry criteria (spikes — hours, no Go).** One hand-made two-PR stack: merge the parent,
watch whether GitHub retargets the child and what Mergify's checks show, then queue the
retargeted child. And one `--fill`-style PR from an agent-shaped branch name, to confirm the
required Linear check passes — if it doesn't, every Phase 1 PR parks in `needs you` and the
branch-naming convention needs settling first.

**Which repo.** `support-app`. Its five gating checks are already right and its predicate has no
deployment branch. `services` means dozens of jobs and an AWS stage deploy per push, plus the
conditions that are not plain names — worth the predicate package handling, not worth being first.

**Nice to haves ✨.** The run log tailed inline rather than a path to paste. Elapsed time as a
live counter. (Tick-age was here in revision 3; it is a requirement now.)

**Out of scope for Phase 1.**

- **Stacked bases** → Phase 2 (`stacking = true`). With the flag off and verdict expiry
  scoped to stacked bases, `base moved` and `refresh` are genuinely unreachable in Phase 1 —
  a claim that is now true *because* the expiry is scoped; unscoped, `main`'s daily drift
  would have fired it hourly.
- **Concurrency, `max_agents > 1`, fan-out** → Phase 2. Untested primitives are the wrong thing to
  run four of.
- **Seams and cross-repo** → Phase 3, blocked on `to-seams` not existing.

### Phase 2 — the backlog

The shape the product is for: authorise a feature, one ticket lands first, four parallel tickets
start themselves the moment its PR opens, and an hour later there is a stack waiting for review.

Stacked bases via `stacking = true` (`--base <parent branch>` on both `tp new` and
`gh pr create`); `max_agents` as a real cap; slices of arbitrary size with the full preview;
`cancel launch`; `base moved`, `refresh` and the retarget-on-parent-merge; and the verdict's
stacked-base expiry, which only becomes reachable once anything stacks.

**The one new risk:** Mergify's queue never sees a stacked PR (`base=main` gates it), so
everything rides on the retarget step and on the queue's behaviour on a just-retargeted
child — which is exactly what the Phase 1 entry spike measures before any of this is built.

### Phase 3 — cross-repo

Seams as prompt context, the draft gate, `compat_check`, `waiting on producer deploy` with
`re-check` as its verb and infrastructure-caused transitions not charging attempts, and the new
`to-seams` skill.

## Dependencies

| Thing | State |
|---|---|
| **`tp remove --force`** (treepad) | **Blocks Phase 1 teardown.** The plumbing exists — `lifecycle.go:243` builds `git worktree remove --force` and `git branch -D` — but no CLI flag reaches it, so `tp remove` uses `git branch -d` and refuses a squash-merged branch. `.mergify.yml` sets `merge_method: squash`, so that is *every* merged ticket. Small, in a repo you own. |
| **`to-tickets` emits `blocked_by[]`** | Phase 2. Edges only, upserting on `ticket_url`, no chain semantics — and its manifest-emission step (plus `references/treepad-manifest.md`) gets deleted, since nothing reads manifests any more. |
| **Retarget behaviour × Mergify queue** | **Phase 1 entry spike.** Two hand-made stacked PRs answer what config files can't: auto-retarget timing on branch deletion, and the queue on a retargeted child. `base=main` gating the queue is already answered by the files. |
| **`--fill` × the Linear check** | **Phase 1 entry spike.** A required check the app's predicate includes; if agent branch names fail it, every Phase 1 PR parks in `needs you`. |
| **`to-seams`** | Phase 3. Does not exist. |

**No longer needed:** the `link`/`restack` Activity-file veto in treepad — with no manifests
there are no members for `Reconcile` to act on — and any Activity-file handling in the app
at all (the file's readers are all manifest-gated).

## Out of scope

- **Work discovery.** The app never chooses what to work on. No Linear polling, no scanning for
  new tickets, no writing its own task rows, and no launching an unlocked task outside a slice you
  authorised. Autonomy exists strictly inside a launch.
- **Repairing stacks.** No moved-base detection beyond a SHA comparison, no `rebase --onto`, no
  patch-equivalence checks, no `gh stack link`. When a parent moves you merge it in; when a
  parent lands, the app re-points the child at `main` and Mergify's queue integrates it from
  there.
- **Merging, rebasing and running tests.** CI decides whether the work is sound; you decide done
  by merging.
- **CI retry with the failing log.** It races the repo's own retries, cancels the run it reacts to
  by pushing, spends shared capacity, and can present a worse branch as `review me`.
- **T3 Code as the substrate.** Its server does session lifecycle, push, PR create and rollup
  reads over typed RPC, and dispatching to it would delete `runner.go` and buy phone access.
  Declined: it replaces the two primitives this design exists to get right with trust in someone
  else's session status, its worktree driver would fight `tp` over the same checkout, and it has
  no dependency model — so unlock, the point, would still be yours to write.
- **A TUI, Slack/OTel egress, multi-machine, `POST /tasks`.** A localhost page is the bet.

## Open questions

**Product**

- **Does a `base moved` descendant refresh automatically, or wait for you?** Automatic keeps the
  stack coherent without attention; manual avoids surprising a running agent with a merge in its
  worktree. Leaning: automatic when no agent is live in that worktree, the verb otherwise.
- **Can you add to an active launch,** or does authorising more work always create a second one?
  Two active launches over overlapping tasks needs a rule; one mutable launch needs an audit trail.
- Does `re-run` on a task that already has a worktree and commits go through the preview?
- Where does intake come from in Phase 1 — a row you insert by hand, or a file the app reads?
- Should local check results gate a push? `/implement` already runs the suite; the app ignores the
  result. Reading it would cut the chance a parent needs a fix, which is the thing that causes
  `base moved` in the first place. Weak predictor of CI green on `services` though.

**Technical**

- `checking` needs a bounded wait before it gives up on a rollup appearing. What number?
  (Settled: clocked from `pushed_at`, counted only over ticks whose reads succeeded, exits
  to `needs you`.)

<details>
<summary><b>Resolved</b></summary>

- **Scheduler or control plane?** → control plane with autonomy bounded to what you authorised.
  The tick *does* launch agents, but only inside a slice you said yes to, and it never discovers
  work.
- **What is the unit of consent?** → a launch: a set of tasks the tick may drive. Not a per-agent
  click (that means babysitting unlock), not the whole task table (that means the app choosing
  work).
- **Should stacking be cut to simplify?** → **no.** Considered and rejected: if unlock waits for
  merge, throughput is capped at the reviewer's queue and the app is a nicer way to run one agent
  at a time. Building a backlog of reviewable PRs without being blocked *is* the product.
- **How are stacks repaired?** → they are not. `git merge` the parent in when it moves;
  re-point the child at `main` when the parent lands; Mergify's queue integrates from there.
  The rebase machinery existed to preserve history that squash-merge (server-enforced)
  discards.
- **Auto-push, or a verb?** → automatic. The launch decision is yours; everything after it is the
  automation, guarded by the path denylist.
- **Does launch preview before applying?** → yes, and it shows what starts now versus on unlock.
- **Does the tick retry a failed task?** → no. Each authorised task launches exactly once; a
  failure stalls the slice visibly and retrying is your verb.
- **Spawn agents, or dispatch to T3 Code's server?** → spawn. See Out of scope.
- **Can the verdict read Mergify's own check-run?** → no. It folds in approval and draft state,
  both false exactly when the app wants to say `review me` — confirmed on live PRs (FAILURE
  while every gating check was green).
- **What does `refresh` do when the merge conflicts?** → a state: `refresh conflicted`, the
  worktree left mid-merge for a human, verbs `abort` and a shell. Never handed to an agent —
  that would be the app merging by proxy.
- **What stops a cancelled launch's already-cut worktrees from lingering?** → a state:
  `cancelled`, verbs `remove worktree` and `launch`. And cancel does not orphan in-flight
  work — the tick still pushes and reads CI for agents already running.
- **Where does the check predicate live?** → hand-written in `command-centre.toml`, with a
  recorded hash of `.mergify.yml` that suppresses green and flags the row when the source
  moves. The parser is deferred until the hash fires more than twice.
- **`.env` in treepad's `[sync] include`?** → taken: the no-op `.env` lines are deleted. The
  file that is both secret-bearing and synced (`.env.development.local`) stays — worktrees
  need it to boot — covered by the agent's deny-settings and the `.env*` push policy.

</details>
