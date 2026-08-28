# 6. Resolve a conflict once, not once per restack

**Date:** 2026-08-28 · **Status:** accepted

## Context

One conflict in a stacked batch was costing five to eight resolutions, and the same one every
time.

Two mechanisms produced that. The first is a git fact the design already half-documented.
`advanceOnto` restacks with `Rebase --onto` when a base squash-merges, which is correct: merging
would replay the branch's own copies of commits the base now carries under new SHAs. But
`git rebase` drops merge commits, and the resolve-merge-conflict procedure ends by committing the
merge. So the restack throws the resolution away and the branch stops on the identical conflict.
The count scales as stack depth times the number of times a base is rewritten, and every rewrite
cascades to the children.

The second is a sequencing fact nobody had written down. `unlockedOnBlocker` unlocks a child onto
its blocker's branch the moment that blocker has an *open* PR, when `stacking = true`. Nothing
asked whether the blocker's branch was in a fit state to be cut from. A base that no longer merges
into `main`, or whose worktree is sitting on an unresolved merge, hands its conflict to every
child cut from it, and each child then re-fights it on every restack of the parent.

## Decision

**`EnsureCheckout` turns on `rerere`.** Both arms, clone and existing-checkout, set
`rerere.enabled` and `rerere.autoupdate` to true. `autoupdate` is the half that matters in an
unattended loop: without it the replayed resolution comes back unstaged and still needs a human
`git add`. This lives in code rather than in a human's shell because it is local config, and a
re-clone on a bare machine is exactly the case with nobody there to set it by hand. Every worktree
of a clone shares `rr-cache`, so one recorded resolution serves every worktree, every rebase and
every restack.

**A launch never cuts from a base that already carries a conflict.** Observe asks
`git merge-tree --write-tree` whether each tracked branch still merges into `origin/main` and
records the answer on `Observation.ConflictsWithBase`, beside `MidMerge`. One derived read,
`conflictedBase`, names the base a cut would be made from when that base is unclean by either
measure, and `""` when it is fine. `main` is never unclean.

Three surfaces refuse on that one fact, because a launch is decided in three places and all three
have to agree: `LaunchCandidate` (the tick's own cut), `Preview` (what an operator authorises),
and `Status` (the row itself, which reads `blocked` and names the base). Without the third, a row
authorised before its base went bad would sit at "waiting for a slot" while every tick silently
refused to cut it.

The fact deliberately stays out of `Unlock`. `refresh` and `push` both call `Unlocked` to answer
"what is this branch based on", not "may this launch", and a conflicted base must not stop a
refresh: refreshing is how the conflict gets resolved in the first place.

**Two rules the code does not enforce.** Do not launch a child until its blocker has merged, where
waiting is affordable. A merged blocker cuts the child from `main` with the blocker's work already
in it, which deletes the whole restack class for that child. `stacking = false` is not the
alternative: it cuts the child from `main` while the blocker is still open, so the child is built
against a tree missing the code it depends on. And a wide rename runs alone. #126 renames `Task`
to `Ticket` across 56 files; anything open beside it conflicts with it, and that conflict is worth
paying exactly once.

## Consequences

Conflicts still stop the loop. `rerere` removes the thinking, not the stop: git still halts the
rebase, the row still reaches `refresh_conflicted`, `MidMerge` is still true, and a human still
types `git rebase --continue`. What changes is that the diff is already resolved and staged when
they get there. Do not read this as conflicts resolving unattended.

The gate refuses; it does not fix. A blocked child stays blocked until somebody resolves the
base's conflict, and the reason on the row is the only prompt to go and do that. Wiring
`cc/skills/resolve-merge-conflict/SKILL.md` to an agent is the change that would close that loop,
and it is a different change with a different risk. It also only pays off once `rerere` is on,
since handing an agent a conflict it has already solved once is pure waste.

A `path` repo gets `rerere` set in the operator's own checkout, not only in a clone the app made.
That is a write into a repository the app does not own. It is the same benefit and two
`git config --unset` calls undo it, so it does not earn a special case.

The conflict read costs one `git merge-tree` per tracked branch per tick, against `origin/main` as
fetched at the top of that tick. It touches no working tree and writes nothing.

The live clone predates this, so `rerere` was set on it by hand once: `EnsureCheckout` only fixes
checkouts it sees after the change lands.
