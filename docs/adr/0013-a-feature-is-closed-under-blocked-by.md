# 13. A feature is closed under blocked_by

**Date:** 2026-09-20 · **Status:** accepted

## Context

`tickets.blocked_by` holds issue URLs and nothing else (`internal/cc/store.go:89`). They arrive
from GitHub's own dependency API, one call per issue
(`internal/tracker/github.go:72`), and after the first import they are the app's to edit
(`#215`'s app-owned split). No writer has ever asked which feature a blocker belongs to.
`ImportTickets` refuses exactly one shape of bad tracker data, the ticket claimed by two features
(`FeatureConflictError`, `internal/cc/store.go:140`), and says nothing about a ticket blocked from
outside its own.

So a cross-feature chain is legal data today, and the app already has two places that cope with
it badly. `plan.Unlocked` refuses a blocker missing from the loaded ticket set with `blocked by
<url>, which is not a tracked ticket`, the refusal ADR 11 exists to avoid. `plan.Preview` refuses
a ticket whose blocker sits outside the slice being authorised, and `handleLaunch` queues it
anyway, so it parks at `queued` with an honest reason and nothing to clear it.

Launching by feature rather than by hand-picked ticket makes the question unavoidable. If a
feature can be blocked from outside itself then "launch this feature" has no closed answer: the
modal draws a fragment of a larger graph with edges running off the side of it, and the tickets on
the far end of those edges are the ones that decide whether any of it starts. Every option that
tolerated the dangling edge put the refusal on a per-ticket row, after the authorisation, where
there is nothing to do about it.

## Decision

**A feature contains every unmerged blocker of every ticket it contains.** A blocker whose pull
request has merged is exempt, because `plan.Unlocked` already counts a merged blocker as satisfied
and `repairBlockedBy` already prunes one that has merged and withdrawn
(`internal/cc/store.go:229`).

Both writers enforce it. `ImportTickets` fails the whole transaction, naming the ticket and its
outside blocker, exactly as it already fails on a two-feature ticket, and the refusal lands in the
`import_refused` event and the banner that reads it. `applyEditTicketIntents` applies the same
check to a hand-edited `blocked_by`, so the invariant survives `POST /ticket`. The check is one
function with two callers, not two copies.

Enforcing at launch instead was the near miss. It keeps bad tracker data visible and diagnosable,
which is worth something. It also produces a feature that reads fine on the board and is
permanently unlaunchable, with the explanation arriving only when you try. Refusing at the door
costs diagnosability once; refusing at launch costs it every time.

Not enforcing at all was the cheapest option and the one already in production. It is how issue
#235 happened: an edge nothing revisits, a dependent stuck forever, and a reason that names the
blocker without naming the cause.

## Consequences

A chain deliberately split across two features is now illegal, and importing the downstream
feature fails whole rather than importing what it can. If you work that way, the fix is in the
tracker: put the blocker in the feature, or merge it first. There is no override, deliberately,
because an override is a second code path that only runs when the invariant is already broken.

ADR 11's argument is untouched. `CONTEXT.md` already says a feature spans one or more repos, so a
cross-repo chain inside one feature stays legal, and §6's seam mechanism keeps working. What this
outlaws is a cross-*feature* edge, which is a different thing that happened to look the same.

The refusal is asynchronous. Import goes through an intent the loop applies
(`internal/cc/loop.go:77`), so a closure break is reported on a later tick through
`store.LastImportError`, not in the response to the request that caused it. ADR 14 covers how the
launch modal reads that.

Closure makes one thing simpler that is worth naming. Because a launched set is closed under
blockers, `plan.Preview` can no longer return `Refused` for a blocker outside the slice when the
slice is a whole feature. Every remaining refusal is a per-ticket fact, an active launch or a
conflicted base, independent of what else is selected. The launch modal can therefore compute
every label once, server-side, and toggle locally without a round trip.
