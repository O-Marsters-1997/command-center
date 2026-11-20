# 25. Merged work removes its worktree

**Date:** 2026-11-20 · **Status:** accepted

## Context

A ticket whose pull request has merged reads `PRMerged`, and remove-worktree was the only verb it
offered. Nothing else was left to decide, yet the ticket sat on the rail until I pressed the verb,
and the worktree held its disk and its branch until then. Pressing the same button on every merged
ticket was a chore, not a decision.

Removal already refuses when it would lose work: a worktree with unpushed commits, a dirty
worktree, or an agent still waiting on an answer. The refusal is recorded as a
`remove_worktree_refused` event and shown on the ticket.

## Decision

**The loop removes a merged ticket's worktree without being asked.** An auto-removal is a
remove-worktree the loop queues itself for a ticket in `PRMerged` with a worktree present, no
pending intent and no recorded refusal.

- `plan.Snapshot.AutoRemovals` makes the decision; only `plan` derives
  ([ADR 13](0013-only-plan-derives.md)).
- The loop turns each auto-removal into a remove-worktree intent through `QueueVerbIntent`, the
  call the verb uses, so the rail shows the pending removal like any queued verb. The next tick
  consumes it through the unchanged `removeWorktreeOne` ([ADR 9](0009-the-loop-owns-reconciled-state.md)).
- **A recorded refusal stops auto-removal for that ticket.** The loop never retries on its own; the
  operator's remove-worktree verb is the only retry.

Removing straight from the tick, with no intent, was rejected: the rail would never show the
removal coming, and there would be two paths into removal to keep in step. Retrying a refusal on
every tick was rejected because a dirty or unpushed worktree stays that way until a human acts,
so each retry would only add another refusal event.

## Consequences

Merged tickets leave the rail a tick or two after the merge is observed, and their worktrees and
local branches go with them. This supersedes the "merged offers only remove-worktree" reading in
[the new UI gap audit](../designs/new-ui-gap-audit.md#3-settle-auto-settle-on-merge-undo--partial).
A ticket refused once before it merged (for example "neither merged nor withdrawn") also never
auto-removes, since the latest refusal is all that is stored; the verb still works.
