# 12. Spend is a share of the subscription windows, and a ticket's spend closes at merge

**Date:** 2026-09-27 · **Status:** accepted

## Context

Agents run on a subscription. `total_cost_usd` is an API-equivalent figure nobody pays. The binding
limits are rolling five-hour and seven-day windows, published only roughly. `claude -p
--output-format stream-json` emits a `rate_limit_event` per request with `utilization` (0 to 1,
whole-percent steps) for each window, covering the whole account, so no field says what one session
used. Every local session also writes per-request `usage` to `~/.claude/projects/**`.

The thing worth making cheaper is getting a ticket to a merged PR, not any one run: a cheap implement
that needs two follow-ups and a resolve is not cheap. A paired replay harness would attribute changes
cleanly but is a second pipeline and spends real window share per experiment. `GET /insights`
plotted tokens per day, which cannot answer whether spend per ticket drops after a change.

## Decision

**Spend is a percentage of each window, not dollars.** cc stores every `rate_limit_event` as a
reading. Each request is weighted by its tokens at API rates (input 1×, cache write 1.25×, cache read
0.1×, output 5×), per request because interval samples cut through runs. A fitted factor converts
weight to percent, from the intervals between readings, weighting each by every local session
transcript in it (interactive use is counted, not excluded). Readings with a large residual are
flagged contaminated and dropped: usage from claude.ai, mobile or other machines. A run's share is
its weight times the factor. Raw per-run deltas were rejected (a 0.4% run reads as 0% or 1%), as was
dividing by Anthropic's published hours.

**A ticket's spend is every run it needed, every kind, until its PR merged.** The cutoff is
`pr_merged.at` per ticket. `withdrawn_at` cannot stand in for it, since `removeWorktree` withdraws
merged tickets too. `MergedTicketSpend` returns one row per merged ticket, and `WithdrawnTicketWaste`
sums disposed runs of tickets withdrawn with no `pr_merged`, reported separately and never averaged
in. Both replace the daily buckets.

`handleInsights` converts weights with one trailing seven-day `Factor` per request (`pctWeek`), as
`deriveGauge` does; below `usage.MinSamples` every value is 0, the masthead's `Calibrating`. Merged
tickets plot by merge date as a dot each with a rolling median over the trailing ten, and waste as a
dashed line. Two guardrails sit beside spend: first push passed CI, and hand churn (lines changed by
commits after the last run was disposed).

## Consequences

cc reads Claude Code's transcripts, which are pruned on a retention schedule, so each interval is
fitted once when its closing reading lands. The factor drifts when Anthropic changes limits;
refitting over a trailing window absorbs it. `Spend` in `CONTEXT.md` is this percentage, and
`cost_usd` is only the weight.

No experiment harness exists: a step in the median needs roughly ten to fifteen merged tickets per
side, and attribution is by date only since nothing records when a change was made.
`GET /insights.json` returns `points` and `waste_pct_week` in place of `buckets`.
