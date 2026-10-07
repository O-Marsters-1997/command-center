# 15. Only plan derives

**Date:** 2026-10-06 · **Status:** accepted

## Context

The board, the launch preview and the loop each rebuilt a ticket's state from the store and the
observation, in `internal/loop`: `loadTicketFacts` made eight store reads, `derive` and `runFactFor`
turned them into labels, and `conflictedBase`, `conflictingPeerHold`, `draftReasonFor` and
`applyVerdict` lived beside them. Three gate tests pinned the pieces separately, and a caller could
reach a different answer than the board by composing the pieces itself.

## Decision

Only `internal/plan` derives. `Rules.Derive(Input)` returns a `Snapshot` holding every ticket's
unlock, state, reason, run fact, conflicted base and draft reason, and `Snapshot.Offers` answers
whether a row's state offers a verb.

- The store returns `plan.Input` (`Store.PlanInput`) and nothing else. There is one adapter, with no
  store interface and no fake.
- `plan` owns `Observation` and the fact types a derivation reads (`RunSummary`, `PushFact`,
  `RefreshFact`, `LaunchMembership`), so it imports nothing impure.
- The web derives per render. Nothing is cached between requests, so a page never shows a label the
  stored facts no longer support.
- `POST /verb` reads the snapshot and answers 409 with the row's state when it does not offer the verb.
- The gates are tested through `Derive` as table tests, not layered under it.

## Consequences

`render` and `handleVerb` call no `plan.Unlocked`, `planTicketsByURL` or `prsByBranch`. The loop,
push and refresh steps still derive their own narrower views; moving them onto the snapshot is
separate work, as is the launch preview.
