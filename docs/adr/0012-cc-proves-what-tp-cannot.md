# 12. cc proves what tp cannot

**Date:** 2026-09-19 · **Status:** accepted

## Context

`removeWorktreeOne` tears a merged (or base-gone) ticket's worktree down via `tp remove --merged`.
`--merged` skips only the ancestor check a squash merge always fails; tp still refuses a dirty
worktree, or a branch with unpushed commits, by comparing the branch against its own
remote-tracking ref (issue #147).

That check needs the ref to exist. Both repos this app drives delete a branch's remote copy the
moment its PR merges, and this app's own `Fetch` runs `--prune`, so by the time a human presses
remove-worktree the ref is usually already gone. tp then has nothing left to compare against and
refuses with "has N unpushed commit(s)" — even when those N commits are exactly the ones already
squashed into `main`, just under a different SHA. The row was stuck: `--merged` cannot see past a
ref GitHub itself deleted, and the refusal was also write-only, an event with nothing reading it
back, so pressing the verb again looked like nothing had happened.

cc already carries what tp is missing. `pushOne` records every push's own tip in the `pushes`
table before this ever runs, so cc can ask a narrower question than tp can: not "does a
remote-tracking ref agree with this branch", but "does this branch sit exactly where cc itself
last pushed it". The two checks were built to divide the labour — tp owns the case it can still
verify, cc owns the one case it cannot (issue #147) — but cc's own half only ever fed back into
the same `--merged` call, so a ref gone missing had no path forward at all.

## Decision

**cc resolves which of three states a branch is in, before tp ever runs.** `RemovalStateFor`
checks the remote-tracking ref first: present, and tp can still check the branch itself
(`RemovableByMerged`). Gone, and the branch sits at the tip cc itself last pushed
(`RemovableByForce`) — the fact only cc can still prove. Gone, and the branch has moved past that
tip (`NotRemovable`) — genuinely unsafe, and now unverifiable by tp either.

**A dirty worktree is checked by cc directly, always.** `--force` bypasses tp's own dirty check
along with its unpushed one, so cc no longer defers that question to tp on either path: `Dirty`
runs `git status --porcelain` before either removal check, and a dirty worktree refuses before tp
is ever invoked.

**`RemovableByForce` calls `tp remove --force`; every other outcome behaves as before.** The ref
still resolving takes `--merged`, letting tp run its own check exactly as it always has.
`NotRemovable` refuses, naming unpushed commits, exactly as it did when that was tp's own
refusal message. Only the pruned-and-provably-safe case changes: cc now finishes what it already
started proving.

**Teardown runs before the GitHub issue closes, not after.** The original order closed the issue
first, reasoning that `tp.Remove` was the irreversible step and a failed close should leave
retryable state behind. That reasoning breaks once tp can refuse for a reason a human cannot
fix without cc's own intervention — a stuck ticket needs to retry the *teardown*, not spend every
retry closing an already-closed issue. Reversing the order costs nothing here: issue #196 already
treats a missing worktree as the state this verb is trying to reach, so a close failure after a
real teardown still leaves the row in place, and a retry finds no worktree left to remove and goes
straight to the close.

**The row shows its own last refusal.** `remove_worktree_refused` was fired and forgotten. Its
detail is now read back, keyed by ticket, and rendered as the row's own warning for as long as its
state still offers the verb — the same channel invariant 2's ready-to-merge flag uses, generalised
from a fixed label to the refusal's own words. Refusal is not persisted state (inv. 14): a
successful removal withdraws the ticket, dropping the row before any stale refusal could ever
render again.

## Consequences

No human confirmation gates the forced path. cc proving merged, pruned, at the last pushed tip and
clean is deliberately treated as equivalent to what a human pressing a confirm button would be
asked to assert — asking anyway would be a rubber stamp on a fact the human has no way to verify
themselves.

A dirty or genuinely diverged worktree still refuses, visibly, on the row — no verb bypasses that
gate, forced or not.

Nothing here changes what tp itself does; `--force` was already tp's own flag. This only changes
who decides to reach for it, and once, in `removeWorktreeOne`.
