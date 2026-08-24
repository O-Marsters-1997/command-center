# Plan: Command Centre — Phase 1.5 and Phase 2 (the backlog)

> Source: [`docs/prds/prd-command-centre.md`](../docs/prds/prd-command-centre.md) § Phases → Phase 1.5,
> Phase 2. Mechanism, state list and invariants are normative in
> [`docs/designs/command-centre-design.md`](../docs/designs/command-centre-design.md) (revision 4) — cited as `§n`
> and `inv. n` below. Where this plan and the design disagree, the design wins, except at the two
> places this plan says so explicitly and gives its reason.
>
> Predecessor: [`plans/command-centre-phase-1.md`](command-centre-phase-1.md). Its Go conventions,
> testing layers, on-disk layout and schema hold unchanged and are not restated here.

Phase 2 goal: **authorise a feature, one ticket lands first, four parallel tickets start
themselves the moment its PR opens, and an hour later there is a stack of PRs waiting for
review.** Phase 1 built the plumbing behind a flag; most of this plan turns it on and proves
it, then adds the three things that genuinely do not exist — `cancel`, `refresh` and the
retarget.

Phase 1.5 comes first and builds nothing: it moves the app off `cc-sandbox` and onto
`support-app`, which is also the only place the two skipped spikes can run.

## Preconditions

Nothing after Phase 0 can be trusted until Phase 0 answers two questions, both of which were
Phase 1 entry criteria that never ran:

| Precondition | Why it gates |
|---|---|
| Retarget behaviour × the Mergify queue | Mergify's queue never sees a stacked PR (`base=main` heads `common_checks` in both repos), so the entire throughput claim rests on the retarget step and on the queue accepting a just-retargeted child. |
| `--fill` × the required Linear check | A required check the predicate includes. If an agent-shaped branch name fails it, every stacked PR parks in `needs you` and the branch-naming convention needs settling before anything is built. |

Both need a repo where Mergify is actually installed. `cc-sandbox` is not one, which is why
Phase 0 runs against `support-app` and Phase 1 (the cutover) follows it rather than the reverse.

## Technical design decisions

### The two blocking open questions, now settled

**A `base moved` descendant refreshes automatically when no run is live in its worktree**
(PRD § Open questions, taking its own leaning). The tick refreshes such a row as part of job 1;
the `refresh` verb remains for the row whose worktree holds a live run, and as the retry after
an `abort` or a refused fast-forward. Rationale: inv. 4 already forbids a git write in a
worktree with a live run or an unresolved merge, so the guard the manual option was protecting
is a precondition the automatic path has to check anyway — and four descendants behind one
parent fix is four clicks otherwise.

**Authorising more work always mints a second launch.** `launches` stays append-only; no
membership is ever added to an existing row. The overlap rule is one line in the preview: a
task already in an *active* launch is labelled `refused` with "already authorised in launch N".
A task in a *cancelled* launch is launchable again, and its new membership is what supersedes
the old one.

### Config after the cutover — `plain/.claude/command-centre.toml`

Phase 1's shape, with the sandbox substitutions removed. Three edits are the whole of Phase 1.5,
and one more is the whole of Phase 4's turn-on:

```toml
max_agents    = 4                     # Phase 4; stays 1 through Phase 1.5 (§Phase 1.5)
port          = 7777
agent_command = ["claude", "-p", "--settings", "{settings}", "{prompt_file}"]
                                      # was testdata/agents/*.sh — the cutover's one-line edit

[[repo]]
name         = "support-app"
path         = "support-app"          # relative to the workspace root
stacking     = true                   # Phase 4 flips it; false through Phase 1.5
compat_check = "GraphQL production compatibility"
mergify_sha  = "sha256:…"             # sha256 of support-app's own .mergify.yml, recorded by hand
deny         = ["scripts/**", "codegen.yml", "next.config.js",
                "vite.config.ts", "eslint-ci.config.ts"]

[repo.checks]                         # transcribed from support-app's .mergify.yml, not copied
                                      # from cc-sandbox's mirror. Its five real gating names,
                                      # plus the Linear check and the path-filtered
                                      # absent_ok arm.
```

The predicate is **re-transcribed from the real file at cutover**, not lifted from the sandbox's
mirror: the mirror was written to exercise the grammar, and a name that differs by one word is
exactly the failure mode must-fix 1 exists for. `mergify_sha` is recorded from the same reading.

`[[task]]` rows stop being hand-written at Phase 2 (`to-tickets` writes them), which is the only
reason that skill change sits inside this plan.

### New states, new verbs

Three states and three verbs. `plan.State` and `plan.Verbs` are the only place either is
spelled; `exhaustive` in `.golangci.yml` turns each addition into a build failure at every
switch that has not handled it, which is the mechanism that finds the call sites.

| State | Constant | Derived when | Verbs |
|---|---|---|---|
| `cancelled` | `plan.Cancelled` | member of a `cancelled` launch, not in any active launch, no run | `launch` |
| `base moved` | `plan.BaseMoved` | stacked base whose recorded base SHA ≠ `origin/<parent>`'s tip | `refresh`, `re-run` |
| `refresh conflicted` | `plan.RefreshConflicted` | the worktree holds an unresolved merge | `abort` |

| Verb | Constant | Does |
|---|---|---|
| `cancel` | `plan.VerbCancel` | withdraws consent for the whole launch the row belongs to |
| `refresh` | `plan.VerbRefresh` | §4a's three-step contract, in the worktree |
| `abort` | `plan.VerbAbort` | `git merge --abort` in the worktree |

`Verbs(Queued)` becomes `[cancel]` — the state that currently offers nothing at all.

**Deviation from design §5, stated deliberately:** `cancelled` offers `launch` only, not
`remove worktree`. Cutting and spawning are one step (`cutAndSpawn` → `spawnRun` inserts the run
skeleton immediately after `tp new` returns), so a member with a worktree always has a run row,
and a cancelled member — which by definition never launched — never has a worktree. The verb
would be permanently refused with "no worktree", which is worse than its absence.

**`cancel` is launch-scoped, not row-scoped.** The button lives on a row because that is where
the page is, but it cancels the launch that row belongs to and the page labels it
`cancel launch (N tickets)`. Consent was given for a slice; withdrawing it per-row would leave
a slice half-authorised, which is not a thing the design has a word for. Members already running
are untouched: `pushPushable`, the verdict step and `recordVerdictTransitions` never consult
launch membership, so in-flight work keeps being pushed and read exactly as the PRD promises.

### New facts, and where each comes from

Nothing latches; every fact below is re-derived each tick from the world, the event log or the
launch tables (inv. 14). **No DDL** — schema version stays 1, per its own header comment.

| Fact | Source | Reaches `plan.Status` as |
|---|---|---|
| cancelled membership | `launches.state = 'cancelled'` ⋈ `launch_members` | `Facts.CancelledMember` |
| base moved | `Observation.BranchTips[parent]` vs `pushes.base_sha_at_push` | `RunFact.VerdictBaseMoved` |
| unresolved merge | `Observation.MidMerge[branch]`, read from the worktree | `RunFact.MidMerge` |
| refused fast-forward | latest `refresh_refused` event since the task's last push | `RunFact.RefreshRefused` |

`Observation` grows two fields (it is persisted as JSON in `meta`, so this costs no migration):

```go
type Observation struct {
    // …existing fields…
    BranchTips map[string]string `json:"branch_tips"` // git rev-parse origin/<branch>, post-fetch
    MidMerge   map[string]bool   `json:"mid_merge"`   // MERGE_HEAD resolves in that worktree
}
```

`BranchTips` replaces the current `obs.PRs[base].HeadOid` stand-in for the base's tip. §4a
specifies "`origin/<parent>`'s tip post-fetch", and a git fact should come from git: the PR
snapshot's `headRefOid` is one indirection off it, and it is whatever GitHub last reported,
which is not the same thing after a reviewer's push.

`MidMerge` is read, not recorded, on purpose: a human who resolves the conflict by hand and
commits clears `refresh conflicted` without telling the app, and `abort` needs no bookkeeping
beyond running.

### `internal/verdict` — one new verdict value, and the expiry moves

```go
const ( Checking Verdict = iota; ReviewMe; NeedsYou; BaseMoved )   // BaseMoved is new
```

The stacked-base expiry moves **ahead of** predicate resolution:

```go
func Evaluate(p Predicate, in Input) Result {
    if in.StackedBase && !in.BaseSHAMatch {
        return Result{BaseMoved, "base moved: the parent advanced past what this branch was cut from"}
    }
    // …unchanged from here…
}
```

Today it only downgrades a green verdict. §4a marks the row `base moved` on the SHA comparison
alone, and the PRD's own scenario is the reason: the descendant's code does not contain the
parent's fix, so **a red check on it may not be its fault** — reading `needs you` sends the user
to debug a failure that a refresh dissolves. `Input` is unchanged; `internal/cc` keeps computing
`BaseSHAMatch`, now off `BranchTips`.

The row also renders the **base's verdict** alongside (PRD § The page; §4b for the preview). One
helper in `internal/cc`, called from both `derive` and `handlePreview`, since "you are about to
build on a red parent" is the same sentence in both places.

### `internal/gh` — one new call

```go
// Edit re-points branch's pull request at base. Idempotent: GitHub's own
// delete-branch-on-merge retarget may have got there first (§4a).
func Edit(ctx context.Context, repoPath, branch, base string) error   // gh pr edit <branch> --base <base>
```

That is the whole of the retarget on the GitHub side, exactly as `--base` was the whole of
stacking.

### `internal/cc` — three new loop steps, and their order

`RunOnce`'s sequence gains four calls. The order is the design, not taste:

```
observe → tickCheckingWaits → SaveObservation → ApplyLaunchIntents
  → applyCancelIntents      ← NEW  before job 3 reads membership: that is the race-free cancel
  → applyKillIntents → reconcileRuns → SaveObservation
  → applyAbortIntents       ← NEW  a human's abort clears the mid-merge that blocks refresh
  → retargetMerged          ← NEW  before refresh, so a merged parent's refresh targets origin/main
  → applyRefreshIntents     ← NEW  and the automatic pass; before push, so the merge ships this tick
  → applyRetryPushIntents → pushPushable → recordVerdictTransitions
  → applyReRunIntents → applyClosePRIntents → applyRemoveWorktreeIntents
  → launchEligible
```

**`refresh` before `push` is what makes the verb work at all.** §4a records that revision 3's
`refresh` had no path to GitHub; here the merge commit moves the local tip past
`pushes.pushed_tip`, so `plan.PushPlan` selects the row on this same tick with no new push code.

**`refresh` (`internal/cc/refresh.go`)** implements §4a's contract in order, and any step that
fails records an event and touches nothing further:

1. Precondition — no live run (`obs.Runs[task].Alive`) and no unresolved merge
   (`obs.MidMerge[branch]`). Inv. 4.
2. `git merge --ff-only origin/<branch>` in the worktree — reviewers' "commit suggestion" clicks
   and Mergify's `update_method: merge` both push to the app's own head branches. Not a
   fast-forward → `refresh_refused` event → the row reads `needs you`. The app never rewrites
   history.
3. `git merge origin/<base_branch>`, base recomputed by the same `plan.Unlocked` call the push
   step uses. Conflict → the worktree is left mid-merge, and next tick's `MidMerge` derives
   `refresh conflicted` with no event needed.
4. Nothing else. The push step delivers.

The automatic pass is the same function over every `base moved` row that clears step 1 and has
no outstanding `refresh_refused` — a refusal is never retried automatically, exactly as a push
failure is not.

**`retargetMerged` (`internal/cc/retarget.go`)**: for every task whose latest push recorded a
non-`main` base, whose own PR is open, and whose base branch's PR reads `Merged` —
`gh.Edit(repoPath, branch, "main")`, then `RecordPush(task, samePushedTip, "main", tipOf(origin/main), now)`.

Reusing `RecordPush` rather than adding a store method is deliberate: appending a `pushes` row
with the **same** `pushed_tip` records the new base without making `plan.PushPlan` think there is
something to push, and `LatestPushes` picks it up by `MAX(id)`, which is what retires the stacked
expiry for that row.

### Store additions

```go
func (s *Store) CancelLaunchesFor(ctx context.Context, taskID string, at time.Time) (members int, err error)
func (s *Store) CancelledMemberships(ctx context.Context) (map[string]bool, error)
func (s *Store) RefreshFacts(ctx context.Context) (map[string]RefreshFact, error)
```

`RefreshFacts` mirrors `PushFacts` exactly — latest `refresh_refused` event per task since that
task's last recorded push, so a later successful refresh-and-push clears it. New event kinds:
`launch_cancelled`, `refresh_refused`, `refreshed`, `merge_aborted`, `retargeted`.

### Routes

Unchanged. `POST /verb` accepts three more values in `supportedVerbs`; `handleVerb` still does
one blind INSERT into `intents` and the loop remains the sole DB writer (inv. 9). No new route:
`cancel` resolving to a launch happens in the loop, where it can be transactional.

### Testing strategy — the delta from Phase 1

Layers and tooling are Phase 1's. What is new:

- **`fakegh`** is keyed on the first two argv words, so `pr edit` needs a fixture entry and an
  argv assertion, not new dispatch code.
- **A parent-advances fixture**: real `git` in the e2e sandbox can move a parent branch, so
  `base moved`, `refresh`, the conflict path and `abort` are all layer-2 tests with no network.
- **New txtar scripts**: `cancel_launch.txtar`, `fan_out_four.txtar` (1 + 4 under
  `max_agents = 4`), `max_agents_caps.txtar` (the same slice under `max_agents = 2`, proving two
  defer), `stacked_base_and_body.txtar` (`--base <parent>` on both `tp new` and `gh pr create`,
  plus "Merge after #N"), `base_moved_refresh.txtar`, `refresh_conflicted_abort.txtar`,
  `retarget_on_merge.txtar`.
- **The page's golden file** is regenerated once per new state, as a reviewed diff.
- **Live criteria** are marked *(live)* below. They run against `support-app` as a daemon, and
  they are the only place `claude -p`, the app-owned deny settings and Mergify's real queue are
  exercised.
- **[2026-08-24] Every *(live)* criterion is dropped as a gate.** They no longer block closing a
  phase, and #39, the ticket that carried the Phase 4-6 ones, is closed unrun. Nothing has
  exercised Mergify's queue, and no fan-out larger than one task has run against a real repo, so
  the first real one is the test. `claude -p` and the deny settings are the exception: those ran
  live on this repo for #37 and #38.

### Out of scope

Seams, the draft gate, `compat_check`, `waiting on producer deploy` and `re-check` are Phase 3
and blocked on `to-seams` not existing. A second configured repo is Phase 3 too, which is what
keeps `Observation`'s branch-keyed maps honest for one more phase — `observe.go` already carries
the note that they must become `(repo, branch)`-keyed when a second repo lands.

---

## Phase 0: The two spikes

**User stories**: PRD § Phase 2 → "Blocked until both Phase 1 entry spikes run"; § Dependencies
rows 3 and 4.

### What to build

No Go. Two hand-made experiments on `support-app`, each written up in
`docs/architecture-reviews/` as an answer, not a summary.

**Spike A — retarget × the queue.** Two stacked PRs by hand: branch `a` off `main`, branch `b`
off `a`, a PR each with `--base` set accordingly. Merge `a`'s PR (squash, as the server
enforces). Then observe, in order: whether GitHub retargets `b` to `main` on branch deletion and
how long it takes; what `b`'s checks read immediately after; whether `gh pr edit --base main` on
an already-retargeted PR errors or no-ops; and whether Mergify's queue accepts `b` once it is
`main`-based.

**Spike B — `--fill` × the Linear check.** One PR opened with `gh pr create --fill --label
keep-open` from an agent-shaped branch name (the `to-tickets` convention, e.g.
`pla-12-short-slug`), body untouched. Whether the required Linear check passes, and if not, what
in the branch name or body it wants.

### Acceptance criteria

- [ ] Spike A's write-up answers all four observations, with the timings observed rather than
      assumed, and states whether `gh pr edit --base main` is safe to call unconditionally.
- [ ] Spike B's write-up states whether an agent-shaped branch name passes the Linear check, and
      if not, names the branch-naming rule `to-tickets` must adopt.
- [ ] Both PRs and both branches are cleaned up.
- [ ] If either answer invalidates a decision in the PRD or design, the doc is edited before
      Phase 1 starts — this plan does not carry a contradiction forward.

---

## Phase 1: Cutover to `support-app` (Phase 1.5)

**User stories**: PRD § Phase 1.5 in full.

### What to build

Config and verification only, and that is the point: the app is repo-agnostic by construction,
so if the cutover needs Go, that is the finding.

Add the `[[repo]]` block above with `support-app`'s **own** five gating names transcribed from
its `.mergify.yml`, its own `sha256`, and its real `deny` additions. Switch `agent_command` to
`claude -p`. Keep `stacking = false` and `max_agents = 1`. Two real tickets, one blocking the
other, as `[[task]]` rows.

Then re-run Phase 1's acceptance criteria live: the launch preview, one authorisation producing
two PRs with no further human action, the verdict going green and then red, a real squash-merge
deriving `merged` and `remove worktree`, and a `SIGKILL` mid-slice resuming without relaunching.

### Acceptance criteria

- [ ] `support-app`'s predicate is transcribed from its own `.mergify.yml`, and its
      `mergify_sha` is that file's hash — neither copied from `cc-sandbox`. *(live)*
- [ ] `repocheck.go`'s startup assertion passes against `support-app` (squash-only,
      delete-branch-on-merge). *(live)*
- [ ] A real `claude -p` run is spawned with the app-owned `--settings` file, inherits no
      `ANTHROPIC_API_KEY`, and its own `git push` and `gh` attempts are denied — inv. 17
      asserted behaviourally for the first time. *(live)*
- [ ] `.env.development.local` exists in the cut worktree (treepad's `[sync]`) and the push
      policy refuses a diff that touches it. *(live)*
- [ ] One authorisation of the two-ticket slice produces two open PRs on `support-app`, the
      second launching only when the first's PR opens. *(live)*
- [ ] The required Linear check passes on both PRs, per Spike B's answer. *(live)*
- [ ] A green verdict is reached against the five real check names, and flipping one to red
      moves the row to `needs you`. *(live)*
- [ ] Squash-merging the first PR derives `merged`, and `remove worktree` succeeds against the
      deleted branch. *(live)*
- [ ] Nothing in `internal/` changed. Any Go edit this phase needed is written up as a finding.

---

## Phase 2: `to-tickets` emits `[[task]]` blocks

**User stories**: PRD § Dependencies → "`to-tickets` emits `blocked_by[]`"; § Out of scope →
"Work discovery" (the app never writes its own intake).

### What to build

In the skills repo (`~/.agents/skills/to-tickets/`), not this one.

A new step emitting `[[task]]` blocks for the tickets it just filed — `ticket_url`, `repo`,
`branch`, `blocked_by` — into the workspace's `command-centre.toml`, upserting on `ticket_url` so
re-running an edited feature updates rows rather than minting duplicates. **Edges only, no chain
semantics:** a fan-out is four tasks naming one blocker, and the app derives every base itself.

Branch names follow whatever Spike B settled. The manifest step (SKILL.md § 7) and
`references/treepad-manifest.md` are deleted: nothing reads manifests any more (§ The Activity
file), and leaving a live path that writes them invites treepad's `Reconcile` back into a design
that removed it.

### Acceptance criteria

- [ ] A feature's tickets produce `[[task]]` blocks whose `blocked_by` names ticket URLs, not
      indices or chains.
- [ ] Re-running over the same feature with one ticket's blocker changed updates that row and
      adds none.
- [ ] A fan-out (one ticket blocking four) emits four rows each naming the one blocker, and
      nothing that implies an order between the four.
- [ ] The manifest step and its reference file are gone from `SKILL.md`, and no step writes to
      `treepad/batches/`.
- [ ] The emitted file loads through `cc.LoadConfig` unedited, and the app's page renders the
      graph the tickets describe.

---

## Phase 3: `cancel launch` and the `cancelled` state

**User stories**: PRD § A launch → "cancel launch stops the tick starting anything further from
that slice and leaves running agents alone"; § The states → `queued`, `cancelled`; design §4b.

### What to build

The first slice with new code, and deliberately the one that needs neither stacking nor a live
repo.

`plan.VerbCancel` and `plan.Cancelled`, `Verbs(Queued) = [cancel]`, `Verbs(Cancelled) =
[launch]`. `Facts.CancelledMember`, derived in `Status` after the run checks and before the
unlocked × authorised 2×2 — a row that ever ran is never `cancelled`.

`POST /verb` accepts `cancel` and queues the intent. `applyCancelIntents` runs before
`launchEligible`: it resolves the task's active launches, sets each to `cancelled`, and appends
one `launch_cancelled` event naming the member count. Every queued sibling reads `cancelled` on
that same tick's render; nothing kills, nothing removes.

The preview gains the overlap rule: a task already in an active launch is `refused` with
"already authorised in launch N". A cancelled member is launchable again, and its new membership
is a new `launches` row.

### Acceptance criteria

- [ ] A `queued` row offers `cancel`, labelled with the number of tickets it withdraws.
- [ ] Cancelling a slice with one running and three queued members: the three read `cancelled`
      on the next tick, and the running one is untouched — no kill, no worktree removal.
- [ ] The tick still pushes and opens the PR for that running member, and still reads its
      verdict, after the cancel.
- [ ] `launchEligible` starts nothing from a cancelled launch, including a member that unlocked
      on the very tick the cancel was applied.
- [ ] A cancelled member offers `launch`, and launching it creates a second `launches` row
      rather than mutating the cancelled one.
- [ ] The preview refuses a task that is already a member of an active launch, naming the
      launch.
- [ ] `events` carries `launch_cancelled` with its member count, and the page's golden file
      covers a `cancelled` row.

---

## Phase 4: `stacking = true`, and the fan-out

**User stories**: PRD § Scenarios → "Authorising a feature with a fan-out in it"; § Phase 2 →
"Already built, waiting to be proved"; § The page → stack order.

### What to build

Mostly a flag flip, and the tests that make the flip a claim rather than a hope.
`plan.Unlocked`'s stacked arm, `tp new --base <parent>`, `gh pr create --base <parent>` and
`plan.PRBody`'s "Merge after #N" all exist and are unreachable today.

Set `stacking = true` and `max_agents = 4`. Then prove, and fix whatever the proving finds:

- One root plus four dependents, one authorisation, four cut off the parent's branch the tick
  its PR opens.
- `max_agents` deferring the surplus: the same slice under `max_agents = 2` starts two and leaves
  two `queued` with "waiting for a slot", not "waiting on a base".
- Each stacked PR opened with `--base <parent branch>` and a body carrying "Merge after #N" —
  the reviewer never sees this page, so the order has to travel with the PR.
- The page rendering stack depth and merge order.
- The preview rendering **the base's verdict** for a row whose base is a blocker's branch
  (§4b), so "you are about to build on a red parent" is a line read before authorising.
- Inv. 2's warning: a non-`main`-based row carrying `ready-to-merge` is flagged on the page.
  Both repos would squash such a PR into its *parent branch*, checks unseen, and the app never
  applies that label itself.

### Acceptance criteria

- [ ] One authorisation of a 1 + 4 fan-out opens five PRs, four of them based on the root's
      branch, with no human action between the root's PR opening and the four launches.
- [ ] Each of the four worktrees was cut with `--base origin/<root branch>` — asserted off the
      `tp` argv, not inferred from the result.
- [ ] Each stacked PR's body contains "Merge after #N" with the root's real PR number, and each
      PR's `--base` is the root's branch.
- [ ] Under `max_agents = 2`, two of the four launch and two stay `queued` reading "waiting for
      a slot"; the next tick after a slot frees starts a third.
- [ ] A slice of arbitrary size previews and authorises with no size limit anywhere in the path.
- [ ] The preview shows each stacked row's base verdict, and a red parent renders as such.
- [ ] The page shows stack depth and merge order for the five rows.
- [ ] A non-`main`-based row carrying `ready-to-merge` renders a warning, and no app code path
      applies that label.
- [ ] A real fan-out runs on `support-app`: five PRs open, reviewer-ready, bottom-up order legible
      from the PR bodies alone. *(live)*

### Deliberately still unreachable

`refresh` and `refresh conflicted` become *reachable* here but are not built until Phase 5. A
`base moved` row in this phase renders the state with `re-run` as its only verb. That is honest
about what exists, and it lasts one phase.

---

## Phase 5: `base moved`, `refresh`, `refresh conflicted`

**User stories**: PRD § Scenarios → "The parent needs a fix"; § The states → `base moved`,
`refresh conflicted`; design §4a.

### What to build

`verdict.BaseMoved` and the expiry moving ahead of predicate resolution.
`Observation.BranchTips` and `Observation.MidMerge`. `plan.BaseMoved`, `plan.RefreshConflicted`,
`plan.VerbRefresh`, `plan.VerbAbort`, and their `RunFact` inputs.

`internal/cc/refresh.go` implementing §4a's four steps, `applyRefreshIntents` for the verb and
the automatic pass over every eligible `base moved` row, `applyAbortIntents` for
`git merge --abort`, and `Store.RefreshFacts` gating the automatic retry after a refused
fast-forward.

The row also renders the parent's verdict, since a red check on a descendant whose base moved
may not be its fault.

### Acceptance criteria

- [ ] Advancing a parent branch moves every descendant to `base moved`, whatever its own checks
      read — a red descendant included.
- [ ] A root row (`base_branch = main`) never reads `base moved`, across many `main` advances.
- [ ] The automatic refresh fires on a `base moved` row with no live run: `merge --ff-only`
      against its own upstream, then `git merge origin/<parent>`, and the merge commit is pushed
      on that same tick by the existing push step.
- [ ] A `base moved` row whose worktree holds a live run is not touched (inv. 4) and offers
      `refresh` as a verb.
- [ ] A conflicting merge leaves the worktree mid-merge and the row reads `refresh conflicted`,
      derived from `MERGE_HEAD` rather than from a stored flag, with the worktree path as
      copy-pasteable text.
- [ ] `abort` runs `git merge --abort` and the row leaves `refresh conflicted` on the next tick's
      observation.
- [ ] A human resolving the conflict by hand and committing also clears the state, with no verb.
- [ ] A refused fast-forward reads `needs you` naming the reason, and the automatic pass does not
      retry it; the `refresh` verb does.
- [ ] The descendant PR's three-dot review diff stays clean after two successive refreshes —
      the parent's merge commits do not pollute it. *(live)*
- [ ] A refreshed descendant reaches `review me` against the parent's new tip. *(live)*

---

## Phase 6: The retarget on parent merge

**User stories**: PRD § Important technical details → "When a parent merges, the app retargets";
design §4a's second cause.

### What to build

`gh.Edit`, `internal/cc/retarget.go`, and the `RecordPush` append that retires the stacked
expiry for the retargeted row.

The step runs before `refresh`, so a descendant whose parent merged has `base_branch = main`
recorded before anything tries to merge `origin/<parent>` — a branch GitHub has deleted. From
there `plan.Unlocked`'s merged-blocker arm already returns `main`, so push, verdict and refresh
all target `main` with no further change, and Mergify's queue takes it.

`gh.Edit` is called unconditionally when the base's PR reads merged and our recorded base is not
`main`, per Spike A's answer on idempotency — GitHub's own retarget racing ours is the expected
case, not an error.

### Acceptance criteria

- [ ] Merging a parent re-points every open descendant PR at `main` via `gh pr edit`, asserted
      off the argv.
- [ ] A second tick over the same already-retargeted PR calls `gh pr edit` again without
      erroring, and appends no duplicate `pushes` row for an unchanged base.
- [ ] The retarget appends a `pushes` row with the same `pushed_tip` and `base_branch = main`, so
      `plan.PushPlan` selects nothing and the stacked expiry stops firing for that row.
- [ ] A retargeted descendant's verdict is evaluated as a root: no `base moved`, no expiry.
- [ ] `refresh` on a retargeted row targets `origin/main` and does not reference the deleted
      parent branch.
- [ ] The full scenario runs live on `support-app`: a 1 + 4 fan-out, the root merged, four
      descendants retargeted, and at least one of them accepted by Mergify's queue. *(live)*
- [ ] `events` carries `retargeted` per descendant.
