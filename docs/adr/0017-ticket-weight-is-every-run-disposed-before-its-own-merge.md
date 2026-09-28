# 17. A ticket's weight is every run disposed before its own merge, converted to a percentage of the week

**Date:** 2026-09-28 · **Status:** accepted

## Context

`GET /insights` (docs/adr/0015) plotted tokens per day, fleet-wide, because nothing yet existed
that could attribute a run's cost to an outcome. Two things changed that. `pr_merged` (plan Phase
1, `internal/cc/merge_events.go`) stamps every ticket with GitHub's own merge time the first tick
that observes it. `usage.Factor` (CC-313) fits a trailing least-squares relationship between a
dollar of cost and a point of subscription utilization, per window — the same number
`deriveGauge` already turns cc's own trailing cost into a masthead percentage. Together they answer
the plan's own Phase 3 question — "is this getting better or worse, and does spend per ticket drop
after a change?" — which a daily token count could not, since a slow week and a busy week look
identical on that axis.

Two decisions had to be made to turn "runs" and "a merge time" into one number per ticket.

**What counts as a ticket's weight.** A ticket's own runs span every kind — `agent`, `resolve`,
`follow_up` — and a launch, an unblock, an automated retry and a person clicking follow-up are all
the same spend by a different name. Weighing only the first `agent` run would make automated
retries invisible, and the run-metrics grain is already `run -> ticket` regardless of kind
(docs/adr/0015's own decision). So the weight is every run belonging to the ticket, summed, no kind
excluded — the acceptance criterion is stated as a single case (an agent run, a resolve and a
follow-up) precisely because that is the case a kind filter would get wrong.

**The merge cutoff, and why `withdrawn_at` cannot stand in for it.** A run disposed after the merge
did not buy the merged code — the PR was already in. Excluding it means every ticket needs its own
cutoff, not the query's own `since`/`until`, so the cutoff is `pr_merged.at`, per ticket. The
obvious alternative — join spend against `withdrawn_at IS NULL`, the convention every other read in
this codebase follows — is wrong for a reason specific to this feature: `removeWorktree`
(`internal/cc/verbs.go`) withdraws a ticket whether or not it merged, so `withdrawn_at IS NOT NULL`
means "tidied away," not "abandoned." Reading it as abandonment would drop every merged ticket's
spend the moment its worktree is cleaned up, which is routine, not a signal. `pr_merged`'s presence
is the only fact that actually distinguishes a shipped ticket from a wasted one, so the merged
query keys off it directly and ignores `withdrawn_at` entirely; the waste query inverts that —
withdrawn, and no `pr_merged` event, ever.

**Percent of the week, not dollars.** `cost_usd` is nullable and provider-computed (docs/adr/0015);
plotting it directly would make the axis the first thing to break under a second stdout dialect,
and a raw dollar figure says nothing about how much of the account's actual subscription a ticket
consumed relative to what else ran that week. `usage.Factor` already answers that question for the
masthead gauge, so `handleInsights` applies the same trailing seven-day factor to each ticket's own
weight (`pctWeek`, `internal/cc/insights.go`) rather than inventing a second conversion.

## Decision

**`MergedTicketSpend` and `WithdrawnTicketWaste`** (`internal/cc/queries/insights.sql`) replace
`RunInsights`'s daily buckets outright — the plan calls this a replacement, not an addition, and a
tokens-per-day panel nobody reads is dead weight, not a fallback. `MergedTicketSpend` returns one
row per ticket with a `pr_merged` event in `[since, until]`, kind-split, summing every run whose
`ended_at` precedes that event's own timestamp. `WithdrawnTicketWaste` sums every disposed run
belonging to a ticket that has `withdrawn_at` set and no `pr_merged` event, in the same range keyed
by its own withdrawal time.

`handleInsights` turns both into `pct_week` via one `Factor` — `FitFactors`'s seven-day window, read
once per request rather than per ticket, matching `deriveGauge`'s own use of a single trailing fit
across everything it scores in one render. A trailing fit below `usage.MinSamples` is simply absent
from the map, and a zero-value `Result` reads as `Factor: 0` — every point's `pct_week` is 0 until
the fit calibrates, the same "nothing to show yet" the masthead gauge already reads as
`Calibrating`, without a second flag this page has no use for.

## Consequences

`GET /insights.json` now returns `points` (one per merged ticket) and `waste_pct_week` in place of
`buckets`; nothing else in the codebase read the old shape (`RunInsights`, `InsightsBucket` and
their queries are deleted, not deprecated). `cc-insights` (`web/src/insights.tsx`) draws a dot per
point, a rolling median over the trailing ten (`rollingMedian`, `web/src/charts.tsx`), and the waste
total as a dashed reference line on the same percentage axis, so a ticket's spend and the fleet's
waste read on one scale.

A ticket merged and later withdrawn by `removeWorktree`'s own tidy-up still appears as a spend
point, correctly, because the query never looks at `withdrawn_at`. A ticket withdrawn without ever
merging appears in no point at all, only in the one waste number — the acceptance criterion this
ADR exists to make deliberate rather than accidental.
