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
  and interval fit, verdict transitions, merge and first-CI events. It spawns, kills and pushes
  nothing.
- Act runs the operator's verbs (kill, re-run, follow-up, abort, resolve, refresh, retry-push,
  re-check, close-pr, remove-worktree, commit-resolution) before retarget, push, the draft gate and
  launch.
- Import and edit-ticket intents still run before observe, so the observation reads the new branch.
- Steps stay `func(ctx, snap) error`, called in order in `RunOnce`: no slice, no dispatch table.
- Launch asks `Snapshot.LaunchAfter(spawned, killed)`, so a verb's spawn and an automatic launch
  share `max_agents`, and a kill frees its slot in the tick that sends it.

## Consequences

A run killed in act is disposed by the next tick's absorb, not the same one. A run disposed in absorb
is pushed in the same tick. A verdict transition and the first-CI event for a push made in act are
recorded by the next tick.
