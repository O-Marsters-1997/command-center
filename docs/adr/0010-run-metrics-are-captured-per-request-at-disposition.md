# 10. Run metrics are captured per API request, at disposition, from the agent's own stdout

**Date:** 2026-09-20 · **Status:** accepted

## Context

`pruneRunLogs` deletes `runs/<id>.jsonl` when a ticket is withdrawn, so anything not captured by then
is lost. Nothing called `agentlog.ParseMetrics` against a stored row, and `Accumulator` only reads
live logs. OTel egress (design §12) needs a collector and a second record of a number the database
can hold, whereas reading our own log once, at disposition, needs neither.

Totals were the wrong grain: cache reads are about 98% of tokens, hiding context growth (53k on the
first request, 256k by the fourth in the one real run). Foreground subagents appear on stdout with a
`parent_tool_use_id`, which the parser ignored, folding their tokens into the main thread.

## Decision

`RecordDisposition` parses the run's log and writes, in the transaction that records `outcome` and
`exit_code`:

- **Run totals**: tokens, turns, duration, `cost_usd`, tool calls and failures, model and
  `metrics_settled`, all nullable. NULL means never measured (cut or spawn failure); a killed run
  stores its partial sums with `metrics_settled = false`.
- **One `run_requests` row per `request_id`**: tokens by kind, `context_tokens`, `model`, first
  `tool`, and `thread` (NULL for main, else the spawning `tool_use` id).

The parser is injected as `MetricsParser`, defaulting to `agentlog.ParseMetrics`. Columns are frozen
once merged, since pruned logs cannot be re-parsed. Insights read every disposed run, withdrawn
tickets included: a withdrawn ticket is where waste lives.

## Consequences

A run's row is its permanent cost record; `Accumulator` serves only live runs. Runs disposed
before capture existed stay unmetered. OTel egress stays deferred.
