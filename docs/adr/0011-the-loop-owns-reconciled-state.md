# 11. The loop owns reconciled state

**Date:** 2026-09-20 · **Status:** accepted

## Context

Invariant 9 says only the loop writes the DB and verbs queue intents. `cc useradd` (#292) breaks that
literally: it hashes a generated password and inserts a row synchronously from the CLI, with no tick
to hand off to because the password exists only in memory until printed. Grouping repos into
projects breaks it the same way: a web handler renames a project or moves a repo into one, and
there is nothing for a tick to reconcile.

What invariant 9 protects (no racing writers, no half-made write on a page, no tick deriving from a
table mutating beneath it) concerns state a tick reads or a page renders as ticket truth. `users`,
`sessions`, `projects` and `repos.project_id` are none of these: no tick step reads them, and
nothing in them can drift from GitHub or a worktree. A project scope changes which tickets a page shows, never what a ticket is.

## Decision

Invariant 9 is narrowed: **the loop is the only writer of reconciled state**, meaning state a tick
observes, derives from, or a page renders as ticket truth. `users`, `sessions` and projects are
configuration and sit outside. `cc useradd` writes directly, and Postgres's `UNIQUE` on `email`
guards concurrent calls. The web handlers write projects directly, with no intent and no nudge, and
a unique index on `lower(name)` guards concurrent creates. `POST /launch` still queues an intent
because `launches` is reconciled state.

## Consequences

`CreateUser` in `internal/loop/auth.go` and the project handlers in `internal/web` are the store
writes that bypass the loop. Other writes to `users`, `sessions` or projects are fine only while no
tick step reads those tables. If one does (say `sessions` gating a page's auth, or a project
steering which tickets launch), writes move behind an intent. Writes to `tickets` or `launches`
outside the loop never qualify.
