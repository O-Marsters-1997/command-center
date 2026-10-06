# 13. Fleet runs split at durable handoffs; read-heavy work goes to foreground subagents

**Date:** 2026-09-27 · **Status:** accepted

## Context

Each ticket got one `claude -p "/implement <url>"` run that explores, implements, tests and reviews in
one context. A long context costs twice: every token is re-read from cache on every later request and
it degrades output (256k by the fourth request in the one real run). A subagent is not free either: it
pays its own opening context (53k there) at the cache-write rate and its summary enters the parent. So
delegation pays for large work read once and early, and loses for small or late work.

The system prompt told agents to do everything themselves, written against background subagents whose
results never reach a single-shot session. Foreground subagents return within the turn.

## Decision

Split a fleet into runs at boundaries that leave a file behind:

- **Explore**: one per launch, writing a brief of at most about 3k tokens (file map, conventions,
  test commands, a section per ticket), written once. Later-wave tickets also get the parent's diff as
  a worked example.
- **Implement**: one per ticket, prompted with the brief's path and the fixed text first so runs share
  a cached opening.
- **Review**: one per PR in a fresh context. It applies its own findings (`/code-review --fix`) and
  pushes, or when findings exceed a size bound writes them to the PR and cc queues a follow-up.

`runs.kind` gains `explore` and `review`. Inside an implement run, read-heavy low-output work (full
test suite, lint output, searching beyond the brief) goes to a foreground Haiku subagent returning a
digest of about 1k tokens, and anything that edits code stays in the main thread. The prompt allows
foreground and still forbids background subagents.

No context ceiling yet. After about twenty merged tickets, check whether peak context in
`run_requests` (ADR 10) predicts the guardrails in ADR 12, and pick a ceiling only if it does.

Rejected: one run per ticket with the agent orchestrating (reviewer shares the author's context), and
a run per phase (five opening contexts per ticket).

## Consequences

A launch pays an explore run first, shared across its tickets. The review size bound must be stated
concretely in the prompt, or reviewers fix or escalate everything.
