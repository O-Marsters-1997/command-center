# Where cloud agents fit

**Date:** 2026-09-14 · **Revision 1** · **Status:** sketch, not designed

Reads with `docs/designs/command-centre-design.md` §1–§4, which stays normative wherever the two
disagree. This document is normative about nothing. It is a shape to argue with.

Language is `CONTEXT.md`'s. Decisions, if any of this survives, become ADRs.

---

## 1 · What changes, and what does not

Three things get added to the app as it stands. An **always-on host**, so the tick survives a
closed laptop. A **second kind of run**, dispatched to a hosted provider instead of a local
worktree. And a **note attached to a slice**, so six agents working the same feature share what
the first one learned.

Nothing is removed. The DAG, the unlock rule, the authorisation gate, the board, the peer
conflict gate and the verdicts are all untouched, because none of them care who typed the
commits. Observe reads `origin/<branch>` and `gh`, not worktrees — only `obs.Worktrees` and
`obs.MidMerge` are worktree-derived — which is why the app tolerates work arriving from somewhere
it did not spawn.

The split that matters is not local against cloud. It is **who owns the working copy while
commits exist but are not yet pushed**. That window is the only place the app can refuse
something, and it is the whole cost of the change.

---

## 2 · The system

```
                       scope with Claude
                  to-plan → to-seams → to-tickets
                               │  tickets, blocked_by[]
                               ▼
                     USER authorises a slice        ← prompt hash binds consent
                               │
                               ▼
  ┌────────────────────────────────────────────────────────────┐
  │  THE TICK · every 15s · always-on host                     │
  │                                                            │
  │    observe ─────────▶ decide ─────────▶ act                │◀──┐
  │    fetch · gh         pure             cut · dispatch      │   │
  │    liveness           table-tested     push                │   │
  │                                                            │   │
  │    SQLite · one checkout per repo · the board              │   │
  └────────────────────────────────────────────────────────────┘   │
                               │                                   │
               ┌───────────────┴───────────────┐                   │
    cut+spawn  │                               │  dispatch         │
               ▼                               ▼                   │
      ┌──────────────────┐            ┌──────────────────┐         │
      │ LOCAL RUN        │            │ CLOUD RUN        │         │
      │ worktree         │            │ provider VM      │         │
      │ claude -p        │            │ shared context   │         │
      │ deny gate BEFORE │            │ agent pushes its │         │
      │ the push         │            │ own branch; gate │         │
      │ refresh · verify │            │ can only flag    │         │
      └──────────────────┘            └──────────────────┘         │
               │  push, gated                 │  already pushed    │
               └───────────────┬──────────────┘                    │
                               ▼                                   │
                  origin/<branch> → draft PR ──────────────────────┘
                  CI decides sound              PR OPEN unlocks dependents
                               │
                               ▼
                     USER reviews and merges
                     children retarget to main
```

The two run kinds diverge at **act** and converge the moment a branch reaches origin. Everything
below that line is identical for both, which is why adding the cloud kind touches the runner and
almost nothing else. The return path is the rule the whole system exists for.

---

## 3 · The rule everything hangs off

A ticket unlocks when its blocker's pull request is **open**, not when it is merged.

That one choice is why the app exists. The reason people write six-hundred-line pull requests is
not laziness, it is that splitting into six means paying six review latencies, because each one
waits for the last to merge. Unlocking on open deletes that cost. Six pull requests now cost one
review latency instead of six.

> The app is not a way to run more agents. It is a way to make small pull requests cheap, and the
> parallelism is only what that looks like from outside.

Two things follow, and they are the reason the rest of the design is shaped the way it is.
Children have to be retargeted to `main` when a parent merges, because a squash-merged branch is
not an ancestor of anything (§4a). And branches cut from the same commit will conflict with each
other, which is what the peer gate is for (ADR 10). Both are the bill for the rule, not features
sitting beside it.

---

## 4 · Who owns what

The layers have not changed. What has changed is that the bottom one is now rentable, and the
middle one needs somewhere to live that is not a laptop.

| Layer | Owns | Never |
|---|---|---|
| **User, with Claude** | What the work is. Which repos, which edges, which slice to authorise. Review and merge | Executes anything |
| **Control plane** | What starts, when, and off what base. Liveness, push policy, draft state, CI reading, retargeting, one page | Chooses work, merges, runs tests |
| **Runner** | Turning one ticket into commits. Local process or hosted provider, interchangeable | Knows another ticket exists |
| **GitHub and CI** | Whether the work is sound, and the only honest record of what is merged | — |

The runner row is the one every vendor is selling and the control plane row is the one nobody is.
That asymmetry is the whole argument for keeping the app: rent the layer that is commoditising
monthly, keep the layer that encodes your opinions about review.

---

## 5 · Two kinds of run

Chosen **per slice, at authorisation**, on the screen where you already stop and read. Not per
repo and not per ticket. `runs.kind` is free TEXT with no CHECK constraint, and §3 step 7 already
says disposition is a `(kind, outcome)` pair so that a new kind is a row rather than an edit.

**Local run.** For work that touches deny-listed paths, needs `verify_command` before you trust
it, or that you expect to re-run with a tweaked prompt.

- Commits exist privately before you decide
- The deny gate can actually refuse a push
- Refresh, restack and conflict repair are available
- Full run log, run phases, live spend
- Needs a machine that is awake

**Cloud run.** For small, independent, well-scoped tickets. Overnight batches. Anything you would
be happy reviewing cold.

- Runs whether or not anything of yours is on
- Context shared across the slice rather than composed per ticket
- No local CPU, so a concurrency cap separate from `max_agents`
- The deny gate can only flag a bad push, not stop it
- Thin detail panel; links out to the provider

**Repairs are always local**, whatever kind produced the branch. When a cloud row needs a working
tree, the app cuts one then, from `origin/<branch>` rather than from main, and spawns a local
agent into it. `cutAndSpawn` is already two steps welded together; unwelding them is the change.
The worktree stops being a precondition of every run and becomes a tool the app reaches for.

That falls out cleanly because run kind is per run, not per ticket, and only the latest run
renders. A cloud implementation followed by a local repair is two rows, and the second one wins.

---

## 6 · How a week goes

The point of the arrangement is that you are in it twice, at either end.

**Evening — scope, then authorise.** Plan the feature with Claude as now: `to-plan`, `to-seams` if
it crosses repos, `to-tickets`. Out comes a set of tickets that know their blockers. Select a
slice, read the preview, add the one-paragraph note every agent in it will see, pick local or
cloud, authorise. That is the last decision until morning.

**Overnight — the tick works the graph.** Unblocked tickets start immediately. The rest start the
moment their blocker's pull request opens, which is minutes after that agent finishes rather than
hours after you reviewed it. Branches that would conflict with an open peer are held rather than
run, so the same conflict is never resolved twice against a moving main.

**Morning — read the board, not the runs.** Every row carries one state and a sentence saying why.
A filled disc means the loop will move it without you; a ring means it is parked until you act,
and those are the only rows worth opening. A red group is usually one problem, not four: the
blocker failed and three rows are queued behind it naming it as the reason.

**Then — review bottom-up, merge, walk away.** A stack presents as an ordered queue starting at
its base, each diff scoped against its parent. Comments on the base propagate down mechanically
when you amend it: `restackBoundary` works out what the child already carries and `advanceOnto`
either merges or restacks, with the push step lease-forcing only in the restack case.

What does not propagate is the lesson. If the review changes a pattern, the later tickets wrote
their own copy of the old one, and re-running them with an amended prompt is yours to do.

---

## 7 · What stays yours

Hosted coordinators exist now and will get better. Cursor shipped Projects on 2026-09-10:
persistent context files that sync across every machine its agents use, subagents in isolated
VMs, and triggers on schedules, Slack threads and PR events. The gap narrowed. These three did
not, because each is an opinion rather than a capability.

**The state is derived, not narrated.** Every row's state comes from `plan.Status`, a pure
function over a snapshot, with table tests and a sentence naming the blocker. A coordinator
agent's answer to "why is this still waiting" is a model's account of its own reasoning. At 7am
those are not the same thing.

**Nothing starts that you did not authorise.** Consent binds to `launch_members.prompt_hash`, so a
ticket whose text changed after you read it does not launch. The hosted products are built the
other way round, acting on schedules and Slack threads without being asked. That is a reasonable
product and the opposite of this one.

**The app never takes the agent's word.** `Disposition` counts commits after a baseline; it does
not read a summary. A failed observation stops the tick (inv. 10) rather than deriving state from
an empty result, which is the difference between a quiet night and forty rows walked to the wrong
state at 3am.

One thing Projects has that this app does not: **shared context across a slice**. Today
`plan.Compose` is 542 bytes and a run inherits the composed prompt plus the ticket body, so six
agents in one slice each rediscover the same things. The lazy version is a note on the launch,
concatenated into every member's prompt, entered on the preview screen. It must go into
`prompt_hash`, or it is a way to change what agents are told without re-authorising.

Agents writing to that context as they go is the version to avoid: concurrent writes with no
ordering, and an agent's account of its own work is the one input this system is built to
distrust.

---

## 8 · Not settled

1. **The deny gate degrades on cloud rows.** A provider pushes before the app can refuse, so the
   rule becomes a flag rather than a refusal. The honest fix is branch protection on the remote,
   which is where a rule like that belongs anyway — and which an agent cannot add, since
   `.github/**` is deny-listed.
2. **The edge does not say why it is an edge.** A blocker might be a code dependency or an
   ordering preference, and `ProspectiveBase` treats a single same-repo blocker as both.
   Distinguishing them is an annotation from `to-tickets`, not app machinery.
3. **`disposeRun` mis-reads a cloud run today.** It guards on `obs.Worktrees[branch] != ""`, so no
   worktree means zero commits means `OutcomeFailed`. A cloud agent that did perfect work would
   land on the board as failed. Counting `baseline..origin/<branch>` is the change.
4. **Whether to rent the runner at all.** A small always-on host gets the overnight goal on its
   own, with no design change and no gate lost. The cloud runner earns its place only if its
   shared context and its interface beat a cold local agent, and that is measured by using it.
