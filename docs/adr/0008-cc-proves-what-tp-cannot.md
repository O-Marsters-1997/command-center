# 8. cc proves what tp cannot

**Date:** 2026-09-19 · **Status:** accepted

## Context

`tp remove --merged` skips the ancestor check a squash merge fails, but still refuses a branch with
unpushed commits by comparing it to its remote-tracking ref (#147). GitHub deletes the branch on merge
and `Fetch` prunes, so the ref is usually gone and tp refuses even though the commits are already
squashed into `main`. The refusal was also an event nothing read back.

cc records every push's tip in `pushes`, so it can ask "does this branch sit where cc last pushed it".
A branch that took hand-pushed commits (lint fixes, merges from main) differs from that record, but a
merged PR's head is already observed, so a branch exactly there is equally proven.

## Decision

- **cc picks the removal path before tp runs.** `RemovalStateFor`: ref present means
  `RemovableByMerged` (tp checks). Ref gone and branch at cc's last pushed tip, or at the merged
  PR's head (#342), means `RemovableByForce`, which calls `tp remove --force`. Ref gone and branch moved on means
  `NotRemovable`, which refuses naming unpushed commits.
- **cc checks dirtiness itself**, first, via `git status --porcelain`, because `--force` bypasses tp's
  check.
- **Teardown runs before the GitHub issue closes.** A stuck ticket needs to retry teardown, not
  re-close an issue. A missing worktree already counts as done (#196), so a later close failure
  retries straight to the close.
- **The row shows its last `remove_worktree_refused`** as its warning while the verb is offered.
  Refusals are not persisted state (inv. 14); a removal withdraws the ticket and drops the row.

## Consequences

No confirmation gates the forced path: merged, pruned, at the last pushed tip and clean is what a
confirm button would ask a human to assert and they could not verify. Dirty worktrees and branches ahead of every proven tip
still refuse. tp is unchanged; only who reaches for `--force` changed.
