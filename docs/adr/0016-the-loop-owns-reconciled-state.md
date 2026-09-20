# 16. The loop owns reconciled state

**Date:** 2026-09-20 · **Status:** accepted

## Context

Invariant 9 (`docs/designs/command-centre-design.md` § 9) reads: "within [the flock], only the
loop goroutine writes the DB — verbs are queued intents." Until now every writer honoured that
literally. A handler that needs a row to exist right away, like `POST /launch`
(`e2e/register/register.go:82`), writes an `intents` row and stops there; the loop is what turns
that row into a real `launches` or `tickets` write on its next tick. That indirection is the whole
point of the invariant: state that later ticks observe, derive from and reconcile against GitHub
and the filesystem is never left half-written by two writers racing, because there is only ever
one.

`cc useradd` (issue #292) breaks the literal rule. It generates a password, hashes it and inserts
the row in one synchronous call from the CLI process, with no flock and no intent queued for a
later tick to consume. There is no "later tick" to hand this off to: the password only exists in
memory for the one moment `useradd` prints it, and an intent record would still need something to
read it back out and finish the write before the process exits.

The question is whether that is a second hole in invariant 9 or a sign the invariant was drawn
one property too wide. Every property invariant 9 actually protects — no two writers racing on the
same row, no page rendering a write that half-happened, no tick deriving state from a table
another goroutine is mutating underneath it — is a property of *reconciled* state: state a tick
reads, a `plan.Status` derives from, or a page renders as ticket truth. `users` and `sessions` are
none of that. No `ObserveFunc`, no plan function and no tick step reads either table; nothing about
a user account can drift against GitHub or a worktree, because nothing ties it to either. The
concurrent-writer hazard invariant 9 exists to prevent doesn't arise for a table the loop never
looks at.

## Decision

Invariant 9 is narrowed rather than excepted: **the loop is the only writer of reconciled
state** — state a tick observes, derives from, or a page renders. `users` and `sessions` sit
outside that boundary. `cc useradd` writes `users` directly, synchronously, with no flock and no
queued intent; Postgres's own `UNIQUE` constraint on `email` is the concurrency guard two
simultaneous `useradd` calls need, not the loop's serialisation.

This composes with the `POST /launch` exception already named at `e2e/register/register.go:82`
rather than replacing it. That one queues an intent *for* the loop, because the state it ultimately
writes (`launches`) is reconciled state. `cc useradd` writes a table the loop never touches at all.
Both are readings of the same narrower invariant, not two unrelated carve-outs.

## Consequences

`internal/cc/auth.go`'s `CreateUser` is the store's one write path that bypasses the loop
entirely, and is named as such at its call site. A future verb that writes `users` or `sessions`
from outside the loop stays fine under this ADR as long as no tick step ever reads either table;
the day one does — `sessions` gating a page's own auth check, say — that table becomes reconciled
state and its writes must move behind an intent like every other one, the same trigger that would
force a revisit of `POST /launch`'s exception. A future verb that writes *reconciled* state (a
`tickets` or `launches` row) from outside the loop does not get this reading — it must queue an
intent, per invariant 9 as already narrowed.
