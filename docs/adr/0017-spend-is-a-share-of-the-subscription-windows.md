# 17. Spend is a share of the subscription windows, not dollars

**Date:** 2026-09-27 · **Status:** accepted

## Context

Agents run on a Claude subscription, not a metered API key. The dollar figure on a run's `result`
line (`total_cost_usd`) is what the same tokens would cost on the API. Nobody pays that. The limits
that do bind are two rolling windows, five hours and seven days, and Anthropic publishes them only
roughly, as hours of Sonnet.

`claude -p --output-format stream-json` emits a `rate_limit_event` after every API request. It
carries `unifiedWindows.five_hour.utilization` and `unifiedWindows.seven_day.utilization`, each a
fraction from 0 to 1 in whole-percent steps. The figure covers the whole account: interactive
sessions, claude.ai, and other machines all move it. No field says what share one session used.

Every local session, interactive ones included, writes its per-request `usage` to
`~/.claude/projects/<project>/<session>.jsonl`, and subagents write to a `subagents/` directory
beside it.

## Decision

**A run's spend is reported as a percentage of each window.** Dollars stay internal.

- cc stores every `rate_limit_event` a run emits as a utilization reading: time, window,
  utilization, `resetsAt`.
- Each request gets a weight: its tokens priced at API rates for its model (input 1×, cache write
  1.25×, cache read 0.1×, output 5×) from a small price table. It is checked against a run's
  `total_cost_usd`, which prices the same tokens the same way. Weights are per request because
  interval samples cut through runs, and transcripts carry no cost.
- A fitted factor converts weight into percent of each window. Its samples are the intervals
  between consecutive readings. For each interval, the weight is the sum across every local
  session transcript under `~/.claude/projects` that falls in the interval, not only cc's runs.
  That counts interactive use instead of excluding it.
- Readings in the same window whose residual is large are flagged as contaminated and left out of
  the fit. The contamination is usage that never reaches a local transcript: claude.ai, mobile,
  other machines.
- A run's share is its own weight times the factor.

Raw deltas per run were rejected. At whole-percent steps, a run that uses 0.4% reads as 0% or 1%,
and anything else happening on the account at the same time lands on the run. Dividing by
Anthropic's published hours was rejected because the CLI already hands over the real signal.

## Consequences

cc reads files outside its own repo and its own run logs, under `~/.claude/projects`. Claude Code
prunes those transcripts after its own retention period, so the fit consumes each interval once,
when the reading that closes it lands, and never relies on re-reading old transcripts.

The factor drifts whenever Anthropic changes the limits. Refitting continuously over a trailing
window absorbs that, and a step in the factor is itself worth showing.

`Spend` in `CONTEXT.md` now means this percentage. The API-equivalent dollar figure is kept in
`cost_usd` as the weight and is not displayed as spend.
