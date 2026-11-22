# 27. An unstacked dependent waits for its blocker to merge

**Date:** 2026-11-22 · **Status:** accepted

## Context

With `stacking` off, a ticket whose one same-repo blocker had an open pull request unlocked at once
and was cut from `main`. `main` did not hold the blocker's code, so the agent started on a premise
that was false. twenty#33 launched while #32's pull request was open, found no widget union types
on its branch, wrote no code and asked a question. The run cost a slot and the operator's attention
for work that could not start.

An agent can also stop for a false premise when unlock was right, for example a file the ticket
names that does not exist. It then writes `.question`, and the ticket reads as an ordinary asked
state ([ADR 14](0014-glyph-is-the-only-colour-key.md)).

## Decision

**With `stacking` off, a dependent unlocks only when every same-repo blocker has merged, then cuts
from `main`.** With `stacking` on nothing changes: an open blocker unlocks the dependent onto the
blocker's branch.

- `plan.Unlocked` makes the decision; only `plan` derives
  ([ADR 13](0013-only-plan-derives.md)). An unmerged blocker in an unstacked repo reads as waiting
  on that blocker's merge, not as "every blocker has a pull request".
- The reason names the blocker and the merge it waits for, so the rail says why nothing launched.

**A premise stop is its own state, not an asked state.** The agent is told to write
`<run>.premise` instead of `.question` when the work cannot start because something the ticket
assumes is absent. The disposition reads it as `PremiseMissing`, shown with its own glyph word and
reason, with Reply and re-run offered. The glyph word and its mark are chosen in `DESIGN.md`, as
ADR 14 requires of a ninth word.

The agent's system prompt also says that `gh` is unavailable, because the agent cannot run it
([ADR 3](0003-conflicts-resolve-once-and-one-peer-at-a-time.md)), so it stops guessing from a
denied call.

Merging the blocker's branch into the dependent at the cut was rejected: the dependent's pull
request against `main` would carry the blocker's commits until it merges, and the cut can conflict.
Launching anyway with a warning was rejected: the run is wasted every time the warning is true.

## Consequences

An unstacked repo serialises a chain of dependents behind merges, where before they ran in parallel
on a wrong base. Turning `stacking` on is how a repo gets parallel chains. This supersedes the
"open is enough" rule in `docs/designs/command-centre-design.md` for unstacked repos.
