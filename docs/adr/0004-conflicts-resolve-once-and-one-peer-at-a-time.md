# 4. Conflicts are resolved once, and one conflicting peer at a time

**Date:** 2026-08-28 · **Status:** accepted

## Context

One conflict in a stack cost five to eight resolutions. `advanceOnto` restacks with `Rebase --onto`,
which drops the merge commit the resolve procedure ends with, so the branch stops on the same
conflict again. And `unlockedOnBlocker` unlocks a child onto a blocker with an open PR without asking
whether that branch still merges into `main`, so every child inherits its conflict.

Two unrelated `main`-based branches that conflict with each other can both reach `review_me`; the
second merge then fights the first's conflict by hand. They share no blocker edge, so nothing
compared them.

## Decision

- **`EnsureCheckout` enables `rerere`** with `autoupdate` (without it the replayed resolution comes
  back unstaged), on clone and existing checkout. Worktrees share `rr-cache`.
- **A launch never cuts from a conflicted base.** Observe records `ConflictsWithBase` from
  `git merge-tree --write-tree` against `origin/main`. `conflictedBase` names the unclean base, and
  `LaunchCandidate`, `Preview` and `Status` all refuse on it (`Status` reads `blocked` and names the
  base). It stays out of `Unlock`, because refreshing is how the conflict gets resolved.
- **Observe records each peer pair** in `ConflictsWithPeer`, one `MergesCleanly` read per ordered tip
  pair so an unmoved tick costs nothing. `conflictingPeerHold` in `internal/cc` walks open
  `main`-based candidates in branch-name order and holds a ticket when an earlier one conflicts with
  it. The lower ref is never held, a held peer does not hold others, and stacked branches are out of
  scope. Held tickets read `blocked` naming the peer (`RunFact.ConflictingPeer`, ranked below
  `ConflictsWithMain` and above `VerdictReviewMe`).
- Two rules the code does not enforce: do not launch a child before its blocker merges where waiting
  is affordable (`stacking = false` cuts it from a tree missing its dependency), and run a wide
  rename alone.

## Consequences

Conflicts still stop the loop: `rerere` removes the thinking, not the stop, so a human still runs
`git rebase --continue` on an already-resolved diff. The gate refuses and does not fix. Only one of a
conflicting pair shows `review_me`, so the second is resolved once, not at a surprise merge.

A `path` repo gets `rerere` set in the operator's own checkout; an older checkout needs it set once.
After the gates:

- A conflict confined to build-regenerated paths needs no agent (#177): the app merges, runs
  `build_command`, commits and pushes. A repo with no `build_command` waits for `resolve`.
- `resolve` (#178) spawns an agent on `cc/skills/resolve-merge-conflict/SKILL.md` that commits
  nothing. The row lands at `conflict_resolved` with the resolution staged for a human.
