# 18. Run metrics are captured per API request

**Date:** 2026-09-27 · **Status:** accepted

## Context

ADR 15 captures nine totals per run at disposition, because `pruneRunLogs` deletes the log and
anything not captured is lost. The totals turned out to be the wrong grain for the next questions.

- `tokens_in` sums input, cache write and cache read. In the one real run we have
  (`internal/agentlog/testdata/run27.jsonl`), cache reads are about 98% of all tokens. Context
  growth, the thing that degrades output quality over a long run, cannot be seen inside that sum.
- In the same run the main thread's context is 53k tokens on its first request and 256k by its
  fourth. That curve exists only per request.
- Foreground subagents appear on stdout as `assistant` messages whose `parent_tool_use_id` is the
  spawning tool call, each with its own `usage`. Verified on Claude Code 2.1.283. The parser
  ignores that field today, so subagent tokens are silently folded into the main thread.

## Decision

**`RecordDisposition` also writes one `run_requests` row per unique `request_id` in the run's
log.** It uses the same parse and the same transaction as the run's totals.

Each row holds `run_id`, `seq`, `request_id`, `at`, `model`, `thread` (NULL for the main thread,
otherwise the spawning `tool_use` id), `input_tokens`, `cache_create_tokens`, `cache_read_tokens`,
`output_tokens`, `context_tokens` (input plus cache write plus cache read) and `tool` (the first
`tool_use` name in the response, if any).

The run-level columns from ADR 15 stay, as rollups of these rows, so existing readers are untouched.

The per-request grain is chosen because ADR 15's own reasoning applies again. A question nobody
has asked yet cannot be answered once the log is pruned. Storing per request costs about a hundred
small rows per run and lets a later question about context curves, subagent share or tool
behaviour be answered from runs that have already happened.

OTel egress (§12 of the design doc) stays deferred, for the reasons ADR 15 gives.

## Consequences

The parser learns `parent_tool_use_id`. Deduplication stays keyed on `request_id`, which already
handles a subagent's blocks landing between two of its parent's.

Backfill works as it does in ADR 15, over logs that still resolve.
