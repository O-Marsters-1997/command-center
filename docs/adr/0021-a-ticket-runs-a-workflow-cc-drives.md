# 21. A ticket runs a workflow of steps that cc drives

**Date:** 2026-10-18 · **Status:** accepted

## Context

A ticket used to be one `claude -p "/implement <url>"` run. The skill decided its own order, lived
outside this repo, and left one log whose run phases were cut at skill invocations. Nothing could
say which part of an implementation was slow or expensive, or whether a change to the procedure
helped.

Measured on run 21 (398 requests): the harness costs about 41k tokens at startup, about 0.3% of the
run's weight, and much of it is a cache hit. About 80% of the weight is the main agent re-reading
its own transcript, which grew to about 275k tokens across 276 turns.

Checked on 2026-10-18:

- `anthropic-sdk-go` is a Messages API client authenticated by an API key. It has no file, shell or
  subagent tools, and it bills the Claude API, which Max plans pay for from a separate monthly
  credit pool ($100 or $200, expiring each cycle). It does not draw on the subscription windows.
- The Agent SDK and `claude -p` still draw on the subscription windows. The Agent SDK exists for
  Python and TypeScript only, and runs the same harness as `claude -p`.
- `claude` 2.1.302 runs a session as a named agent with `--agent`, defines agents inline with
  `--agents`, and trims startup with `--tools`, `--strict-mcp-config` and
  `--disable-slash-commands`.

## Decision

**cc drives a ticket through a workflow, and each step is one run.** Order, retries and model
choice are Go, not a prompt.

- A workflow is a Go value in the binary, with each agent step's prompt embedded from a Markdown
  file. Each step names its model, its tools and whether it writes a handoff. An agent step spawns
  through the existing `agent_command`: its prompt joins cc's system prompt file, and `{tools}`
  restricts its tools. Each run records the hash of the workflow it ran under.
- The default workflow is setup (command) → read (Haiku) → implement (Sonnet) → check (command) →
  review (Sonnet, read-only) → fix (Sonnet, only on findings) → pr (Haiku writes the body; cc pushes
  and opens the draft).
- Setup and check are command steps. Each is a run of the repo's `verify_command`: setup proves the
  baseline on the fresh worktree, and check proves the change.
- A step starts from a fresh context: the ticket plus the handoffs before it. Handoffs are files
  at a path cc names outside the git tree.
- The workflow is data in `plan.Rules`. `plan` derives the next step from the ticket's runs and
  their outcomes each tick ([ADR 13](0013-only-plan-derives.md)); no current step is stored.
- A failed check, or review findings, loop through fix for at most two rounds per ticket. After
  that the ticket parks as failed, and its reason names the step.
- A steer, reply or follow-up resumes the latest agent step's session
  ([ADR 17](0017-a-conversation-continues-by-resume-never-by-pipe.md)). The workflow then continues
  from the step after it.
- Today's flow ships as workflow v0, a single implement step that sends `/implement <url>` on the
  full harness. Existing runs backfill as v0 implement, and a launch chooses its workflow.

The Go SDK was rejected because it would move spend to a capped credit pool and mean rebuilding the
tools, permissions and compaction that Claude Code already has. A TypeScript Agent SDK worker was
rejected because it runs the same harness, and its levers are `claude -p` flags, so all it adds
is Node in the runtime path. An LLM orchestrator was rejected because it pays for its own growing
context on every delegation, and its step boundaries hold only as well as the model obeys.

## Consequences

Each extra step costs about one startup, and buys a context that does not carry the steps before
it. The runner's invariants survive: one process per run, stdout is a file, and runs outlive a cc
restart. A step's outcome is only as good as its handoff, so a handoff's shape is part of the
workflow and changes its hash. Spend and its per-step capture follow
[ADR 10](0010-spend-is-a-share-of-the-subscription-windows.md).
