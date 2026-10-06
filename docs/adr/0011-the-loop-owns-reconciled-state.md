# 11. The loop owns reconciled state

**Date:** 2026-09-20 · **Status:** accepted

## Context

Invariant 9 says only the loop writes the DB and verbs queue intents. `cc useradd` (#292) breaks that
literally: it hashes a generated password and inserts a row synchronously from the CLI, with no tick
to hand off to because the password exists only in memory until printed.

What invariant 9 protects (no racing writers, no half-made write on a page, no tick deriving from a
table mutating beneath it) concerns state a tick reads or a page renders as ticket truth. `users` and
`sessions` are neither: no tick step reads them, and nothing in them can drift from GitHub or a
worktree.

## Decision

Invariant 9 is narrowed: **the loop is the only writer of reconciled state**, meaning state a tick
observes, derives from, or a page renders. `users` and `sessions` sit outside. `cc useradd` writes
directly, and Postgres's `UNIQUE` on `email` guards concurrent calls. `POST /launch` still queues an
intent because `launches` is reconciled state.

## Consequences

`CreateUser` in `internal/cc/auth.go` is the one store write that bypasses the loop. Other writes to
`users` or `sessions` are fine only while no tick step reads either table. If one does (say
`sessions` gating a page's auth), writes move behind an intent. Writes to `tickets` or `launches`
outside the loop never qualify.
