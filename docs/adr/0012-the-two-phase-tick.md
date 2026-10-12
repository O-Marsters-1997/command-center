# 12. The two-phase tick

**Date:** 2026-10-06 · **Status:** accepted

## Context

A tick that interleaves reading and acting puts each step wherever an earlier step's side effect
happens to be visible, so reordering one means re-deriving by hand.

## Decision

A tick is observe, **absorb**, one derive, then **act**.

- Absorb records what already happened: cancels and kills, run disposition with its spend transcript
  reads and interval fit, verdict transitions, merge and first-CI events. It spawns and pushes
  nothing, so a run a kill stops is disposed in the same tick.
- One `PlanInput` and one `Derive` produce the snapshot act reads. Act does not re-derive; only the
  observation's live parts (worktrees, run liveness, mid-merge) move during it.
- Act runs the operator's verbs before the automatic steps, and retarget after the spawning verbs and
  before refresh, so a refresh never repeats a restack.
- Import and edit-ticket intents run before observe, so the observation reads the new branch.
- Steps are plain functions called in order in `RunOnce`: no slice of steps, no dispatch table.
- Launch asks `Snapshot.LaunchAfter(spawned)`, so a verb's spawn and an automatic launch share
  `max_agents`.

## Consequences

A run disposed in absorb is pushed in the same tick. A verdict transition and the first-CI event for a
push made in act are recorded by the next tick.
