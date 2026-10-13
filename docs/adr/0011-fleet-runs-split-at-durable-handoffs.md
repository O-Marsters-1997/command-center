# 11. Fleet runs split at durable handoffs

**Date:** 2026-09-27 · **Status:** accepted

## Context

A long context costs twice: every token is re-read from cache on every later request, and it degrades
output. A subagent is not free either: it pays its own opening context at the cache-write rate and its
summary enters the parent. So delegation pays for large work read once and early, and loses for small
or late work.

## Decision

Split a fleet into runs at boundaries that leave a file behind:

- **Implement**: one per ticket, prompted with the fixed text first so runs share a cached opening.
  Later-wave tickets also get the parent's diff as a worked example. `/implement` runs `code-review`
  and `simplify` inside the run, so no run exists only to review.
- **Follow-up**: one per owner message, resuming the ticket's session (ADR 17).

Inside an implement run, the prompt allows foreground subagents and forbids background ones.

`max_turns` caps implement and follow-up runs.

## Consequences

A review finding the run did not fix reaches the owner as the pull request itself, not as a second run.
