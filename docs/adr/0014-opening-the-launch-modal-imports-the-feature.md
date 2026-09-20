# 14. Opening the launch modal imports the feature

**Date:** 2026-09-20 · **Status:** accepted

## Context

The features list reads the tracker live and shows every feature it offers, imported or not. The
launch action sits on the row, so it can fire against a feature with no rows in `tickets` at all.

Everything the modal has to show needs those rows. `plan.Unlocked` walks `byURL` built from
`Store.Tickets`. `plan.Preview` needs a launch membership and a conflicted base. The prompt hash
that binds consent to content (§4b) is `plan.Hash(plan.Compose(t))` over a stored ticket. None of
it can be computed from a feature name.

Writing those rows is the loop's job and nobody else's. Invariant 9 says only the loop goroutine
writes the database; handlers insert intents. `handleImportFeature` follows it, queueing an import
intent and redirecting, and `applyImportIntents` runs at the top of `RunOnce`, before observe, so
an imported ticket has its branch and PR read on the same tick (`internal/cc/loop.go:77`). The
tick period is fifteen seconds (`internal/cc/loop.go:22`).

Fifteen seconds of nothing is not an acceptable response to the app's new primary action.

## Decision

**Opening the launch modal queues an import intent and nudges the loop to tick now.** `Loop.Run`
selects on a nudge channel beside its ticker; a nudge runs one tick immediately. The modal renders
a pending state and resolves into either the candidate list or the refusal it reads from
`store.LastImportError`.

The open is a `POST`, wrapped in `requireBrowserOrigin` like every other mutating handler. A GET
that writes would be the actual mistake here, and the modal is opened by a button, not a link.

Reading the tracker directly and importing at confirm was the alternative with the cleanest story:
nothing written for a modal you abandon, no wait at all. It buys a second derivation path. The
modal's DAG would come from `tracker.Ticket.BlockedBy` and the board's from `tickets.blocked_by`,
computed by different code over differently shaped data, agreeing until the first change to either.
The whole point of the fleet view is that `/graph.json` serves the same `[]group` the board ranges
over (`docs/prds/prd-fleet-view.md` § One derivation), and this would have put a third renderer
beside them reading somewhere else.

Polling without the nudge was the same design minus ten lines, and minus the only thing that makes
it usable.

## Consequences

Cancelling the modal leaves the feature imported and its tickets on the board. That is deliberate
and it is the line this design draws: an import is the tracker's shape, not a decision you made,
so persisting it costs nothing to undo and nothing to regret. What is never written before you
confirm is your *changes* to it, which is to say the unticks. Those live in the island and die
with it.

A click now costs one `gh issue list` plus one `gh api .../dependencies/blocked_by` per issue
(`internal/tracker/github.go:72`), serially. On a twenty-ticket feature that is twenty-one calls
before the modal fills. The nudge removes the tick's own latency and nothing else; if this becomes
the slow part, the fix is in `githubSource.Tickets`, not here.

The loop's period stops being the only thing that starts a tick. `RunOnce` was already idempotent
and already safe to call off-schedule, which is what `App.RunOnce` exists for, so the nudge adds a
caller rather than a mode. A nudge arriving mid-tick coalesces: the channel is buffered to one and
a full buffer is dropped, because a tick already in flight will pick up the intent anyway.

Two surfaces now read `store.LastImportError`: the board's banner and the pending modal. That is
one store method with two readers, and the banner keeps its job of reporting a refusal you walked
away from.
