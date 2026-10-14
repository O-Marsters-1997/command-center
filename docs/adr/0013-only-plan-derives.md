# 13. Only plan derives

**Date:** 2026-10-06 · **Status:** accepted

## Context

The board, the launch preview and the loop all need a ticket's state. If each composes it from the
store and the observation, a caller can reach a different answer than the board.

## Decision

Only `internal/plan` derives. `Rules.Derive(Input)` returns a `Snapshot` holding every ticket's
unlock, state, reason, run fact, conflicted base and draft reason, and `Snapshot.Offers` answers
whether a row's state offers a verb.

- The store returns `plan.Input` (`Store.PlanInput`). There is one adapter, with no store interface
  and no fake.
- `plan` owns `Observation`, the fact types a derivation reads and the value sets they carry, such
  as `RepoPhase`. The store converts its columns to them at the boundary, through `Parse…`.
- `plan` imports only the stdlib-only leaves `spend`, `verdict` and `option`, enforced by depguard.
  A fact that may be absent is an `option.Option[T]`, never a `*T`.
- The web derives once per render from one frame of store reads. It caches no derived state between
  requests, so a page never shows a label the stored facts no longer support. External reads such as
  pushable repository listings and settled log metrics may be cached; neither cache holds its mutex
  during I/O.
- `POST /verb` reads the snapshot and answers 409 with the row's state when it does not offer the verb.
- The gates are tested through `Derive` as table tests, not layered under it.

## Consequences

A new rule is a change in `plan` and nowhere else. A handler that wants a ticket's state asks the
snapshot; one that composes it from store reads is a bug.
