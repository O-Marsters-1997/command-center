# 22. Account usage arrives through the statusline

**Date:** 2026-11-16 · **Status:** accepted

## Context

The rail and Insights meters read rate-limit windows from `utilization_readings`, and the only
writer was the loop at run disposition, parsing `rate_limit_event` lines from cc's own run logs.
Usage from interactive Claude sessions outside cc was never read, so with no finished cc runs the
meters showed a stale or empty account.

Claude Code passes its statusline command a JSON payload on stdin. In 2.1.325 it carries
`rate_limits.five_hour` and `rate_limits.seven_day`, each with `used_percentage` (0 to 100) and
`resets_at` (Unix seconds), taken from the account's real usage.

## Decision

- `cc record-usage` reads one statusline payload on stdin, turns each window into an
  `agentlog.Reading` stamped with the invocation time, and writes it through
  `Store.RecordReadings`. That already dedupes on `(at, window)`, so it sits beside the loop's
  run-disposition writer. The invocation time never repeats, so it first drops a reading whose
  utilization and reset time equal the latest stored one, keeping the table to real changes.
- The user wires it into their statusline command. cc installs nothing.
- It swallows its own errors, logs them to stderr and exits 0, so it never breaks the statusline.
  It gives up after three seconds.
- cc polls nothing. Readings arrive only while a Claude session runs, and the meters keep showing
  the last one, aged, between sessions.
- A payload without `rate_limits` writes nothing. A loop-spawned probe run would cost tokens per
  probe and is not built.

## Consequences

The gauges track real account usage whenever any Claude session is open. Each invocation opens the
database and runs the migration check, which is the cost of a statusline refresh. A CLI release
that renames the payload fields stops the feed silently except for the stderr line, so
`ParseStatusline` is the one place to follow it.
