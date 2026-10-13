# 3. Conflicts are resolved once, and one conflicting peer at a time

**Date:** 2026-08-28 · **Status:** accepted

## Context

A restack with `rebase --onto` drops the merge commit a resolution ends with, so without help a branch
stops on the same conflict at every restack, and every child unlocked onto a conflicted blocker
inherits its conflict. Two unrelated `main`-based branches that conflict with each other share no
blocker edge, so nothing else compares them, and the second merge fights the first's conflict by hand.

## Decision

- **`EnsureCheckout` enables `rerere`** with `autoupdate` (without it the replayed resolution comes
  back unstaged). Worktrees share `rr-cache`.
- **A launch never cuts from a conflicted base.** Observe records `ConflictsWithBase` from
  `git merge-tree --write-tree` against `origin/main`. `plan.ConflictedBase` names the unclean base,
  and launch, preview and status all refuse on it (status reads `blocked` and names the base). It
  stays out of unlock, because refreshing is how the conflict gets resolved.
- **Observe records each peer pair** in `ConflictsWithPeer`, one `MergesCleanly` read per ordered tip
  pair, cached by tip so an unmoved tick costs nothing. `plan` walks open `main`-based candidates in
  branch order and holds a ticket when an earlier one conflicts with it. The lower ref is never held,
  a held peer does not hold others, and stacked branches are out of scope. A held ticket reads
  `blocked` naming the peer, ranked below `ConflictsWithMain` and above `VerdictReviewMe`.

## Consequences

Conflicts still stop the loop: `rerere` removes the thinking, not the stop. The gate refuses and does
not fix; the owner resolves in the worktree, or refreshes. Only one of a conflicting pair shows
`review_me`, so the second is resolved once, not at a surprise merge.
