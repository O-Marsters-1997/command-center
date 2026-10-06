# 9. A feature is closed under blocked_by, and opening the launch modal imports it

**Date:** 2026-09-20 · **Status:** accepted

## Context

`ImportTickets` refused a ticket claimed by two features but not one blocked from outside its own
feature. Such a chain made `plan.Unlocked` refuse an unknown blocker, and `handleLaunch` queue a
ticket `plan.Preview` had refused, leaving it parked with nothing to clear it (#235). "Launch this
feature" had no closed answer, and the refusal arrived per ticket after authorisation.

The launch action can also fire on a feature with no rows in `tickets`, yet the modal needs them to
compute anything. Only the loop writes (invariant 9), and its fifteen-second tick is too slow for the
primary action.

## Decision

- **A feature contains every unmerged blocker of its tickets.** A merged blocker is exempt.
  `ImportTickets` fails the whole transaction naming the ticket and outside blocker (an
  `import_refused` event and banner), and `applyEditTicketIntents` applies the same function to
  hand-edited `blocked_by`. Enforcing at launch was rejected: it yields a feature that looks fine and
  is unlaunchable.
- **Opening the modal `POST`s an import intent and nudges the loop** to tick now. The modal shows a
  pending state, then the candidates or the refusal from `store.LastImportError`. Reading the tracker
  directly was rejected because it adds a second DAG derivation beside the board's and `/graph.json`.

## Consequences

A chain split across features is illegal and the downstream import fails whole. The fix is in the
tracker, with no override. A cross-repo chain inside one feature stays legal.

The refusal is asynchronous, read by both the board banner and the modal. With closure, `Preview`'s
remaining refusals are per-ticket (active launch, conflicted base), so the modal computes labels once
server-side.

Cancelling leaves the feature imported, since an import is the tracker's shape and only your unticks
live in the island. A click costs one `gh issue list` plus one dependency call per issue, serially;
the fix is in `githubSource.Tickets`. The nudge channel is buffered to one and a full buffer drops it,
since the running tick picks up the intent anyway.
