# 10. One conflicting peer at a time

**Date:** 2026-09-13 · **Status:** accepted

## Context

`conflictedBase` (docs/adr/0006-resolve-a-conflict-once.md) refuses a launch cut from a base that
already carries a conflict, but it says nothing about two open, unrelated, `main`-based branches
that conflict with each other. Both can reach `review_me` at once, both can be merged, and the
second merge then fights the first one's conflict by hand at merge time — exactly the unattended
resolution `rerere` was meant to remove, just moved from restack time to merge time.

A peer is not a sibling: two `main`-based branches share no blocker relationship, so nothing in
`Unlock` or `conflictedBase` ever compares them. The comparison has to be pairwise, and it has to
run whether or not either ticket has a `BlockedBy` edge to the other.

## Decision

**Observe records the pair, never the decision.** For every pair of a repo's tracked branches with
a resolved tip, one `git merge-tree` read (`MergesCleanly`) says whether they merge together, kept
on `Observation.ConflictsWithPeer` beside `ConflictsWithBase`, keyed under each branch so either
side of the pair can be looked up once something else knows which one should yield. Observe has no
way to break that tie itself: two branches merged together are symmetric, and nothing about the
git fact says which one goes first.

**The sweep runs in ref order, before the rows.** `internal/cc` is the one place that has an
ordering over tickets, so `conflictingPeerHold` sorts every open, `main`-based candidate by branch
name and walks them in that order: a ticket is held the moment an earlier (lower-ref) candidate's
recorded pair says the two conflict. The lower ref is never held by this gate — only ever by
whatever its own run and verdict already say — so of any conflicting pair exactly one proceeds.

**`blocked` with a distinct reason, not a twentieth state.** The held ticket's `RunFact` carries
`ConflictingPeer`, the peer's branch, ranked in `statusFromPush` below `ConflictsWithMain` and
above `VerdictReviewMe` — the state it suppresses, so a ticket whose checks would otherwise read
`review_me` reads `blocked` instead, naming the peer. It rides on `RunFact`, not `Facts`: `Status`
resolves a run's own disposition before it ever reaches the `ConflictedBase` switch, and a ticket
this gate holds has always run and always has an open pull request, so a fact placed on `Facts`
would never be read.

**Stacked branches are out of scope.** A candidate must be `main`-based; a stacked branch's own
base is a sibling ticket's branch; `conflictedBase` already owns what happens when that base
carries a conflict.

## Consequences

Only one of a conflicting pair ever shows `review_me` at a time, so a human merges the lower ref,
watches the higher one's own conflict-with-main gate catch the now-real conflict on the next tick,
and resolves it once instead of at a surprise merge.

The peer sweep is pairwise: every tracked branch in a repo is checked against every other tracked
branch, not only open, `main`-based ones, which costs more `git merge-tree` calls per tick than
`ConflictsWithBase`'s one-per-branch read costs. Filtering the sweep itself to open, `main`-based
candidates is a lazy win available whenever that cost is measured and found to matter; it does not
change what any row shows, since `conflictingPeerHold` already ignores every pair it does not ask
for.

The refinement that skips a peer which is itself held is a separate ticket: today, a three-way
conflicting chain holds every ticket but the lowest ref behind the next-lowest, even where the
next-lowest is itself held behind another.
