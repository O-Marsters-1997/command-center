# 20. Fleet runs split at durable handoffs; read-heavy work goes to foreground subagents

**Date:** 2026-09-27 · **Status:** accepted

## Context

Every ticket gets one `claude -p "/implement <url>"` run that explores, implements, tests and
reviews in a single context. Two costs pull in opposite directions.

- **A long context costs twice.** Every token that enters the main thread is re-read, from cache,
  on every later request, and a bloated context degrades the output. The one real run reached 256k
  tokens of context by its fourth request.
- **A subagent is not free.** It pays its own opening context, 53k in the same run, at the
  cache-write rate. Its summary then enters the parent.

Delegation therefore pays when the work is large, read once and early in the run, and loses when
the work is small or late.

The agent system prompt (`internal/cc/settings.go`) tells the agent to do all of its work itself.
That line was written against background subagents, whose result never reaches a single-shot
`claude -p` session. Foreground subagents do return within the turn.

## Decision

**cc splits a fleet into runs at boundaries that leave a file behind:**

- **Explore:** one run per launch. It writes a brief of at most about 3k tokens: file map,
  conventions, seams, test commands, and a section per ticket. The brief is written once and never
  rewritten. Tickets in later waves also get the parent ticket's diff as a worked example, instead
  of a second explore.
- **Implement:** one run per ticket, as today, prompted with the brief's path. The fixed text comes
  first so every run in the launch shares a cached opening.
- **Review:** one run per pull request, in a fresh context. It reads the diff and the ticket, then
  applies its own findings (`/code-review --fix`) and pushes. When the findings exceed a size
  bound, meaning a design problem rather than a local fix, it writes them to the PR and cc queues
  a follow-up run instead.

`runs.kind` gains `explore` and `review` beside `agent`, `resolve` and `follow_up`, so spend splits
by kind with no further work.

**Inside an implement run, work that reads a lot and returns little goes to a foreground Haiku
subagent.** That means running the full test suite, reading lint output, and searching beyond the
brief. The subagent returns a digest of at most about 1k tokens. Anything that edits code stays in
the main thread. The system prompt line is rewritten to allow foreground subagents and to keep
forbidding background ones. This is a deliberate exception to running Sonnet everywhere, and it is
cheap to reverse.

**No context ceiling yet.** `run_requests` (ADR 18) records every run's context curve. Once about
twenty tickets have merged under this split, check whether peak context predicts the guardrails in
ADR 19. Choose a ceiling, and a way to enforce it, only if it does.

Two alternatives were rejected:

- **One run per ticket with the agent orchestrating.** The reviewer would share the author's
  context, and phases would be visible only through subagent attribution.
- **A separate run for every phase.** This pays the opening context five times per ticket, and
  every test failure bounces back to a fresh implementer.

## Consequences

A launch now spends on an explore run before any ticket starts. That cost is shared across the
launch's tickets.

The review run's size bound is a judgment the prompt has to state concretely, or reviewers will
either fix everything or escalate everything.
