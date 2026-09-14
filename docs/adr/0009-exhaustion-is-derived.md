# 9. Exhaustion is derived, never stored

**Date:** 2026-09-13 · **Status:** accepted

## Context

Authorising a second feature today interleaves it with the first. `LaunchPlan`
(`internal/plan/launch.go:24`) walks its candidates in input order and takes the first eligible
ones until `max_agents` runs out. That input order comes from `Store.Tickets`, which is
`ORDER BY url`, so the winner is decided by a lexicographic sort on an issue URL. With
`max_agents = 3` in `cc/config.toml`, whichever feature's URLs happen to sort earlier takes every
freed slot until it drains, and the other feature's tickets sit at `queued`, reason
`waiting for a slot`, for as long as that takes. Nothing on the board says one feature is holding
the fleet.

The fix is to order candidates by launch rather than by URL, so authorised features run one at a
time and a night's work can be queued in advance. That needs a notion of a launch having nothing
left to start, and this is where the design contradicted itself. §8 lists the schema as
`launches … state (active/done/cancelled)` and §4b says a launch is "`active` until every member is
terminal or the user cancels it". No code has ever written `done`. `CancelLaunchesFor`
(`internal/cc/launch.go:211`) holds the only `UPDATE launches` in the repo. Launches are created
active and stay active for good.

So the transition had to be either written or removed, and that is a real decision rather than a
tidy-up, because the two options fail differently.

## Decision

**A slice is exhausted once every ticket it covers has a pull request, and the tick computes that
fresh each pass.** Exhaustion is never written down. `launches.state` keeps `cancelled` and nothing
else.

`LaunchPlan` orders its candidates by launch and fills slots from the oldest launch that is not
exhausted. Because exhaustion is recomputed every tick, the ordering corrects itself without a
migration, a promotion step, or a repair path.

The split follows §8's existing rule that status is derived and there is no status column to go
stale, and it draws the line in the place that rule implies. Cancelling is a decision a human made
that nothing in the world records, so it is stored. Exhaustion is a fact about pull requests that
the tick already reads, so it is derived. One column holding both kinds of fact was the mistake in
the original schema line.

Exhaustion deliberately means the agents are finished, not that the work is. Every member has a PR;
none of them is merged, because the app never merges (invariant 2). Defining it as "merged" instead
would read better but strand the fleet overnight: the tickets reach `review me`, wait for a human,
and the next feature never starts. A definition that only advances while somebody is awake is
useless to the thing this is for.

## Consequences

Storing `done` would have gone stale in a way that costs a night. A PR closed unmerged at 03:00
leaves its launch still reading `done`, its ticket needing work, and nothing to reopen it before
morning. Derived exhaustion un-exhausts that launch on the next tick and it reclaims its slots.
That is the whole argument, and it is worth more than the legibility that a stored column buys.

The design doc is now wrong in two places. §8's schema line still lists `done`, and §4b still says a
launch is active until every member is terminal. Both describe a transition that will never be
written. Read this ADR as superseding them, and do not "fix" the missing `done` by adding it.

Sequencing is strict but work-conserving in one case worth knowing about. A launch counts as
exhausted the moment its last PR is up, which is usually well before its tickets are merged, so the
next feature starts while the previous one sits in review. Features run one at a time in the sense
that matters for spawning agents, not in the sense that one is fully closed out before the next
begins. Two features can have open PRs at once and routinely will.

Nothing a human does by hand is gated on this. `applyReRunIntents` is explicitly not gated by
unlock, authorisation or a prompt-hash match (`internal/cc/verbs.go:134`), and `refresh` and the
other verbs are per-row intents the loop consumes without consulting launch membership. Exhaustion
stops the tick starting new work in a launch and touches nothing else.

The cost is one extra column in `ActiveLaunchHashes` (`internal/cc/runs.go:279`), which already
joins `launches`, plus a launch id on `LaunchCandidate` and a sort. `internal/plan` stays pure and
table-testable. There is no schema change.

What this does not give you is a queue you can see or rearrange. The order is `launches.id`, which
is insertion order rather than anything anybody chose, and there is nothing to show on the board or
change before bed. Adding that later means storing a position, which this ADR's own line permits: a
priority you set is a decision nothing can reconstruct, so it belongs in a column, exactly as
cancelling does. The sort key has one owner in `LaunchPlan`, so it is a small change when it comes.
Exhaustion stays derived either way.
