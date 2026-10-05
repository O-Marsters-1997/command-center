# 19. A ticket's spend closes at merge, and is tracked over time rather than tested

**Date:** 2026-09-27 · **Status:** accepted

## Context

The thing worth making cheaper is getting a ticket to a merged pull request, not any single run. A
cheap implement run that needs two follow-ups and a resolve is not cheap. Per-run spend is blind
to that.

A paired replay harness was considered: re-run a fixed set of merged tickets from their base
commits under a baseline and a variant, then score them with the merged PR's own tests. It would
attribute a change cleanly, but it is a second pipeline to build and maintain, and each experiment
spends a real share of the weekly window.

## Decision

**A ticket's spend is the sum over every run it needed, every kind included, until GitHub reports
its pull request merged.**

- Merge state is the one honest merge test in a squash-only repo (see `CLAUDE.md`), and cc already
  reads it.
- Spend on a withdrawn ticket is reported separately, as waste. It is not averaged into merged
  tickets.
- Merged tickets are plotted over time by merge date, as a dot per ticket with a rolling median. A
  change to prompts, orchestration or model shows up as a step in the median.
- Two guardrails sit beside spend, so a cheaper but worse change stays visible:
  - whether the first push passed CI;
  - hand churn, meaning lines changed on the branch by commits that land after the ticket's last
    run was disposed.

No experiment harness is built. Ticket size varies and runs are noisy, so a step in the median
needs roughly ten to fifteen merged tickets on each side before it is readable. That lag is
accepted in exchange for building nothing beyond the rollup.

## Consequences

Attribution is by date only. If two changes land in the same week they cannot be separated, and
nothing in cc records when a change was made.
