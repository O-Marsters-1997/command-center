# 6. Exhaustion is derived, never stored

**Date:** 2026-09-13 · **Status:** accepted

## Context

`LaunchPlan` took candidates in `ORDER BY url`, so whichever feature's URLs sorted first took every
freed slot until it drained. Running authorised features one at a time needs a notion of a launch
having nothing left to start. The design doc listed `launches.state` as `active/done/cancelled`, but
no code ever wrote `done`.

## Decision

**A slice is exhausted once every ticket it covers has a pull request, computed fresh each tick.**
`launches.state` keeps `cancelled` and nothing else. `LaunchPlan` orders candidates by launch and
fills slots from the oldest launch not exhausted. This supersedes design §8's `done` and §4b's
"active until every member is terminal"; do not add `done`.

Cancelling is a human decision nothing records, so it is stored. Exhaustion is a fact about PRs the
tick already reads, so it is derived.

Exhausted means the agents are finished, not merged, because the app never merges (invariant 2).
Waiting for merge would strand the fleet overnight.

## Consequences

A stored `done` goes stale: a PR closed unmerged at 03:00 would leave its launch `done` with nothing
to reopen it, whereas derived exhaustion reclaims the slots next tick. The next feature starts while
the previous sits in review. Human verbs such as `applyReRunIntents` are not gated on it.

Order is `launches.id` (insertion), with no visible or rearrangeable queue. Adding one means storing a
position, which fits this ADR: a priority you set is a decision, so it belongs in a column.
