# Plan: fleet orchestration

> Source: the grilling session of 2026-09-27, and ADR 20. Lands after `plans/ticket-spend.md`
> Phase 3, so its effect shows up as a step in the spend median.

## Technical design decisions

**Run kinds:** `explore` and `review` join `agent`, `resolve` and `follow_up` (`loop.go`
constants). Every kind goes through `spawnRun`, so every kind gets metrics capture for free.

**max_turns is per-kind, not one repo-wide value.** A real implement run ran 140 turns before its
first review pass even started. Splitting the run does nothing for that if the implement kind can
still run unbounded; review's cap in particular should be deliberately low, to force the size-bound
decision in ADR 20's review prompt rather than let a review quietly turn into a slow implement.

**Schema** (migration after ticket-spend's):

- `runs.ticket_id` becomes nullable.
- `runs.launch_id` is added (nullable, FK `launches`), with a check that exactly one of the two is
  set.
- An explore run belongs to a launch.
- Ticket spend apportions a launch's explore spend equally across its `launch_members`.

**Explore run:**

- It is inserted by `insertLaunch` (`launch.go`), once per launch.
- It runs in a detached worktree at the launch's base.
- Its prompt comes from `plan.ComposeExplore(tickets)`: write the brief to a given path, keep it to
  about 3k tokens, with sections for file map, conventions, seams, test commands and one per ticket.
- The brief lives at `<runs>/launch-<id>/brief.md`, outside any ticket worktree.
- `launchEligible` holds a launch's tickets until the explore run is disposed and the brief exists.
- If the explore run fails, the tickets launch without a brief. It does not block the launch.
- It runs on Haiku: writing a brief is reading and summarizing, not the judgment work implement and
  review need. `agent_command` gains a `{model}` placeholder alongside `{agents}` so a run's kind
  picks its own model at spawn.

**Implement prompt:** `/implement <url>` must stay first, because a slash command has to lead the
prompt. The prompt then adds:

- `## Brief` with the brief's path;
- for a ticket whose blocker has a PR, `## Worked example` naming the blocker's branch and the
  `git diff` command to read it;
- `## Ticket` with the body.

The shared cached prefix is the system prompt and tool definitions, which are already identical
across runs. No reordering is needed.

**System prompt** (`settings.go`, `agent` kind):

- Replace "do any work, including code review, yourself" with two rules:
  - foreground subagents are allowed, background ones are not;
  - skip code review, because a review run follows.
- Add the delegation rule: work that reads a lot and returns little goes to the `digest` subagent.
- State the comment rule (`~/.claude/rules/comments.md`) inline rather than leaving it to
  `clean-comments` to catch after the fact: real runs show `check-staged-comments.sh` bouncing a
  commit and forcing a whole rework pass.
- Name the shell as zsh and point at `rg` over `grep --include`: the same bash-glob `Exit code 1`
  and `Unknown skill: gh-desc`/`unslop` failures show up verbatim across unrelated runs, so this is
  environment drift, not per-ticket bad luck. Fixing it here removes it from every run at once.

**Digest subagent:**

- cc writes an `agents.json` beside the run's settings: `digest`, model `haiku`, tools `Read`,
  `Grep`, `Glob`, `Bash`, instructed to return at most about 1k tokens.
- `agent_command` gains an `{agents}` placeholder passed as `--agents`.
- The settings deny list still applies to it.

**Review run:**

- It is spawned by the tick when an `agent` or `follow_up` run disposes successfully and the
  mechanical push has opened or updated the PR.
- One review per push.
- Its prompt comes from `plan.ComposeReview`: run `/code-review --fix` on the branch against its
  base, commit the fixes, and do not push.
- Findings over the bound go to `<runs>/<id>.findings.md` instead of being fixed.
- The bound is stated in the prompt: any finding that changes a public interface, moves code across
  packages, or needs more than about 50 changed lines.
- On disposition:
  - commits are pushed by the existing mechanical push;
  - a non-empty findings file queues a `follow_up` with the findings as its text
    (`ComposeFollowUp`).
- A review of a follow-up's push never escalates again. Its findings go to the PR as a comment.

---

## Phase 1: Digest subagent

**User stories:** keep the implement run's main context small without changing the pipeline.

### What to build

- The `agents.json` writer and the `{agents}` placeholder.
- The system prompt rewrite (foreground allowed, delegation rule). Review stays in-run until
  Phase 3.

### Acceptance criteria

- [ ] `settings_test.go` asserts the new text and that background subagents are still forbidden.
- [ ] A spawned run's argv carries `--agents <path>`, and the file defines `digest` on Haiku.
- [ ] `run_requests` for a real run shows the subagent's requests under their own thread.
- [ ] A sampled run's log has no `Unknown skill` error and no bash-glob `Exit code 1` from the
      rewritten prompt.
- [ ] A sampled run's diff never trips `check-staged-comments.sh`.

---

## Phase 2: Explore run and brief

**User stories:** explore once per launch instead of once per ticket.

### What to build

- The schema change.
- `ComposeExplore`, the insertion in `insertLaunch`, and the gate in `launchEligible`.
- The `## Brief` section in the implement prompt.
- The explore run shows on the board under its launch; the minimum is a line in the masthead while
  it runs.

### Acceptance criteria

- [ ] A launch of three tickets spawns one explore run, then three implement runs, each prompt
      naming the same brief path.
- [ ] A failed explore run releases the tickets without a brief.
- [ ] The ticket spend rollup includes a third of the explore run for each of three members.
- [ ] The explore run's argv carries `--model haiku`.

---

## Phase 3: Review run

**User stories:** review in a fresh context; the author doesn't review its own work.

### What to build

- `ComposeReview` and the spawn trigger on disposition.
- The findings file, and the escalation to `follow_up`.
- Removal of in-run review from the system prompt.

### Acceptance criteria

- [ ] A successful implement run is followed by exactly one review run on the same worktree.
- [ ] Review commits are pushed; an empty findings file queues nothing.
- [ ] A non-empty findings file queues a follow-up whose instruction is the findings.
- [ ] A review after a follow-up never queues a second follow-up.
- [ ] Review's spawned `--max-turns` is lower than implement's.

---

## Phase 4: Parent diff for later waves

**User stories:** later waves learn from the merged or stacked parent instead of re-exploring.

### What to build

- The `## Worked example` section for tickets whose blocker has an open or merged PR, built from
  `plan.Unlocked`'s base choice.

### Acceptance criteria

- [ ] A stacked ticket's prompt names the blocker's branch and the diff command.
- [ ] A ticket with no blocker gets no section.
- [ ] The prompt hash changes only when the section's content changes.

## Constraints

- `/implement` lives in `~/.agents/skills`, outside this repo. Its own "review with /code-review"
  line conflicts with Phase 3 until it is edited there. The system prompt override covers it in
  the meantime.
- Haiku for the digest subagent is a narrow exception to running Sonnet everywhere (ADR 20).
