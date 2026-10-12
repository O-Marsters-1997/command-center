# 5. Exhaustion is derived, never stored

**Date:** 2026-09-13 · **Status:** accepted

## Context

Running authorised features one at a time needs a notion of a launch having nothing left to start.
That is a fact about pull requests the tick already reads.

## Decision

**A slice is exhausted once every ticket it covers has a pull request, computed fresh each tick.**
`launches.state` is `active` or `cancelled`. `LaunchPlan` orders candidates
by launch and fills slots from the oldest launch not exhausted.

Cancelling is a human decision nothing else records, so it is stored. Exhaustion is a fact about PRs,
so it is derived.

Exhausted means the agents are finished, not merged, because the app never merges. Waiting for merge
would strand the fleet overnight.

## Consequences

A PR closed unmerged at 03:00 reclaims its slots next tick, where a stored `done` would go stale. The
next feature starts while the previous sits in review.

Order is `launches.id` (insertion), with no rearrangeable queue. Adding one means storing a position:
a priority you set is a decision, so it belongs in a column.
