# 8. A feature is closed under blocked_by, and opening the launch modal imports it

**Date:** 2026-09-20 · **Status:** accepted

## Context

A ticket blocked from outside its own feature has a blocker the derivation cannot see, so it parks
with nothing to clear it, and "launch this feature" has no closed answer.

The launch action can fire on a feature with no rows in `tickets`, yet the modal needs them to compute
anything. Only the loop writes reconciled state ([ADR 9](0009-the-loop-owns-reconciled-state.md)),
and its tick is too slow for the primary action.

## Decision

- **A feature contains every unmerged blocker of its tickets.** A merged blocker is exempt.
  `ImportTickets` fails the whole transaction naming the ticket and outside blocker (an
  `import_refused` event and banner), and `applyEditTicketIntents` applies the same check to
  hand-edited `blocked_by`. Enforcing at launch was rejected: it yields a feature that looks fine and
  is unlaunchable.
- **Opening the modal (`POST /launch/open`) queues an import intent and nudges the loop** to tick now.
  The modal shows a pending state, then the candidates or the refusal from `store.LastImportError`.
  Reading the tracker directly was rejected because it adds a second DAG derivation beside the
  board's and `/graph.json`.

## Consequences

A chain split across features is illegal and the downstream import fails whole. The fix is in the
tracker, with no override. A cross-repo chain inside one feature stays legal.

The refusal is asynchronous, read by both the board banner and the modal. With closure, the
remaining launch refusals are per-ticket (active launch, conflicted base), so the modal computes
labels once server-side.

Cancelling leaves the feature imported. A click costs one `gh issue list` plus one dependency call per
issue, serially. The nudge channel is buffered to one and a full buffer drops it, since the running
tick picks up the intent anyway.
