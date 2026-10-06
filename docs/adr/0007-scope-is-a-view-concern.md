# 7. Scope is a view concern, not an instance boundary

**Date:** 2026-09-19 · **Status:** accepted

## Context

Running `cc` per repo, from inside each, would break three things. `plan.Unlocked` refuses a blocker
missing from the loaded tickets, so cross-repo consumers would park permanently (the case ADR 3 kept
the draft gate for). The schema has no instance column, so instances share or split the DAG. And
`max_agents` is the only cap on concurrent agents, so N instances give N times the budget.

"Show me this repo's work" is a question about what renders.

## Decision

One daemon, one database, every repo. A **scope** is a query parameter the board reads and the loop
ignores. `?feature=` is primary (`CONTEXT.md`). `?repo=` is secondary, and `cc open` sets it from the
cwd's git origin.

**A scope admits a group whole.** Filter groups after `groupRows`, keeping any whose root or child
matches. Filtering rows would leave the root missing and render a zero-value row, and filtering in
`store.Tickets` would reproduce the `plan.Unlocked` refusal.

## Consequences

A scoped board shows out-of-scope blockers, named with their own feature or repo. The band counts
scoped rows and scope controls are plain links (`band.tmpl` is outside the five-second swap). Live
agents stays global, bounded by `max_agents`.

A second instance on another `CC_DATA_DIR` is unsupported; the flock will not notice. Serving repos
that must not see each other needs an instance column, a per-instance flock and a config split.
