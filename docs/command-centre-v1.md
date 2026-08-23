# Command Centre — v1 design

**Date:** 2026-08-22 · **Revision 4** · **Status:** designed, not built

**Supersedes:** `.claude/handoffs/treepad__command-centre-architecture.md`, and revisions 1–3
of this document. Revision 1 was reviewed in
`docs/architecture-reviews/command-centre-v1-review.md`; revision 2 in
`command-centre-v2-buildability-review.md` (six mechanisms that do not work as written);
revision 3 in
`docs/architecture-reviews/command-centre-v3-phase1-foundations-review.md` (verdict: BUILD
AFTER FIXING 9, with five claims found false — three propping up deletions). **Revision 4 is
the response to that review.** §14 records what changed from revision 1; §15 from revision 2;
§16 from revision 3, including the false premises corrected.

Product framing lives in `prd-command-centre.md`; this document is the mechanism and is
**normative** where the two disagree (state list, unlock rule, invariants).

A local app that takes a DAG of tickets across several repos and drives them to reviewable
pull requests using agents, one worktree per ticket. **The user authorises a slice of work
and reviews the resulting PRs; everything between those two acts is the app's job.**

The purpose is a *backlog of reviewable pull requests produced without being blocked between
them*. A ticket's dependency is satisfied when its blocker's PR is **open**, not merged, so
throughput is decoupled from review latency. If unlock waited for merge, the app would be a
more elaborate way to run one agent at a time.

It does not decide *what* to build (planning skills do) and it does not perform git or
GitHub mechanics (`treepad` and `gh` do). It launches agents inside an authorised slice,
watches them, pushes what they produced, reads CI, and shows one page.

**Non-goals.** It never merges, never rebases, never runs tests, never repairs a stack, and
never decides a ticket is done. CI decides whether work is sound; the user decides done by
merging. **It also never chooses work**: nothing outside an authorised launch is ever
started.

---

## 1 · Division of labour

| Layer | Singularly responsible for | Must never |
|---|---|---|
| `to-plan` → `to-seams` → `to-tickets` | Above-ticket knowledge: which repos, which **edges** (`blocked_by[]`), which integration seams | Execute anything, or express chain semantics |
| **User** | Which work exists, which slice to authorise, review, merge | — |
| **App** | Launching agents *inside an authorised slice*, liveness, pushing, PR draft state, CI reading, retargeting a child PR when its parent merges, one status page | Merge, run tests, choose work, hold tracker or domain knowledge |
| `tp` (treepad) | Cutting and removing one repo's worktrees | Know that a workspace or a second repo exists |
| Mergify's merge queue | Integrating at merge time — merges `main` into the branch (`update_method: merge`, both repos) and re-runs CI. **Only PRs based on `main` enter it** (`base=main` is the first `common_checks` condition in both files), which is why the app owns the retarget step above |
| CI | Whether the work is sound | — |
| GitHub | Review, merge, deleting a merged branch (`deleteBranchOnMerge: true`, both repos — verified via API) | — |

**treepad's surface is two commands** — `tp new <branch> --base <ref>` and
`tp remove --force` — and never `tp batch sync`, `gh stack link`, or `restack`. Stacking
needs a `--base` on the worktree and a `--base` on `gh pr create`; it does not need batch
machinery. **The app learns worktree paths from `git worktree list --porcelain`**, never by
reimplementing treepad's path derivation — that keeps the contract surface at the two
commands rather than the five revision 3 silently depended on (path formula, Activity path,
slug fold).

The app never imports treepad's `batch/` package. This is **a rule, not a structure** —
`batch/` is exported and importable across repos — and it holds because `unlock` is a
*different* predicate (edges + PRs → base), not a re-implementation of `ready`. If the app
ever needs a treepad predicate verbatim, import it rather than mirroring it.

The app's unlock gate is **OPEN or MERGED**, matching `batch.ReadyToMaterialise`
(`batch/ready.go:20-22`) even though the app no longer calls that code path. Revision 1
claimed OPEN only, which stranded descendants whose parent merged before they got a slot.

## 2 · Data flow

```
spec.md
  │  to-plan          (one small feature; flags cross-repo seams)
  ▼
plans/<feature>.md
  │  to-seams         (only if >1 repo; halts for approval)
  ▼
plain/.claude/seams/*                        ← private, uncommitted
  │  to-tickets
  ▼
app task table
  (ticket_url, repo, blocked_by[], seams[])
  │
  │  USER selects a slice, reads the preview, authorises  → launch + members (with prompt hashes)
  ▼
  │  app tick, every ~15s:
  ▼
OBSERVE   git fetch origin --prune · PR snapshot (§3) · pid liveness · worktree facts
  │       any read fails → the tick stops here: no transitions, no launches (inv. 10)
  ▼
DECIDE    unlock(edges, PRs) → {unlocked, base_branch, reason}   (pure)
          disposition of dead runs → (outcome)                    (pure)
          push plan · verdicts · draft state · launch plan        (pure)
  ▼
ACT       push + gh pr create --base <branch> --fill --label keep-open
          tp new <branch> --base origin/<base_branch> · spawn agent
          gh pr edit --base main when a parent merged
  │  agent runs /implement → runs the repo's tests → commits → exits
  ▼
required checks green (stacked base unmoved) → "review me"
stacked base moved                           → "base moved"  (verb: refresh)
only the cross-repo compat check red          → "waiting on producer deploy"
anything else red                             → "needs you"
  │
  │  USER reviews and merges, bottom-up; the app retargets children; Mergify's queue integrates
  ▼
merge → dependents' unlock re-derived; seams marked landed
```

## 3 · The tick

One reconcile loop, level-triggered — it never branches on why it woke. **Sleep-after-work,
never overlapping**: the next tick starts ~15s after the previous one finishes. The tick has
three phases; the split is the design's central structural property, because it is what
makes a failed read safe (the tick simply does not act) and what makes every decision a
table test.

### Observe — all I/O that reads the world

1. **`git -C <repo> fetch origin --prune`** per repo. treepad never fetches before cutting
   (`git worktree add` uses the local ref, `lifecycle/lifecycle.go:69`; its only fetch is
   `internal/treepad/restack.go:87`). `--prune` because both repos delete branches on merge,
   and a stale remote-tracking ref would make a deleted base look cuttable.
2. **The PR snapshot**, two calls shaped by `gh pr list`'s real defaults (`--state open
   --limit 30` — which would hide merged PRs entirely and truncate below `support-app`'s
   open-PR count today):
   - one bulk read per repo:
     `gh pr list --state open --limit 100 --json
     number,headRefName,headRefOid,baseRefName,baseRefOid,isDraft,state,statusCheckRollup`
   - one fallback read per **tracked branch** absent from the bulk result:
     `gh pr list --state all --head <branch> --json number,state,baseRefName,headRefOid`
     — this is what detects MERGED and CLOSED. Tracked branches are the app's own task
     branches, so the fan-out is bounded by the working set (~10), not the team's PR volume.

   `internal/gh` normalises the result before anything else sees it: the rollup is a
   **multiset of a union type** (live data shows one check name appearing five times at one
   head SHA with mixed CANCELLED/SUCCESS, and `StatusContext` entries with no `name`), so it
   is reduced to **latest completed run per name** (by `startedAt`), with `CheckRun` and
   `StatusContext` collapsed into one `CheckState`. That reduction is the package's first
   table test. Nothing outside `internal/gh` touches gh's JSON shape.
3. **Liveness** per `running` task: `kill(-pgid, 0)` plus a process-start-time identity
   check. Alive → running, whatever the log says. No timing rule. Read-only here; the
   consequences are decided in the next phase.
4. **Worktree facts**: `git worktree list --porcelain` (the authoritative branch → path
   map), plus per active worktree: dirty?, commits since the run's baseline, unresolved
   merge?

**If `git fetch` or any `gh` read exits non-zero, the tick ends here** (invariant 10):
no state derived from GitHub changes, nothing launches, the failure lands in `events`, and
the page shows tick age and the last error. An empty PR list from a failed call must never
be read as "no PRs" — that mistake would walk every `checking` row to `needs you` and every
open-blocker dependent to `blocked` in one tick.

### Decide — pure, global, over the snapshot

5. **Intents.** Verbs clicked on the page since the last tick (they are queued, not applied
   inline — §8) are folded in as inputs.
6. **Unlock** (§4): `(edges, PR snapshot) → {unlocked, base_branch, reason}` per task. Pure;
   "would this launch, and off what, and why is it waiting?" is a table-driven test. The
   `reason` is what the page renders on `queued` rows.
7. **Disposition** of each dead run, as data: commits since *that run's* baseline →
   `outcome: push`; none → `outcome: failed`. A branch in the loop is not extensible; a
   `(kind, outcome)` pair is — a future local-verify run is a new row kind, not an edit.
8. **The push plan** (§7): every app-owned branch whose local tip differs from its last
   pushed tip, whose diff against its base clears the repo's push policy. This is deliberately
   a *reconciliation*, not an event: it delivers a dead run's commits, a `re-run`'s new
   commits, and a `refresh`'s merge commit through one code path.
9. **Verdicts** (§8) over the normalised snapshot; draft state (Phase 3); merges and
   closures; seam-drift hashes (§3.9 of revision 3's numbering — recompose, hash, compare;
   a mismatch flags the row `seam changed`).
10. **The launch plan**: every task that is unlocked, belongs to an **active launch**, whose
    recomposed prompt hash matches the hash recorded at authorisation (§4b), has no prior
    run, and fits under `max_agents`. **`max_agents` is global, applied after all repos'
    unlock results are in** — inside a per-repo loop it would deterministically starve every
    repo after the first.

    This step touches nothing outside an active launch. The tick never adds a task, never
    reads a tracker, never launches the same task twice; every relaunch is a user verb.

### Act — execute the plan

11. Pushes and `gh pr create --base <base_branch> --fill --label keep-open`. **If an open PR
    already exists for the branch, it is adopted, never duplicated** — that makes a crash
    between create and record a non-event. `keep-open` because both repos auto-close PRs
    after 14 days of inactivity (`.mergify.yml`, both), which would otherwise close a parked
    row and cascade its dependents.
12. Retargets: for each child whose parent merged, `gh pr edit --base main` (idempotent if
    GitHub's own retarget-on-branch-delete already did it) — §4a.
13. Cuts and spawns per the launch plan: `tp new <branch> --base origin/<base_branch>`,
    spawn. Intent is recorded before each outward effect (invariant 20), and worktree
    existence is re-read from git, not inferred from run rows — so a crash between cut and
    record is also a non-event.

Goroutines: **the loop and HTTP — two, not five.** Agent processes are polled by the tick
(a re-attached process after a crash is not a child, so `Wait` was never available in the
recovery path anyway; one mechanism for both cases). HTTP handlers write only the intent
queue. If tick duration becomes visible, the deferred tune is "at most one PR read in flight
per repo; the loop reads the last snapshot plus its age" (§12) — measured today, observe
costs ~6s on one repo, so Phase 1 (one repo) fits the 15s period with room.

## 4 · Unlock

Unlock is a **pure function of the dependency edges and the PR snapshot**, computed fresh
every tick, returning per task whether it can start, what its base is, and why it is
waiting:

```
unlock(task, prs) → { unlocked, base_branch, reason }

  edges are classed first:
    stacking edge — blocker in the same repo
    gating edge   — blocker in another repo (never affects unlock or the base; it feeds
                    the Phase-3 draft gate, §6)

  over stacking edges only:
    none               →  unlocked, base = main
    exactly 1, PR OPEN →  unlocked, base = the blocker's branch   (stacking on)
                          unlocked, base = main                    (stacking off)
    exactly 1, MERGED  →  unlocked, base = main
    2+                 →  unlocked iff EVERY blocker is MERGED, base = main
```

`base_branch` is a **branch name**. The two consumers need different forms and one value
cannot serve both: `tp new` takes a ref and gets `origin/<base_branch>` (a bare parent name
may not resolve locally — verified); `gh pr create --base` takes the branch name.

**Open is enough**, and this is the load-bearing decision. A descendant stacks on unlanded
work — that is what stacking means, and it is what keeps the fleet from idling on review
latency. **A draft PR counts as OPEN**: unlock asks only whether there is something to cut
from, and a draft's branch exists. (Whether building on a colleague's WIP is *wise* is the
launch preview's job to surface, not unlock's to forbid.)

**A merged blocker's base is `main`, not its branch.** Both repos set
`deleteBranchOnMerge: true`, so a merged blocker's branch does not exist; its change is in
`main`. Revision 3's "base = the blocker's branch" for the MERGED case produced a base that
is deleted on the happy path.

**`stacking` is a per-repo config flag** (§8). Off, unlock's *timing* is identical — a
dependent still launches the moment its blocker's PR opens — but every base is `main`.
Phase 1 runs with it off, which is what lets Phase 1 prove launch-on-unlock without proving
stacked bases, using the same function and the same tests rather than a different rule.

**Fan-out is not a chain.** Four tickets sharing one blocker each get base = that blocker's
branch. There is no chain between them, so nothing serialises them and there is no chain
contract for `to-tickets` to get wrong (must-fix 5 of the v2 review stays dissolved).
`to-tickets` emits `blocked_by[]` and nothing else.

**Fan-in cannot stack** — nothing can be cut from two branches at once — so a task with 2+
blockers takes `main` and waits for all of them to merge.

**Unlock does not start anything.** A task launches only when it is unlocked *and* belongs
to an active launch (§4b). Unlock alone puts a row in `ready`, where it waits for a human.

PR identity throughout is `(repo, headRefName)` — a branch name alone collides the moment a
second repo exists.

### 4a · Stacks are never repaired

When a parent branch advances — a review fix pushed onto it — its descendants do not have
that change. **The comparison exists only for stacked bases** (`base_branch` ≠ the repo's
default branch): the app compares the descendant's recorded base SHA against
`origin/<parent>`'s tip post-fetch, and marks the row `base moved`. A root's base —
`origin/main` — moves 130–187 times a fortnight on these repos (measured); being behind
`main` is *normal*, and integrating it is Mergify's queue's job (`update_method: merge`,
with a `Queue when ready` rule that exists precisely for `#commits-behind>=1`). Revision 3's
unconditional comparison would have made `base moved` the steady state of every root row.

`base moved` has **two causes with two repairs**:

- **The parent advanced.** Verb: `refresh`.
- **The parent merged.** GitHub deletes the branch and retargets the child PR to `main`; the
  app confirms with `gh pr edit --base main` (idempotent), records `base_branch = main`, and
  the row's `refresh` now targets `origin/main`. This is the *normal terminal case* of every
  stacked PR, not an edge — `origin/<parent>` ceasing to exist must not strand the verb that
  names it.

**`refresh` is a contract, not just a command:**

1. Precondition: no live run in the worktree and no unresolved merge (invariant 4).
2. `git fetch origin <branch> && git merge --ff-only origin/<branch>` — reviewers'
   "commit suggestion" clicks and Mergify's own `update_method: merge` push to the app's
   head branches, and `restack`, which used to reconcile a branch with its own upstream, was
   deleted. Not a fast-forward → `needs you`; the app never rewrites history.
3. `git merge origin/<base_branch>`. **Conflict → `refresh conflicted`**: the worktree is
   left mid-merge for a human, verbs `abort` (`git merge --abort`) and the worktree path as
   copy-paste text. The resolution is never handed to an agent — an agent resolving a merge
   is the app merging by proxy.
4. On success the local tip differs from the pushed tip, so **the push step delivers it**
   (§3.8). Revision 3's `refresh` had no path to GitHub at all: its invariant 1 pushed only
   dead runs with commits, so the verb that replaced all repair machinery was inert.

The repair is a merge, not a rebase, and the justification is now *stronger* than revision 3
claimed: squash is **server-enforced** in both repos (`allow_merge_commit: false`,
`allow_rebase_merge: false` — verified via API), so there is no history to preserve, and a
descendant PR's three-dot diff stays clean under repeated refreshes (verified — merge
commits from the parent do not pollute the review).

There is still no `rebase --onto`, no patch-equivalence check, no `gh stack link`, and no
`stack-stale` state. What revision 3 got wrong about the merge queue: it does not rebase
(`update_method: merge`), and **it never sees a stacked PR at all** — `base=main` is the
first `common_checks` condition in both repos, so a child enters the queue only after the
retarget above. The genuinely untested remainder — auto-retarget timing, and the queue's
behaviour on a just-retargeted child — is a pre-Phase-2 spike with two hand-made PRs
(§13.9). One related hazard, named as an invariant: both repos carry a "Merge when ready"
rule with **no base condition**, so labelling a stacked child `ready-to-merge` would
squash-merge it *into its parent branch* with no checks. **The app never applies that label,
and the page warns on any non-`main`-based row that carries it** (invariant 2).

### 4b · A launch is the unit of consent

The user selects a set of tasks and authorises it. That set — a `launches` row plus its
`launch_members` — is the **entire scope within which the tick may spawn anything**.

The preview shown before authorising lists what starts now, what starts on unlock and off
what base, and what is refused and why. It is where a wrong base or an unexpected chain
becomes a line the user reads rather than an afternoon of wasted agent work. **It also shows
the base's verdict** where the base is a blocker's branch — "you are about to build on a red
parent" is a line to read, not a rule to enforce.

**Consent is bound to content.** The preview composes each task's prompt to render it;
`hash(composed)` is stored on the member row at authorisation. At launch time (§3.10) the
tick recomposes and refuses to spawn on mismatch — the row reads `seam changed` instead. A
launch can sit `queued` for hours; without the hash, the user's yes would attach to whatever
the seam files and ticket say *later* (and the seam files sit on the same filesystem the
agents run on).

A launch is `active` until every member is terminal or the user cancels it. **Cancelling
stops the tick starting anything further from that slice and leaves running agents alone**;
the tick still pushes and reads CI for work already in flight — cancel withdraws consent for
*future* work, it does not orphan finished work. Killing is per-row and separate. A
cancelled member that never launched reads `cancelled` (verbs: `remove worktree` if one was
cut, `launch` to re-authorise).

`launches` and `launch_members` record decisions no amount of reading the world can
reconstruct — as does `runs` (§8); revision 3's "one table not re-derived" undercounted.

## 5 · State machine

Two independent facts decide where a task sits before it runs: is it **unlocked** (§4), and
is it **authorised** (§4b). Neither alone starts anything.

**Status is derived, not stored.** One pure function computes the label from the stored
facts — launch membership, latest run's `(kind, outcome)`, latest push, verdict, unlock
result, base kind — and the page, the tests, and `events` all call it. That is what keeps
the unlocked × authorised cross a real 2×2, lets `queued` say *why* (waiting on a base is
hours-or-forever; waiting on a slot is seconds — the page must not render them alike), and
gives orthogonal flags somewhere to live: **`seam changed` is a flag on a row, not a state**
(it composes with `queued`, `review me`, anything), as is draft-ness.

```
              unauthorised        authorised
   blocked  →   blocked      │      queued  (reason: base / slot)
   unlocked →   ready        │      queued
                  │                   │
             user launches      tick launches
                  └─────────┬─────────┘
                            ▼
        running ──► checking ──► review me ──► merged
           │            │
           │            ├──► base moved ──(refresh)──► checking
           │            │         └──(conflict)──► refresh conflicted
           │            ├──► waiting on producer deploy ──► checking
           │            └──► needs you
           ├──► failed
           ├──► cut failed
           └──► push failed

parent PR closed unmerged:  a member that has RUN  ──► base gone
                            a member never launched ──► blocked / queued (re-derived)
```

| State | Means | Verbs |
|---|---|---|
| `blocked` | a stacking blocker has no OPEN or MERGED PR; unauthorised | launch (authorises; starts on unlock) |
| `queued` | authorised, waiting — the row says for what: its base, or a slot under `max_agents` | cancel |
| `ready` | unlocked, unauthorised — the row a human acts on | **launch** |
| `running` | agent process alive | kill |
| `checking` | pushed, PR open, CI running or not yet reporting | re-run, close PR |
| `review me` | PR open, every gating check green (stacked base unmoved) | close PR |
| `base moved` | stacked base advanced past the recorded base SHA, or the parent merged (base now `main`) | **refresh**, re-run |
| `refresh conflicted` | `refresh`'s merge conflicted; worktree left mid-merge for a human | abort, (shell — path on the row) |
| `waiting on producer deploy` | every gating check green except the cross-repo compat one | re-check |
| `needs you` | a gating check red (other than the compat one), a refused push, or a refused fast-forward | re-run, kill, close PR |
| `failed` | dead run, no commits since its baseline | re-run |
| `cut failed` | `tp new` failed (hooks, disk, base unresolvable) | re-run |
| `push failed` | push or `gh pr create` failed | **retry push** (no agent), re-run |
| `base gone` | this row ran, and its parent's PR was closed unmerged — the work it built on is withdrawn | re-run, remove worktree |
| `pr closed unmerged` | someone closed this row's PR | re-run, remove |
| `cancelled` | authorised then cancelled before launching | remove worktree, launch |
| `merged` | PR merged | remove worktree |

**A row that has ever run never returns to `blocked`** (invariant 19) — `blocked` means
"nothing to cut from, unauthorised", and a row with a worktree, commits and possibly an open
PR is neither. Revision 3's §3.8 sent dependents of a closed PR there, silently dropping
their consent and their artifacts; they now read `base gone`.

`checking` exits to `needs you` if no rollup matching the pushed tip appears within a
bounded wait — **counted only over ticks whose observe phase succeeded** (a GitHub outage
must not walk every in-flight row to `needs you` at once), and clocked from `pushed_at`.

**A wedged agent is not a state the app can compute.** Liveness is a pid check by design; an
agent stuck inside a 40-minute tool call is indistinguishable from one working. The page
shows each running row's elapsed time and pgid, and `kill` is one click.

Verbs: `launch` · `re-run` (relaunch in the same worktree, incremental, handed the
composed-spec diff when `seam changed` is set) · `refresh` (§4a) · `retry push` (re-run the
push step alone — a push failure is auth or non-fast-forward, and relaunching an agent to
retry a network operation spends the wall for nothing) · `re-check` (**`gh run rerun <id>`**
on the compat check's run, id parsed from the rollup's `detailsUrl` — no gating workflow in
either repo declares `workflow_dispatch`, so revision 3's "re-dispatch the workflow" named a
mechanism that does not exist) · `close PR` (`gh pr close` — the app can open a PR; it needs
a sanctioned way to unopen one) · `cancel` · `kill` (`-pgid` SIGTERM → SIGKILL) ·
`remove worktree` (`tp remove --force`, only with MERGED PR state or on a `base gone`/
`cancelled` row the user clears). Worktree path, pgid, base and elapsed time render as plain
text so a terminal session is a copy-paste away.

The page shows **stack depth and merge order**, and — because a localhost page is invisible
to the reviewer who actually merges — the app also writes the order where the reviewer is:
**each stacked PR's body carries a "Merge after #N" line** (via `gh pr create --body`
augmentation of `--fill`). `gh pr create --base` makes an ordinary PR, not a GitHub Stack
(Stacks exist only via the `gh stack` CLI — treepad ADR 0003), so without this line a
reviewer sees a correct diff against a branch name they don't recognise and no ordering at
all.

## 6 · The seam mechanism

A **seam** is a named integration point between repos. It exists *only* because cross-repo
work has no shared git history to read. Single-repo features need none, and seam count is a
free complexity signal.

Seams live in `plain/.claude/seams/` — private, uncommitted, workspace-level. They must not
be committed to team repos: that would impose one person's orchestration on colleagues.

A seam is **a file, pasted whole into a prompt**. No symbol addressing, no marker comments,
no parsers. **It has exactly three jobs and nothing verifies it.**

1. **Prompt context.** Prompts are composed at authorisation and again at launch, never
   embedded: `/implement <ticket-url>` plus the current contents of every seam the ticket
   consumes. `hash(composed)` binds the launch to what was previewed (§4b) and identifies
   what a run ran against. Because nothing is copied, amending a seam simply stops every
   dependent's hash matching — there is no propagation step to build. The app stores each
   run's composed prompt so a re-run can be handed a mechanically computed before/after diff.
2. **Draft gate.** A consumer PR opens as a draft and is un-drafted only when its CI
   verdict is green and every seam it consumes has a merged producer. This uses GitHub's
   own affordance, so the constraint is visible to teammates who might otherwise merge it.
3. **Retirement pointer.** Each seam declares `lands_at` — one or more paths in the
   producing repo. Once the producer merges, later consumers are pasted the real file from
   `main`, so the private seam file is never read again and cannot go stale.

A producer-side assertion was available and was declined: `services` does hold a single
committed SDL file (`packages/core-graphql/src/schema.ts`, a ~16,100-line `gql` template
literal), so a seam could be a verbatim excerpt and a check could be a grep. It is not
built, because the seam mechanism's job here is coordination, not enforcement. Recorded as a
deferral in §12.

### What a cross-repo consumer actually experiences

This is the honest statement revision 1 got backwards, and it constrains the design more
than anything else in it.

- `support-app` has **no committed copy of the schema.** `schema.graphql` is untracked and
  absent from the working tree; `scripts/gen.sh` `rm -f`s and re-`curl`s it from a running
  server, and skips the whole GraphQL block when `CI` is set.
- `.github/workflows/graphql-prod-compat.yml` regenerates types from **production** and
  typechecks against them, and `.mergify.yml:20` makes it a **required** check.

So a consumer implementing an agreed-but-unlanded seam field fails a required check
**deterministically until the producer deploys** — not merges. There is nothing to patch
locally and no way to make it green early.

The app's answer is naming rather than mechanism. `waiting on producer deploy` is entered
only when the compat check is the *sole* red required check — which distinguishes "the seam
is not live yet" from "this consumer is broken" using check names `.mergify.yml` already
enumerates. The state exits on `re-check` (`gh run rerun` against the compat run) once the
producer's change is live, which regenerates types against the deployed schema.

**"Parked indefinitely" is not a state these repos permit.** Both auto-close any PR
untouched for 14 days unless it carries `keep-open` (`.mergify.yml`, both repos) — which is
why the app opens every PR with that label (§3.11). With the label, a stuck producer parks
its consumers until a human acts; the escalation is that the producer's own row shows
`needs you`. Without it, revision 3's "by design" was a robot's countdown.

Seam amendment is a user-invoked skill, not an automatic loop. Most PR comments are code
standards or implementation disagreements and do not touch the seam.

## 7 · What "done enough to push" means, and who can push at all

The app runs no tests. `/implement` already does — "run the full test suite once at the
end" (`~/.agents/skills/implement/SKILL.md:11`) — so local testing exists; the app simply
does not check the result. CI is the correctness gate, and the app's contribution is to read
it precisely (§8) rather than to duplicate it.

**Two boundaries, not one.** Revision 3 asserted the push denylist "blocks the only push
whose consequences are not confined to a branch" — false: the agents arrive with their own
pre-approved push. `support-app/.claude/settings.json` is *tracked* (in every worktree via
git) and allows `Bash(git:*)`, `Bash(gh pr:*)` — which includes `gh pr merge` — and
`.claude/settings.local.json` is in treepad's `[sync] include` and allows `Bash(node *)`.
The app "never merges" is worthless while an agent it launched can.

**Boundary 1 — the run.** Every spawn passes an **app-owned settings file** via
`claude -p --settings <path>` whose `permissions.deny` blocks `git push`, all of `gh`, and
network fetch tools. Deny rules take precedence over the repo's allows, so the tracked and
synced settings become harmless without breaking the human's own worktree workflow. The
app owns pushing; the agent has no legitimate need for either. (Invariant 17.)

**Boundary 2 — the push policy.** Per-repo, in config (§8) — the executing surface is
repo-shaped and a global three-entry list cannot cover it. The default set, applied to every
repo: `.github/**`, **`.mergify.yml`** (repo root — outside `.github/`, and it is the file
the app's own verdict predicate is transcribed from; an agent edit to it de-syncs invariants
11–12 silently and its `label=bypass` + body-checkbox rule skips all checks, with the body
half agent-writable via the app's own `--fill`), `CODEOWNERS`, `**/package.json` (six
tracked files match in `support-app`, two with postinstall hooks — root-equality misses
them), lockfiles, `pnpm-workspace.yaml` (carries `patchedDependencies` — code injected into
`node_modules` at install — and the registry the `.npmrc` token is sent to), `.npmrc`,
`.env*`. Per-repo additions in §8's example. `pnpm install` is the first step of every
required check, with a live secret in env — install-time execution is the shortest path from
an agent diff to a credential, which is why the config surface is listed, not just the
workflow directory.

**No diff touching a policy path is ever pushed.** That row goes `needs you` with the
offending paths listed, and the refusal lands in `events`.

**The predicate copy gets a staleness detector.** The app records `sha256(.mergify.yml)`
beside each repo's predicate; when the file's hash no longer matches, green verdicts are
suppressed to `checking` with reason `check config changed` until the user re-confirms the
predicate (updating the stored hash). Silent staleness — the failure the v2 review predicted
in both directions — becomes a visible row. (Same move as the base SHA.)

There is no verify command and no per-repo verify config. `quarantined` does not exist and
`to-tickets` needs no `files` field.

## 8 · Config, schema and on-disk layout

```
ai-development/
  treepad/                            one small change (§10)
  command-centre/
    cmd/cc/main.go                    flock → SQLite → ticker + HTTP
    internal/plan/                    unlock, eligibility, disposition, push plan — pure
    internal/verdict/                 predicate evaluation — pure
    internal/gh/                      exec + decode + normalise; owns gh's JSON shape
    internal/tp/                      the two treepad commands
    internal/cc/                      store, loop, runner, http — the imperative shell

plain/.claude/command-centre.toml     app config (user-edited; stays with the workspace)
plain/.claude/seams/                  seam files (user-edited)

~/Library/Application Support/command-centre/plain/     mode 0700
  command-centre.db                   SQLite; flock target
  runs/<run-id>.jsonl                 agent stdout, one file per run
```

The DB and run logs are **not** under `plain/` because the worktrees are its siblings: from
any worktree, `../.claude/command-centre.db` was a same-uid writable path to the consent
table, every composed prompt, and every other agent's tree. Moving two path constants now is
cheap; the seam files stay (they are user-edited inputs; the prompt-hash binding in §4b is
what protects consent from their mutation).

`internal/plan` and `internal/verdict` import no `os/exec`, no `database/sql`, no
`net/http` — guarded the way treepad guards `batch/`: an `api_test.go` that fails the build
on a forbidden import. In Go a package is the unit of dependency; a pure *file* in an impure
package stays pure only by convention.

```toml
max_agents = 4
port       = 7777

[[repo]]
name         = "support-app"
path         = "support-app"
stacking     = false                  # Phase 1; Phase 2 flips it
compat_check = "GraphQL production compatibility"
deny = [                              # on top of the default set (§7)
  "scripts/**", "codegen.yml", "next.config.js",
  "vite.config.ts", "eslint-ci.config.ts",
]
  [repo.checks]
  all_of = [
    { success = "Lint" },
    { success = "Typecheck" },
    { success = "Tests" },
    { success = "Generated files" },
    { success = "GraphQL production compatibility" },
    { any_of = [
        { success = "verify / Linear issue is linked" },
        { success = "verify-linear-issue / Linear issue is linked" },
        { success = "verify-linear-issue / Linear Issue Linked" },
    ]},
  ]

# services, for reference (Phase 2+): deny adds "terraform/**", "infra/**", "tools/**",
# "sst.config.ts", ".pnpmfile.mjs", "patches/**", "vitest/**", "vitest.config.js";
# its predicate needs the full grammar below — write it as a fixture in Phase 1.
```

**The predicate grammar** is `all_of` / `any_of` / **`not`** / `success` / `skipped` /
`absent_ok`. Verified against both files:

- `support-app`'s five names plus the Linear `any_of` are correct (re-verified).
- `services`' names are `Lint`, `Typecheck`, `Unit Test`, `Generated files up-to-date`,
  `Integration Tests Passed`, `Local Integration Tests Passed`; deployment is
  `("Deploy / Deploy SST Stage" | "Deploy / Deploy PR Stage" | ("Evaluate" && skipped "Deploy"))`;
  the Linear branch is a **four**-way `or` — its first arm is `author = dependabot[bot]`, a
  PR attribute, transcribed as a rollup-independent flag on the snapshot, which is why the
  grammar needed `not` and a non-check escape hatch rather than pretending the "check
  subtree" is purely checks.
- `Lint GitHub Actions / Lint` is path-filtered; Mergify's real clause is a **negation**
  (`-check-neutral AND -check-pending AND -check-failure`). `absent_ok` approximates it with
  defined semantics: **an absent check with `absent_ok` reads `pending` until the bounded
  wait elapses, then `passing`** — never green from a young snapshot, which would reopen
  per-check the hole invariant 11 closes at the rollup level.

**`verdict/` is pure over a normalised snapshot, not gh's JSON.** Its inputs, honestly
listed: the reduced check map (latest per name — §3.2), `headRefOid` vs the pushed tip, the
recorded base SHA vs `origin/<base_branch>`'s tip (stacked bases only, §4a), whether the
base is the default branch, `pushed_at` and now (the bounded wait), and the config-hash
match (§7). Every case above is a table test, and both repos' predicates are fixtures from
Phase 1 even though Phase 1 evaluates only one.

**There is no retry-pending rule.** Revision 3 said "a red check with a queued re-dispatch
is `checking`" — but the rollup exposes no attempt data and the retry workflow's runs attach
to `main` commits, so the rule had no input (the same asserted-without-a-data-source failure
as revisions 1–2). Red is red; `re-run` and `re-check` are the verbs; automating it lives in
§12 with its conditions. `services`' auto-retry means a red there may self-heal — accepted,
named in §13.

**Reading Mergify's own merge-protection check-run instead does not work** — re-verified
with live data: it reports FAILURE on PRs whose every gating check is green, because it
folds in `#approved-reviews-by>=1`, `-draft`, `-label=WIP` — each false exactly when the app
wants to say `review me`.

### Schema

Written down before code, because "task fact vs run fact vs push fact" is the split Phase 2
stresses (a `refresh` is a push with no run; a `re-run` is a second run against one PR):

```
tasks           ticket_url PK · repo · branch · blocked_by[] · seams[]
                (intake upserts on ticket_url — re-running to-tickets must not mint rows,
                 or invariant 8 loses its key and double-launches)
launches        id · created_at · state (active/done/cancelled)
launch_members  launch_id · task_id · prompt_hash        ← consent, bound to content (§4b)
runs            id · task_id · kind · pgid · proc_started_at · baseline_sha ·
                prompt_hash · log_path · outcome · exit_code · ended_at
pushes          id · task_id · pushed_tip · base_branch · base_sha_at_push · pushed_at
events          append-only: every transition, verb, refusal, tick error
intents         verbs from HTTP, consumed by the loop
meta            schema_version (refuse to start on mismatch)
```

Status is **derived** (§5); there is no status column to go stale. Truth is DB ∪ worktrees ∪
the PR snapshot, re-derived each tick. `launches`, `launch_members`, `runs` and `pushes` all
record history the world cannot reconstruct — revision 3's "one table not re-derived" was
refuted by its own crash-recovery paragraph.

**SQLite**: WAL, `busy_timeout=5000`, and **only the loop goroutine writes** — HTTP verbs
insert `intents` the next tick consumes. That closes the race revision 3 shipped (a `cancel`
landing between unlock and launch let the tick spawn from a launch the user had just
cancelled — the flock is per-process and both writers shared the process), makes every verb
idempotent, and is why the per-agent exit-collecting goroutine could be deleted (§3).

Agent stdout is **redirected to a file**, never piped. An app crash leaves the fleet alive;
a restart takes the flock, re-reads `runs`, re-checks each pid with `kill(-pgid, 0)` plus
the start-time match, and carries on mid-run. Spawn with `Setpgid`; cancel with a custom
`Cancel` signalling `-pgid` SIGTERM then SIGKILL, because `exec.CommandContext`'s default
signals one pid and `claude -p` spawns tool subprocesses that would otherwise orphan holding
the worktree.

The page shows **tick age and the last tick error** — Phase 1 requirements, not polish: they
are the only signal separating "quiet because nothing needs doing" from "the loop died an
hour ago", and a level-triggered design has no other way to say so.

## 9 · Invariants

Numbered so each can become a test.

1. The app pushes only branches it created, only when the local tip differs from the last
   pushed tip, and only when the diff against the branch's recorded base clears the repo's
   push policy. Push is a reconciliation (§3.8): a dead run's commits, a re-run's commits
   and a refresh's merge commit all ship through it. A refused push is `needs you` with the
   offending paths listed.
2. The app never merges a pull request, never applies `ready-to-merge`, and warns on any
   non-`main`-based row carrying that label (both repos will squash such a PR into its
   *parent*, checks unseen).
3. The app never deletes a worktree that is dirty or holds unpushed commits.
4. The app never performs a git write (merge, reset, branch move) in a worktree with a live
   run or an unresolved merge.
5. The app's treepad surface is `tp new --base` and `tp remove --force`; it never calls
   `tp batch sync`, `gh stack init/add/submit/modify/rebase/sync`, or any rebase; worktree
   paths come from `git worktree list --porcelain`, never from a reimplemented derivation.
6. Liveness is `kill(-pgid, 0)` plus a start-time identity check. No timing rule ever marks
   a run dead.
7. A dead run's disposition comes from artifacts — commits since **that run's** recorded
   baseline SHA — recorded as data (`kind`, `outcome`), never from the absence of events.
8. The tick spawns a process only for a task that is unlocked, belongs to an `active`
   launch, whose recomposed prompt hash matches the hash recorded at authorisation, and has
   no prior run. Every relaunch is a user verb.
9. One app instance per workspace (flock on a sibling lock file, not the DB file itself —
   locking the DB deadlocks modernc.org/sqlite under SQLITE_BUSY); within it, only the loop goroutine
   writes the DB — verbs are queued intents.
10. A tick whose `git fetch` or `gh` read exited non-zero applies no GitHub-derived
    transition and launches nothing; the failure is recorded and the page shows tick age.
    An empty PR list is data only from a zero-exit call. `checking`'s bounded wait advances
    only over ticks whose observe phase succeeded.
11. A CI verdict is computed only over the repo's configured predicate, from a normalised
    snapshot (latest completed run per check name) whose `headRefOid` matches the pushed
    tip, and — for a stacked base only — whose recorded base SHA matches
    `origin/<base_branch>`'s tip. An empty or absent rollup is never green; an absent
    `absent_ok` check is `pending` until the bounded wait elapses; a green verdict is
    suppressed while the repo's `.mergify.yml` hash no longer matches the one the predicate
    was written against.
12. `waiting on producer deploy` is entered only when the configured compat check is the
    sole red required check and every other required check is green.
13. A consumer pull request is un-drafted only when its CI verdict is green and every seam
    it consumes has a merged producer.
14. Draft state, CI verdict and unlock are re-derived every tick — facts are stored, labels
    are derived, nothing is latched.
15. Every worktree is cut with an explicit `--base origin/<base_branch>` resolved from a
    snapshot fetched (with `--prune`) on the same tick.
16. A task with one stacking blocker whose PR is OPEN launches with base = the blocker's
    branch when `stacking` is on, and base = `main` otherwise; a MERGED blocker always
    yields base = `main` (the branch is deleted on merge); a task with two or more stacking
    blockers launches only when every one is MERGED, base = `main`. Gating (cross-repo)
    edges never affect unlock or the base.
17. Every run is spawned with an app-owned `--settings` file denying `git push`, all of
    `gh`, and network fetch tools; no run inherits `ANTHROPIC_API_KEY`; no run uses
    `--bare`.
18. The HTTP surface binds `127.0.0.1`; every mutating verb is POST-only; a request whose
    `Origin` is absent or mismatched, or whose `Host` is mismatched, is rejected.
19. A task that has ever run never returns to `blocked`; a withdrawn base is `base gone`.
20. Intent is recorded before every outward effect, and effects are reconciled from the
    world: an existing open PR for a branch is adopted, never duplicated; worktree existence
    is read from git, never inferred from run rows.

## 10 · Required upstream and workspace changes

**treepad — one, small.** Expose `tp remove --force`. The plumbing exists —
`lifecycle.go:242-247` builds `git worktree remove --force` and `git branch -D` behind an
internal `force` bool, and the TUI already passes it — but `internal/commands/remove.go`
registers no flags. ~5 lines. Needed because `merge_method: squash` means `git branch -d`
refuses *every* merged ticket. The app passes `--force` only when it holds MERGED PR state
for the branch, or when the user clears a `base gone`/`cancelled` row.

**Workspace config (plain, private gitignored files) — two one-liners, take them now:**

- Delete `.env` from both repos' `.treepad.toml` `[sync] include` lists. A no-op today
  (neither repo has a root `.env`) and a latent secret-copy tomorrow. Note the file that
  *is* both secret-bearing and synced is `support-app`'s `.env.development.local` — it stays
  synced (worktrees need the local override to boot) and is covered by boundary 1 (§7): the
  agent's settings deny the exfiltration paths, and `.env*` is in every push policy.
- No change needed for `.claude/settings.local.json` in the sync list: invariant 17's deny
  file takes precedence over its allows.

**No treepad Activity-file change is required — the app no longer touches the file at
all** (§16). Revision 3 kept writing it as a double-launch guard; verified: every treepad
reader — the launch keys, the run-state column, the log pager — is gated on **Batch/manifest
membership**, and this design writes no manifests, so the file the app wrote was read by
nobody, while forcing the app to reimplement `launcher.ActivityPath` + `internal/slug`
across a repo boundary. The app's double-launch guard is invariant 8 (its own DB) plus
invariant 9 (the flock). If the app ever starts writing manifests, that is the trigger to
revisit — and the cheap upstream form is a three-line exported `batch.ActivityPath`.

**Skills** — contracts specified here, written separately:

| Skill | Change |
|---|---|
| `to-plan` | Recommend and prompt `to-seams` when a plan touches >1 repo; do not gate. (Revision 3 also tasked repointing a `design-an-interface` reference — verified: no such reference exists in the installed skill.) |
| `to-seams` | **New.** Writes `plain/.claude/seams/<name>`; declares producer repo, consumer repos, and `lands_at`. Halts for approval. |
| `to-tickets` | Emit per ticket: `repo`, `blocked_by[]`, `seams[]` into the app's task table, **upserting on `ticket_url`**. **Delete** its manifest-emission step and `references/treepad-manifest.md` — nothing reads manifests any more, and the chain rules that file carries (including the fan-in rule the v2 review flagged) are the machinery §4 replaced with edges. |

## 11 · Verified external behaviour this design relies on

Re-verified 2026-08-22 (revision 4 pass — against source, config, live PRs and the GitHub
API, not citations inherited from earlier revisions).

**treepad**

- `tp new` accepts `--base`, "Ref to branch the new worktree from" (`docs/commands.md:74`);
  the value is passed verbatim to `git worktree add --no-checkout -b <branch> <path> <base>`
  (`lifecycle/lifecycle.go:69`) — no validation, no fetch, and **a bare parent-branch name
  resolves only if a local branch exists**, hence `origin/<base_branch>` always (§4).
- `tp remove` takes no flags; the force plumbing exists unexposed (§10).
- treepad never fetches before cutting; its only `git fetch` is
  `internal/treepad/restack.go:87` (a member's own branch). Hence observe step 1.
- `RestackDecision` (`batch/restack.go:26`) compares a branch against its own upstream,
  never its base — blind to the moved-base case, which is why the app compares base SHAs
  itself. `restack`'s *other* job — fast-forwarding a branch to its own upstream — is the
  precondition `refresh` and `re-run` now carry explicitly (§4a), because deleting the
  mechanism did not delete the need.
- `MergedBranches` is ancestry-based (`for-each-ref --merged`) — useless post-squash, hence
  PR-state evidence for teardown. `tp prune` iterates every worktree unfiltered; unused.
- Every Activity-file reader is Batch-gated (`tui_batch.go:26-28,136-176`,
  `tui_update.go:369-371`); with no manifests, `tp ui`'s 60-second `Reconcile` cannot see or
  act on app worktrees.
- `[sync]` supports `!` exclusions (`internal/sync/sync.go:246-256`) — revision 2/3's "no
  denylist exists" was false twice over.

**GitHub / gh / Mergify**

- `gh pr list` defaults to `--state open --limit 30`; `statusCheckRollup` is valid on the
  list command; rollup entries carry no per-entry oid and no attempt data; `headRefOid`,
  `baseRefOid`, `isDraft` are selectable top-level fields. One check name can appear many
  times per head SHA with conflicting conclusions (observed ×5, mixed CANCELLED/SUCCESS).
- `gh pr create --base` takes a **branch name**; `--fill` fills title/body from commits;
  `gh pr edit --base` exists (the retarget verb).
- Both repos, server-side (API): `delete_branch_on_merge: true`;
  `allow_merge_commit: false`; `allow_rebase_merge: false` (squash is enforced, not just
  configured).
- Both `.mergify.yml`s: `base=main` is the first `common_checks` condition (stacked PRs
  never enter the queue); `update_method: merge` (the queue merges, it does not rebase);
  `batch_size: 4`; a no-base-condition "Merge when ready" rule on `label=ready-to-merge`;
  a 14-day stale-close rule with a `keep-open` escape; `merge_protections` reported via
  check-runs and folding in approvals/draft/WIP (confirmed FAILURE on live all-green PRs).
- `services` auto-reruns failed itest shards while `run_attempt < 4`
  (`itest-retry.yml:20-31`); those reruns are invisible in PR rollups.
- Re-parenting fires `pull_request: edited`; the only workflows listening are the
  `linear-issue` ones (a required check) — no *test* workflow re-runs on retarget, which is
  why a stacked verdict must expire on base movement. A `refresh` push fires `synchronize`
  and re-runs everything.
- `/implement` runs the suite and `/code-review`; `git push`/`gh pr create` appear nowhere
  in implement, code-review or tdd — the app genuinely owns pushing.

## 12 · Deferred, with re-entry triggers

| Deferred | Re-enter when |
|---|---|
| Local verify (a real pre-push test gate) | work reaches `review me` that `/implement`'s own suite should have caught. Disposition-as-data (inv. 7) makes it a new run kind, not a loop edit |
| Producer-side seam assertion (grep the seam against `lands_at`) | producer drift wastes consumer work more than once |
| CI retry with the failing log | you click `re-run` on CI-red rows often enough to want it automated — then only on a *terminal* failure, capped log as a file, never auto-pushing a denylisted diff |
| Detecting producer deploys automatically | `waiting on producer deploy` rows sit unnoticed |
| Automating the retry-pending distinction (`services` itest) | red-that-self-heals parks rows often enough to annoy; mechanism is `gh run view <id> --json attempt,status` per red row — a priced second call, not a free field |
| Scope containment / `quarantined` | you reject PRs for drive-by edits often enough to notice |
| Automatic `refresh` on a `base moved` row | you click it every time. Gate on no live run (inv. 4 already forbids the dangerous case) |
| Poll/act split — at most one PR read in flight per repo, loop reads the last snapshot + age | tick duration becomes visible (observe measured ~6s/repo today; Phase 1 is one repo) |
| `gh stack link` for reviewer-visible stack structure | reviewers ask for more than the "Merge after #N" body line. The v2 defect was treepad's unscoped *caller*, not the mechanism — the app would be scoped by construction |
| Generating the predicate from `.mergify.yml` (YAML-alias parser) | the hash detector (§7) fires more than twice; the boolean grammar is already the parse target |
| Adaptive rate-limit governor | you hit rate limits often enough to notice. Until then `max_agents` is the knob |
| `POST /tasks` | you want to add a ticket without re-running `to-tickets` |
| Slack / OTel / Datadog egress | a localhost page is not enough. Tick-age and last-error are already Phase 1 page fields |
| MCP permission-prompt loop | denied permissions become a common failure mode |
| TUI | the HTTP page is demonstrably the wrong shape |
| Chain depth cap or warning | rebase churn from deep chains shows up in practice (a ~4-deep flag already ships in `to-tickets`' guidance; the page shows depth as information) |
| Parent decision-trace into descendant prompts | descendants re-litigate decisions their parent already made |
| Retention on run logs / worktree disk | disk pressure is noticed. Worktrees measure ~2–4 GB each under `[sync]`; prune run logs on `remove worktree` |
| Multi-machine | never — different product, different auth |

## 13 · Known limits accepted

1. **Nothing anywhere validates a consumer against a seam.** Cross-repo consumers are red
   until the producer deploys, and the app's only response is to name the state correctly.
2. **The app cannot tell a wedged agent from a working one** (§5). The page shows elapsed
   time; the judgement is the user's.
3. **`.env.development.local` is synced into every `support-app` worktree.** It is the local
   override worktrees need to boot, so it stays; the mitigations are invariant 17 (the
   agent's deny settings) and the `.env*` push policy. The no-op `.env` sync lines are
   deleted (§10). `support-app/.npmrc` is tracked and holds a registry token — a repo fact
   the app cannot fix; rotating it to the `${TIPTAP_PRO_TOKEN}` interpolation CI already
   uses is recorded here as the team-side fix.
4. **Stacking on open PRs means a red parent's descendants do not have its fix.** The app
   detects it (`base moved`), names it, shows the parent's verdict on the row and in the
   launch preview, and offers `refresh`. Deliberate throughput trade — the reason the design
   exists. Nothing *forbids* launching on a red parent; the preview makes it a line you read.
5. **A red on `services` may self-heal** (auto-retried itest shards) and the app cannot see
   the retry (§8) — so `needs you` there can fire on a red that clears in minutes. Accepted:
   the row has a verb, and automating the distinction is priced in §12.
6. **Squash-merge is server-enforced**, which is what makes merge-don't-rebase free (§4a).
7. **`tp prune` is unusable for this workflow** (unfiltered, ancestry-based); the app uses
   `tp remove --force` with PR-state evidence instead.
8. **Merge order lives in the PR body, not GitHub's stack UI** — `gh pr create --base`
   makes ordinary PRs, and real Stacks exist only via the `gh stack` CLI, which stays out
   (§12).
9. **Mergify's queue never sees a stacked PR** (`base=main` gates it), so merge-time
   integration happens only after the app's retarget step. The remaining untested half —
   auto-retarget timing on branch deletion, and queue behaviour on a just-retargeted child —
   is a **pre-Phase-2 spike**: one hand-made two-PR stack, merge the parent, watch
   `baseRefName`, then queue the child. Hours, no code. A second Phase-1-relevant spike:
   confirm a `--fill`'d PR from an agent branch passes the required Linear check — if not,
   every Phase 1 PR parks in `needs you` and the branch-naming convention needs settling
   first.
10. **Ticket precision is inside the correctness surface** and nothing checks it.
11. **Latency and shared CI capacity are the wall, not GitHub API quota.** The PR snapshot
    costs ~1 GraphQL point per call (~14% of quota at three repos); what actually bursts is
    tick duration (§3) and `services`' PR-stage pool — a team-shared resource `max_agents`
    should be chosen against, since one parent fix can trigger four descendants' full check
    sets.
12. **A second workspace doubles concurrency silently** — `max_agents` and the flock are
    per-workspace; the subscription limit is per-account.

## 14 · Corrected from revision 1

Four premises in revision 1 were false. Each is stated here because the conclusions drawn
from them were reasonable *given* the premise, and a future reader should not re-derive
them.

| Revision 1 claimed | Actually | What it had been used to justify |
|---|---|---|
| `.treepad.toml` is committed in both repos | Globally ignored (`~/.gitignore-plain:1`); untracked in both. It appears in `[sync] include` *because* git will not carry it | Rejecting a `[verify]` key in treepad config; part of the case for no local gate |
| `services` has no single schema file — the schema is code-first across ~100 module dirs | One committed file, `packages/core-graphql/src/schema.ts`, a ~16,100-line `gql` template literal | Relaxing "a seam is a file, verbatim"; deleting the producer-side `lands_at` assertion |
| A `support-app` consumer can patch its own committed schema copy and go green pre-merge | `schema.graphql` is untracked *and absent*; `gen.sh` deletes and re-curls it; `graphql-prod-compat` is a required check that regenerates from **production** | The entire consumer mitigation, and invariant 13's "merged producer" formulation |
| The app runs no tests, therefore no tests run locally | `/implement` runs the full suite at the end (`SKILL.md:11`) | Framing "CI is the only gate" as removing local testing rather than leaving it unchecked |

Design changes beyond those corrections:

| Revision 1 | Now | Why |
|---|---|---|
| `tp batch sync --json` is safe to call every tick | Requires the treepad Activity-file veto first | `link` and `restack` run unconditionally with no liveness input; `RestackFastForward` fires on the clean worktree of a thinking agent, and `link` pushes what it is given |
| Descendant gate: parent has an OPEN PR | OPEN **or** MERGED | a parent merging before its descendant got a slot stranded it permanently in `waiting`, on the happy path |
| CI red → re-run the agent with the failing log | CI red → `needs you` | the retry races the repo's own check retries, cancels the run it reads by pushing, spends shared CI capacity, and can present a worse branch as `review me` |
| Any dead run with commits is pushed | plus a `.github/`/lockfile/`package.json` denylist | a push drives dozens of jobs and an AWS deploy; an agent editing CI config edits its own judge |
| "commits present" | commits since the run's baseline SHA | attempt 1's commits made attempt 2 look successful |
| CI green = the rollup is green | rollup `head_oid` must match the pushed tip, over configured required checks | an empty or stale rollup read as green and un-drafted untested work |
| Nine states | thirteen, adding `stack-stale`, `pr closed unmerged`, `push failed`, `waiting on producer deploy` | each was a reachable condition with no state, no exit and no verb |
| `needs you` covers "a wedged agent" | it does not; the page shows elapsed time and `kill` | pid-only liveness cannot compute wedged, so the state named something undecidable |
| Invariant: never deletes a worktree | never deletes a worktree **that has commits or is dirty** | recreating an unlaunched fan-in root off fresh `main` is the fix for the stale-base bug, and the invariant was protecting work that is not there |
| Invariant: `.env` through a denylist | dropped; recorded as an accepted limit | treepad's `[sync] include` is an allowlist the app does not own |
| Fan-in needs no treepad change | still no schema change, but needs the recreate step | position 0 is materialised unconditionally on tick 1 and never advanced |

Unchanged and re-affirmed under review: the layer split, the single level-triggered loop,
pid liveness with no timing rule, artifact-first classification, file-redirected agent
output, `tasks.status` as truth with `events` as audit, never merging, stacking retained,
and seams as prompt context only.

## 15 · Changed in revision 3

Revision 2 was reviewed in `command-centre-v2-buildability-review.md`, which found six
mechanisms that do not work as written. Revision 3 fixes two and **dissolves four** by
changing two decisions that sit above them.

### The two decisions

**1 · The tick's authority is scoped to an authorised launch.** Revision 2's §3.10 topped up
agents into any ready worktree. Revision 3 has the user authorise a *slice* — a `launches`
row — and the tick may spawn only inside it (§4b). The app gained real autonomy where it
matters (a slice drains without you) and lost it entirely where it does not (it never
chooses work).

**2 · Stacks are never repaired.** Revision 2 kept stacking but planned to detect and repair
moved bases. Revision 3 keeps stacking, drops repair, and merges the parent in when it moves
(§4a). This is what removes `tp batch sync`, `gh stack link` and `restack` from the design,
and with them most of revision 2's remaining machinery.

Also considered and **rejected**: cutting stacking altogether and unlocking on merge. It is
simpler, but throughput would then be capped at reviewer latency and the app would be an
elaborate way to run one agent at a time. Producing a backlog of reviewable PRs without
being blocked between them *is* the product.

### What that did to the six must-fixes

| # | Review's finding | Revision 3 |
|---|---|---|
| 1 | CI verdict: config cannot express the predicate, two `services` names wrong, verdict never expires | **Fixed.** Boolean predicate in config (§8), names re-verified, and a recorded base SHA expires the verdict into `base moved` (§3.6) |
| 2 | Activity file has two owners; the app deletes it and re-arms the double-launch | **Fixed, and halved.** Existence-only, never deleted over work (inv. 6). The upstream `link`/`restack` veto is no longer needed |
| 3 | The stale-root fix is a no-op — `main` should be `origin/main` | **Dissolved.** Every cut passes an explicit `--base` resolved from a list fetched the same tick (inv. 15); there is no remove-and-recreate step left to be wrong |
| 4 | `tp` calls are unscoped: unowned branches get pushed, teardown loops | **Dissolved.** No `tp batch sync`, so no manifests, nothing to scope, and no teardown loop |
| 5 | The `to-tickets` chain rule reverses a shipped skill and misses fan-out | **Dissolved.** The app derives bases from `blocked_by` edges (§4); fan-out is N tasks sharing a blocker with no chain between them, so there is no chain contract to get wrong |
| 6 | `waiting on producer deploy` has no working verb | **Fixed.** The verb is `re-check` (workflow re-dispatch, no agent), and infrastructure-caused transitions charge no attempt |

*(Revision 4 note: rows 2 and 6 of this table were themselves found wrong — see §16.)*

### Other changes from revision 2

| Revision 2 | Revision 3 | Why |
|---|---|---|
| `tp batch sync --json` every tick | `tp new --base` at launch, `tp remove --force` at teardown | stacking needs two `--base` flags, not batch machinery |
| Readiness (§4) | Unlock (§4) — a pure function returning `{unlocked, base_ref}` | the base *is* the interesting output |
| `waiting` | `blocked` / `queued` / `ready` | unlocked-vs-authorised are independent facts |
| `stack-stale`, awaiting a hand-rebase | `base moved`, verb `refresh` | squash-merge flattens the merge commit, so the cheap repair is free |
| At most two attempts per task | The tick launches each authorised task exactly once; every relaunch is a user verb | with a cap there was a counter to get wrong |
| `required_checks = []string` | a boolean predicate | `.mergify.yml` has `or` branches, `check-skipped`, and a path-filtered check that passes when absent |
| Intake is manifests + task table | task table only | nothing reads manifests any more |
| Mergify queue × stacked PRs "unexamined" | the design's largest untested assumption | revision 3 delegates all merge-time integration to it |

## 16 · Changed in revision 4

Revision 3 was reviewed in
`docs/architecture-reviews/command-centre-v3-phase1-foundations-review.md` — seven dimension
reviews plus a dedicated citation-verification pass against treepad source, both plain
repos, live PRs and the GitHub API. Verdict: **BUILD AFTER FIXING 9**, and the
revision-1/2 failure pattern had recurred: of 24 load-bearing claims checked, **five were
false and three of those sat directly under a deletion or a dissolved must-fix**. Revision 4
is the response. The full false-claims table lives in the review; the ones that changed the
design are recorded here so no future reader re-derives a conclusion from them.

### False premises corrected

| Revision 3 claimed | Actually (verified) | What changed |
|---|---|---|
| `gh pr list --json …statusCheckRollup` supplies PR state, CI state and the unlock input | Defaults are `--state open --limit 30`: MERGED invisible (killing inv. 16's OR-MERGED arm, `merged`, `pr closed unmerged`, teardown evidence), and the open list truncates below `support-app`'s open-PR count today. `headRefOid`/`baseRefOid`/`isDraft` were also unselected, leaving inv. 11 with no input | §3.2: bulk-open + per-tracked-branch reads, full field list, `internal/gh` normalisation (the rollup is a multiset of a union type — one name observed ×5 at one head SHA with mixed conclusions) |
| Mergify's merge queue "rebases onto current `main`" | `update_method: merge`, both repos. And `base=main` heads `common_checks`, so **the queue never sees a stacked PR**; `delete_branch_on_merge: true` means a merged parent's branch is gone | §1, §4a: the app owns the retarget (`gh pr edit --base main` on parent merge); `refresh` gains its base-gone cause; the queue spike is scoped to what is genuinely unknown (§13.9) |
| Inv. 16: a MERGED blocker's base is "the blocker's branch" | That branch is deleted on merge — the happy path produced an uncuttable base | §4/inv. 16: MERGED → base `main`; fetch gains `--prune` |
| Verdict expiry: recorded base SHA vs "the branch's current base", unconditional | `origin/main` moved 130–187×/fortnight — `base moved` would have been every root row's steady state, while the PRD simultaneously claimed the state unreachable in Phase 1 | §4a/inv. 11: expiry exists only for stacked bases. The PRD's claim is now true |
| `refresh` = one command | It had no push path (inv. 1 pushed only dead runs with commits), no conflict state, no ff-precondition (Mergify and reviewers push to the app's branches; `restack`'s reconciliation was deleted with nothing replacing it), and its target ref vanishes when the parent merges | §4a: refresh is a four-step contract; §3.8/inv. 1: push restated as reconciliation; `refresh conflicted` state; inv. 4 |
| "A red check with a queued re-dispatch is `checking`" | The rollup exposes no attempt data and the retry workflow's runs attach to `main` commits — the rule had no input (the same asserted-without-a-data-source failure as revisions 1–2) | Deleted. Red is red; accepted limit §13.5; automation priced in §12 |
| Must-fix 6 "Fixed — `re-check` re-dispatches the workflow" | No gating workflow declares `workflow_dispatch` | `re-check` = `gh run rerun <id>`, id from the rollup's `detailsUrl` (§5) |
| Must-fix 2 "Fixed, and halved" — the app writes/deletes the Activity file, existence-only | Every treepad reader is manifest-gated, and revision 3 writes no manifests: **nothing read the file**, while the app had to hand-mirror `launcher.ActivityPath` + `internal/slug` across a repo boundary — and §8's restart sentence contradicted inv. 6 inside the same document | **Dissolved, revision 3's own move finished**: the file is deleted from the design; the guard is inv. 8 + inv. 9; worktree paths come from `git worktree list --porcelain` (inv. 5) |
| "A stuck producer leaves its consumers parked indefinitely, by design" | Both repos auto-close PRs at 14 days unless labelled `keep-open` | Every PR is created `--label keep-open` (§3.11, §6) |
| The push denylist "blocks the only push whose consequences are not confined to a branch" | Agents inherit pre-approved `Bash(git:*)`/`Bash(gh pr:*)` (incl. `gh pr merge`)/`Bash(node *)` from tracked and synced `.claude` settings | §7 boundary 1: app-owned `--settings` deny file on every spawn (inv. 17). The push policy is defence-in-depth, widened and per-repo, `.mergify.yml` included |
| "`launches` is the one table not re-derived" | `runs` holds ≥7 non-derivable columns per the same section's restart story; push facts belonged to neither | §8: the schema, enumerated — `pushes` as its own entity (a refresh is a push with no run; a re-run is a second run against one PR), `launch_members` with prompt hashes |
| §13.3's `.env` paragraph (and "no denylist to apply", surviving from rev 2) | `[sync]` supports `!` exclusions; bare `.env` exists in neither repo; the synced secret-bearing file is `.env.development.local` — the one rev 3 said was *not* synced | §10/§13.3 rewritten to the verified facts; the no-op sync lines deleted |
| §4 vs the PRD: one blocker → base = blocker's branch, while Phase 1's second ticket "cuts off `origin/main`" | The same input demanded two different bases — Phase 1 would have implemented a different unlock than §4 | The per-repo `stacking` flag (§4, §8): identical timing, `main` bases when off. Phase 1 runs it off; Phase 2 flips it — same function, same tests |

### Structural changes beyond the corrections

- **The tick is observe → decide → act** (§3), with one fail-closed rule: a failed read ends
  the tick before anything changes (inv. 10). This is a loop-shape property the v2 review
  asked for and revision 3 dropped; it cannot be retrofitted as error handling.
- **Pure decisions got a package, not a file**: `internal/plan` (unlock, eligibility,
  disposition, push plan) and `internal/verdict`, both import-guarded; `internal/gh` as the
  anti-corruption layer over gh's JSON (§8). Revision 3 purified the easy two and left the
  dangerous decisions — launch eligibility, disposition, the denylist — inline in the loop.
- **Status is derived, verbs are intents, only the loop writes** (§5, §8) — closing the
  cancel-vs-launch race and the crash-mid-tick duplicate-PR/duplicate-cut windows
  (inv. 20's adopt-don't-create rule).
- **New states** `refresh conflicted`, `base gone`, `cut failed`, `cancelled`; `retry push`
  and `close PR` verbs; `kill` removed from `checking` (the agent there is dead by
  construction); `seam changed` demoted to a flag; a run never returns to `blocked`
  (inv. 19). One normative state list — this section's (the PRD defers).
- **The DB and run logs moved out of the worktrees' sibling directory** (§8) — the consent
  table was a `../` away from every agent.
- **Consent bound to content**: `prompt_hash` recorded per member at authorisation, checked
  at spawn (§4b, inv. 8).
- **Merge order published in the PR body** ("Merge after #N"), not only on localhost (§5).
- **Tick age and last tick error are Phase 1 page requirements** (§8), not nice-to-haves.
- **Pre-Phase-2 spikes named** (§13.9): the two-PR stack (retarget timing + queue on a
  retargeted child) and the `--fill`-vs-Linear-check test, the second of which gates
  Phase 1's own exit criterion.

Verified *stronger* than revision 3 claimed, kept with better evidence: squash is
server-enforced, not just configured; repeated `refresh` does not pollute a descendant PR's
review diff; rejecting Mergify's merge-protection check-run as the verdict is confirmed on
live PRs (FAILURE while every gating check was green); the app genuinely owns pushing.

**Do not add.** Every fix above is smaller than the mechanism it corrects — five are
deletions or single flags. The one temptation this revision resisted on the reader's behalf:
owning the retarget is one `gh pr edit` call, not a return of `restack`.
