# 9. The loop owns reconciled state

**Date:** 2026-09-20 · **Status:** accepted

## Context

Racing writers, a half-made write showing on a page, and a tick deriving from a table that changes
beneath it are all hazards of state a tick reconciles against GitHub and the worktrees. Projects are
not that state: nothing in them can drift from GitHub or a worktree, and a project scope changes
which tickets a page shows, never what a ticket is.

## Decision

**The loop is the only writer of reconciled state**: state a tick observes or derives from, or a page
renders as ticket truth, such as `tickets`, `launches`, `runs` and their events. Verbs queue intents
for the loop and nudge it.

Projects are configuration and sit outside. The project handlers write projects directly, guarded by
a unique index on `lower(name)`.

## Consequences

The project handlers in `internal/web` are the writes that bypass the loop. A
table moves behind an intent the moment a tick step derives from it. Writes to `tickets` or `launches`
outside the loop never qualify.
