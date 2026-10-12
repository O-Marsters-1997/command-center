# 11. Fleet runs split at durable handoffs; read-heavy work goes to foreground subagents

**Date:** 2026-09-27 · **Status:** accepted

## Context

A long context costs twice: every token is re-read from cache on every later request, and it degrades
output. A subagent is not free either: it pays its own opening context at the cache-write rate and its
summary enters the parent. So delegation pays for large work read once and early, and loses for small
or late work. A reviewer sharing the author's context reviews with the author's blind spots.

## Decision

Split a fleet into runs at boundaries that leave a file behind:

- **Explore**: one per launch, writing a brief of at most about 3k tokens (file map, conventions,
  test commands, a section per ticket). Later-wave tickets also get the parent's diff as a worked
  example.
- **Implement**: one per ticket, prompted with the brief's path and the fixed text first so runs share
  a cached opening.
- **Review**: one per push, in a fresh context. The next tick after a push sees an implement or
  follow-up run whose tip is the recorded pushed tip with an open PR, and spawns one review run
  (`plan.ComposeReview`). A review is never itself reviewed.

A review applies its own findings and always disposes as `push`, whether or not it committed: a clean
review must not fail the ticket. Findings over the bound (about 50 changed lines, a public interface,
another package) go to `<runs>/<id>.findings.md`. Absorb turns a non-empty file into an intent, and
act spawns a follow-up carrying it. When the run before the review was a follow-up, the findings are
posted to the PR as a comment instead, so escalation happens once.

Inside an implement run, read-heavy low-output work (full test suite, lint output, searching beyond
the brief) goes to a foreground Haiku subagent returning a digest of about 1k tokens, and anything
that edits code stays in the main thread. The prompt allows foreground and forbids background
subagents.

`max_turns` caps implement, follow-up and resolve runs; `review_max_turns` caps review and is always
lower.

## Consequences

A launch pays an explore run first, shared across its tickets. The review size bound must be stated
concretely in the prompt, or reviewers fix or escalate everything.
