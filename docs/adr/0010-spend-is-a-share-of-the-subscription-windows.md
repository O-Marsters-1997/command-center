# 10. Spend is a share of the subscription windows, and a ticket's spend closes at merge

**Date:** 2026-09-27 · **Status:** accepted

## Context

Agents run on a subscription. `total_cost_usd` is an API-equivalent figure nobody pays. The binding
limits are rolling five-hour and seven-day windows, published only roughly. `claude -p
--output-format stream-json` emits a `rate_limit_event` per request with `utilization` (0 to 1,
whole-percent steps) for each window, covering the whole account, so no field says what one session
used. Every local session also writes per-request `usage` to `~/.claude/projects/**`.

A run's log is pruned when its ticket is withdrawn, so anything not read from it by then is lost.

The thing worth making cheaper is getting a ticket to a merged PR, not any one run: a cheap implement
that needs two follow-ups and a resolve is not cheap.

## Decision

**Spend is a percentage of each window, never dollars.** cc stores every `rate_limit_event` as a
reading. Each request is weighted by its tokens at API rates (input 1×, cache write 1.25×, cache read
0.1×, output 5×), per request because interval samples cut through runs. A fitted factor converts
weight to percent, from the intervals between readings, weighting each by every local session
transcript in it (interactive use is counted, not excluded). Readings with a large residual are
flagged contaminated and dropped: usage from claude.ai, mobile or other machines. A run's share is its
weight times the factor. Raw per-run deltas were rejected (a 0.4% run reads as 0% or 1%), as was
dividing by Anthropic's published hours.

**A run's weight is captured once, at disposition.** `RecordDisposition` parses the run's own stdout
and stores its weight as `cost_usd` in the transaction that records `outcome`, NULL when never
measured. The column is frozen once written, since a pruned log cannot be re-parsed. It is
`double precision`, not `numeric`: the weight is a float estimate that the factor fit consumes as
`float64`, so exact decimals would buy precision the source never had. Live runs'
spend is re-read from their logs per render.

**A ticket's spend is every run it needed, every kind, until its PR merged.** The cutoff is
`pr_merged.at` per ticket; `withdrawn_at` cannot stand in for it, since removing a worktree withdraws
merged tickets too. `MergedTicketSpend` returns one row per merged ticket, and `WithdrawnTicketWaste`
sums disposed runs of tickets withdrawn with no `pr_merged`, reported separately and never averaged
in.

Insights convert weights with one trailing seven-day factor per request (`spend.PctWeek`). Below
`spend.MinSamples` every value is 0, the masthead's `Calibrating`. Merged tickets plot by merge date as
a dot each with a rolling median over the trailing ten, and waste as a dashed line.

## Consequences

cc reads Claude Code's transcripts, which are pruned on a retention schedule, so each interval is
fitted once when its closing reading lands. The factor drifts when Anthropic changes limits; refitting
over a trailing window absorbs it. `Spend` in `CONTEXT.md` is this percentage, and `cost_usd` is only
the weight: no page shows it as money.

A step in the median needs roughly ten to fifteen merged tickets per side, and attribution is by date
only.
