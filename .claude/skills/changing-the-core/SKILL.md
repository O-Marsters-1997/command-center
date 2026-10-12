---
name: changing-the-core
description: >
  How to change the command-center core: a new verb, state, intent or tick step across
  internal/plan, internal/loop and internal/web. Holds the hand-synced checklists (a missed place
  is a silent no-op), the tick phases, the intent error policy and the derivation rules.
  TRIGGER — load BEFORE editing internal/plan or internal/loop, adding a verb, state, intent or
  tick step, or touching handleVerb in internal/web/server.go. Plain requests count: "add a button
  that…", "a new state for…", "the loop should also…". Load alongside go-idiomatic and go-testing.
  SKIP for template-only or CSS-only work and for store queries that no tick step reads.
---

# Changing the core

The core is `internal/plan` (pure decisions) and `internal/loop` (the tick). Verbs reach it from
`internal/web`. Rules: [ADR 11](../../../docs/adr/0011-the-loop-owns-reconciled-state.md),
[ADR 14](../../../docs/adr/0014-the-two-phase-tick.md),
[ADR 15](../../../docs/adr/0015-only-plan-derives.md). Read them for the why; this file is the
where.

## The tick

`RunOnce` is: intents that must land before observe (track, import, edit-ticket), **observe**,
**absorb**, one `l.derive` (`PlanInput` plus `Rules.Derive`), **act**.

| Phase | Holds | Never |
|---|---|---|
| absorb | Recording what already happened: repo state, runs disposed, verdict transitions, merged and first-CI events, cancel and kill intents | Spawns or pushes |
| act | The operator's verbs, then retarget, refresh, push, the draft gate, launch | Re-derives: `snap` is fixed, only `obs` (worktrees, run liveness, mid-merge) is live |

Steps are plain `func() error` literals in `absorb` and `act`, run in order by `runSteps`. No
slice, no dispatch table. Order is meaning: a spawning verb runs before retarget, retarget before
refresh, and launch last so `Snapshot.LaunchAfter(spawned)` shares `max_agents` with verb spawns.
`launchReviews` also spawns, after refresh. A new step goes where its inputs are already true and its effects are not needed earlier.

## New verb

Miss one and the verb queues, then does nothing.

1. `Verb*` const in `internal/plan/verbs.go`.
2. `IsRowVerb`, or `handleVerb` answers 400 "unsupported verb".
3. `Verbs(state)` for every state that offers it. A row shows at most 3 buttons, `VerbFollowUp`
   excluded (`verbs_test.go` enforces it).
4. `apply<X>Intents` in `internal/loop`, built on `eachIntent` and looking the ticket up with
   `snap.Entry(intent.TicketID)`. `l.ticket` reads the whole tickets table per call; use it only
   where the handler needs a `store.Ticket` that `plan.Ticket` lacks.
5. Its position in `act()`. If it spawns, it runs before launch and goes through `spawnRun`,
   which records it in `l.spawned`.
6. If it carries a payload, the branch in `handleVerb` (`VerbFollowUp` is the model: validate,
   then `QueueVerbIntentWithPayload`) and the loop handler reading `intent.Payload`.
7. The button: `board.tmpl` ranges over `.Verbs` and `session.tmpl` over `.ActionVerbs`, so a
   plain verb needs no template edit; one with a payload or a confirm needs its form there.
8. Tests:
   - a `verbs_test.go` table row per state that offers it;
   - a loop test that ticks **twice** and asserts the second tick does nothing: intents are
     consumed, so a handler that re-acts or leaves the intent queued shows up here;
   - a web test that the verb on a state not offering it answers 409, beside the one in
     `internal/web/server_test.go`.

`handleVerb` order is: `IsRowVerb` (400), payload check (400), `snap.Entry` (400 unknown),
`snap.Offers` (409), queue. Keep refusals before the queue.

## New state

1. Const in `internal/plan/plan.go` **before** `stateCount`.
2. `State.String()`: a distinct name; only `Blocked` may read `"blocked"`
   (`TestStateStringIsDistinctAndOnlyBlockedReadsBlocked`).
3. The `Status` branch. **Branch order is the precedence rule**: `BaseGone`, then `PRMerged`,
   then `statusFromRun` (alive, then outcome), then the pre-launch facts. Put the branch where
   its fact should win, and add a `status_test.go` case proving it beats the states below it
   and loses to those above.
4. `Verbs(state)`, and the `verbs_test.go` row.
5. `Unattended()` lists every state in an explicit case, not only in `default`;
   `TestUnattendedNamesEveryState` fails otherwise, and `exhaustive` lints the switch.
6. `Glyph(state)`: one of the eight glyph words. The `default` is `GlyphFailed`, so a state
   left out looks like a failure.
7. The state list in `docs/designs/command-centre-design.md` § 5 (the one normative list) and
   `CONTEXT.md` if the term is new.

## Intent error policy

`eachIntent` owns it. Handlers return errors; they do not write the event or consume the intent.

- **Refusal** (nothing to do, wrong precondition): the handler records its own event and returns
  `nil`. The intent is consumed.
- **Per-intent failure** (a git error, a failed spawn): return the error. `eachIntent` appends an
  `intent_failed` event naming the verb, then consumes the intent, and the tick carries on. The
  operator presses the verb again.
- **Infrastructure failure** (the loop's own store call fails): return it from `eachIntent` itself
  and the tick aborts. The event is written before the consume, so an outage leaves the intent
  queued.
- **`errIntentPending`** is the only way to keep an intent queued (a follow-up while the run is
  alive). Do not invent another.

## Derivation rules

- New inputs enter through `plan.Input`, `Facts` and `RunFact`, filled in by the store or loop.
  `plan` imports only `spend` and `verdict`, and `api_test.go` enforces it.
- Only `plan` derives. No `State.String()` comparison, no state switch and no re-deriving a
  verdict outside it; ask `Snapshot.Entry` or `Snapshot.Offers`.
- `plan` never calls `time.Now`. Time arrives as `Input.Now`; the loop passes `l.clock.Now()`.
- Reconciled state is written only by a tick step. A web handler queues an intent
  (`QueueVerbIntent`); configuration rows (users, sessions, projects) are the ADR 11 exception.

## Checking

- `go test ./internal/plan` is cheap and catches most misses. Run the loop tests (Docker) for any
  new intent.
- `go test ./internal/web -update` only when a golden should change; read the diff.
- After `internal/store/queries` or a migration change, `just sqlc` and commit `ccdb`.
