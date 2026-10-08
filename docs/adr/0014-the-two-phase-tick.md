# 14. The two-phase tick

**Date:** 2026-10-06 · **Status:** accepted

## Context

`RunOnce` interleaved reading and acting: it disposed a run, derived, pushed, recorded verdict
transitions, derived again, ran the verbs, derived a third time and launched. Each step sat where an
earlier step's side effect happened to be visible, so reordering one meant re-deriving by hand, and
a verb's spawn reached `max_agents` only because a late derive happened to see it.

## Decision

A tick is observe, **absorb**, one `PlanInput` and `Derive`, then **act**.

- Absorb records what already happened: cancels, run disposition with its spend transcript reads
  and interval fit, verdict transitions, merge and first-CI events. It spawns and pushes nothing.
  Kill intents are applied first in absorb, so the run they stop is disposed in the same tick.
- Act runs the operator's verbs (re-run, follow-up, abort, resolve, refresh, retry-push,
  remove-worktree, commit-resolution) before retarget, push, the draft gate and
  launch.
- Retarget runs after the spawning verbs and before refresh, so a refresh never repeats a restack.
- Import and edit-ticket intents still run before observe, so the observation reads the new branch.
- Steps stay `func(ctx, snap) error`, called in order in `RunOnce`: no slice, no dispatch table.
- Launch asks `Snapshot.LaunchAfter(spawned)`, so a verb's spawn and an automatic launch share
  `max_agents`.

## Consequences

A run disposed in absorb is pushed in the same tick. A verdict transition and the first-CI event for a push made in act are
recorded by the next tick.

The snapshot is not re-derived inside act, so only `obs` (worktrees,
run liveness, mid-merge) is live there.
