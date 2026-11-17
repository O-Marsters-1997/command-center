# 7. cc proves what treepad cannot

**Date:** 2026-09-19 · **Status:** accepted

## Context

`treepad.Remove` with `Force` skips the ancestor check a squash merge fails, but still refuses a
branch with unpushed commits by comparing it to its upstream (`ErrUnpushed`) and a dirty worktree
(`ErrDirty`). GitHub deletes the branch on merge and `Fetch` prunes, so the upstream ref is
usually gone; with no upstream the library has nothing to compare and cannot tell a squashed branch
from one that was never pushed.

cc records every push's tip in `pushes`, so it can ask "does this branch sit where cc last pushed it".
A merged PR's head is already observed, so a branch exactly there is equally proven.

## Decision

- **cc picks the removal path before treepad runs.** `RemovalStateFor`: ref present means
  `RemovableByMerged` (treepad checks the upstream). Ref gone and branch at cc's last pushed tip, or
  at the merged PR's head, means `RemovableByForce`, recorded on the `worktree_removed` event. Ref
  gone and branch moved on means `NotRemovable`, which refuses naming unpushed commits.
- **Both paths call `Remove{Force: true}`.** `Force` skips only the ancestor check, so the library's
  `ErrDirty` is the dirty refusal and cc does not run its own `git status`.
- **Pushes set the upstream.** A branch cut from `origin/<base>` tracks it, so cc pushes with `-u`;
  otherwise every pushed commit would read as unpushed to the library.
- **Teardown runs before the GitHub issue closes.** A stuck ticket retries teardown, not the close. A
  missing worktree (`ErrNotFound`) counts as done, so a later close failure retries straight to the
  close. `ErrPostHook` means the removal happened, so it is logged and the teardown carries on.
- **The row shows its last `remove_worktree_refused`** as its warning while the verb is offered.
  Refusals are not persisted state; a removal withdraws the ticket and drops the row.

## Consequences

No confirmation gates the forced path: merged, pruned, at a proven tip and clean is what a confirm
button would ask a human to assert, and they could not verify it. Dirty worktrees and branches ahead of
every proven tip still refuse. A repo whose `.treepad.toml` configures an interactive hook cannot be
cut or removed by cc: the library refuses it with `ErrInteractiveHook`, and the text reaches the
`cut_failed` or refusal event.
