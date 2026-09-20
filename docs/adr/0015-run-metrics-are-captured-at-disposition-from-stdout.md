# 15. Run metrics are captured at disposition, from the agent's own stdout

**Date:** 2026-09-20 · **Status:** accepted

## Context

`internal/agentlog.ParseMetrics` (issue #275) can turn one run's log into its settled totals —
tokens, turns, duration, cost, tool calls and failures, the model — but until now nothing ever
called it against a stored row. The only place spend appears today is `internal/cc/spend.go`'s
`Accumulator`, read live off a running agent's log for the board and the band. That cache is
per-request and per-path; it answers "what is CC-42 spending right now," never "what has this
fleet spent."

Two decisions had to be made to answer the second question, and both cut against something this
design already named.

**Where the read happens.** §12 of `docs/designs/command-centre-design.md` defers "Slack / OTel /
Datadog egress" until "a localhost page is not enough." That is a real trigger, and it has not
fired — there is still no insights page reading these columns. Observability is re-entering ahead
of its own named trigger, for a narrower reason: `pruneRunLogs` (`internal/cc/verbs.go`) deletes
`runs/<id>.jsonl` the moment a ticket is withdrawn, because the JSONL is disposable working data,
never the fleet's own record. Once a log is pruned, no later pass can ever recover what it said.
Waiting for an insights page to justify reading these logs would mean losing every run disposed of
before that page existed.

**How the read happens.** §12 also names the alternative directly: push to OTel as each run ends,
rather than read its own stdout after the fact. OTel egress means a collector, a schema, and a
second system of record for a number Postgres already has room for in the row that names the run.
Reading the log this instance already wrote, once, at the moment the run's own disposition is
decided, needs neither.

## Decision

**`RecordDisposition` parses the run's log and writes nine metrics columns in the same UPDATE that
writes `outcome` and `exit_code`.** The parser is injected as `MetricsParser`, a func type matching
`ObserveFunc`, `CheckoutFunc` and `TrackerSource` rather than a fifth interface, defaulting to
`agentlog.ParseMetrics` and overridable per instance via `WithMetricsParser` — the seam a test uses
to drive `disposeRun` without a real log file on disk.

`tokens_in`, `tokens_out`, `turns`, `duration_ms`, `cost_usd`, `tool_calls`, `tool_failures`,
`model` and `metrics_settled` are all nullable, and NULL and zero mean different things throughout.
A `cut_failed` run (no worktree, no process, no log) and a spawn failure (no `RecordSpawn`, so no
`log_path` in the row) both leave every column NULL: there was nothing to parse. A killed run's log
exists but never reached a `result` line; `ParseMetrics` sums what it saw and reports
`Settled: false`, and that partial lands as real numbers with `metrics_settled = false` — a
0-token run and a never-measured run must not read the same.

**The field set is frozen the moment this migration merges.** `pruneRunLogs` is deliberately
unchanged — the JSONL stays exactly as disposable as before. That means a column not captured
here is gone forever the moment its ticket is withdrawn; there is no later log to re-parse it from.
`tool_calls` and `tool_failures` ship in this same migration despite no panel reading either one
yet, because the alternative is adding them in a second migration after the first batch of logs
that could have answered them is already deleted.

**Insights read every disposed run, `tickets.withdrawn_at` included.** A run's cost was spent
whether or not its ticket survives to be looked at again. This is stated explicitly because it
will look like an oversight otherwise: the instinct is to join spend against live tickets and
drop what a `WithdrawTicket` cleared out. That instinct is backwards for this feature's own reason
to exist — "what did my agents spend, and how much of it was wasted?" (plan Phase 2) — a withdrawn
ticket is disproportionately where wasted spend lives. Excluding it would undercount the very
number this work was built to surface.

## Consequences

A run's own row is now the fleet's permanent record of what it cost, independent of whether its
log survives. The in-memory `Accumulator` keeps its job for a run with no row yet — a live agent —
and every disposed run reads Postgres instead; no code path reads both for the same run.

Nothing yet reads these nine columns for a person to look at. That gap is deliberate: this ticket
is the write path and the ADR, not the panel. The backfill (`BackfillMetrics`, run once at startup
over every run with a `log_path` that still resolves and NULL metrics) exists so that panel, when
it lands, is not limited to runs disposed of after today.
