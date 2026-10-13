# 20. History is kept only where something reads it

**Date:** 2026-10-13 · **Status:** accepted

## Context

`events`, `intents`, `pushes`, `runs`, `utilization_readings` and `utilization_intervals` are
append-only, and nothing deleted from them. Some of that history is load-bearing: a ticket's spend
and the insights page join every `run` to its ticket's `pr_merged` event with no date bound
(ADR 10). The rest is never read again: a consumed intent, a reading older than the latest per
window, an interval outside the factor fit's trailing seven days, and the `tick_error` event a
failing tick writes every tick.

## Decision

- Delete consumed `intents` 30 days after `consumed_at`.
- Delete `tick_error` events after 30 days.
- Delete `utilization_readings` after 90 days.
- Delete `utilization_intervals` once `end_at` is 14 days old, twice the fit's trailing window.
- Keep `runs`, `pushes` and every other event kind forever. They are the audit trail that spend and
  insights read.
- The loop runs the deletes in batches, and each one filters on an indexed column.
- A new append-only table states its retention in the migration that creates it.

## Consequences

Spend and insights never lose history, so `runs` and `events` grow with the work done rather than
with time. Widening the fit window means widening the interval retention in the same change.
