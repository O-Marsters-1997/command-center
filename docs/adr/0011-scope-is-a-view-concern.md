# 11. Scope is a view concern, not an instance boundary

**Date:** 2026-09-19 · **Status:** accepted

## Context

The app runs as one process against one config file listing every `[[repo]]`. Working a multirepo
day to day raises the obvious alternative: install `cc` globally, run it from inside `support-app`
or `services`, and have each invocation serve only the repo it was launched in, the way most
developer tools behave.

Three things in the app already assume the opposite.

`plan.Unlocked` builds `byURL` from the tickets that instance loaded and refuses any blocker
missing from it: `blocked by <url>, which is not a tracked ticket`. A repo-scoped instance never
loads its cross-repo blockers, so every consumer in a cross-repo feature parks on that refusal
permanently, with no verb to clear it. This is the case ADR 4 kept the draft gate for.

The schema has no instance column. `tickets.url` is the primary key and `launches`, `launch_members`,
`runs`, `pushes` and `events` all reference it. Two instances against the default `CC_DATABASE_URL`
share one fleet and overwrite each other; giving each its own database splits one DAG across
databases that cannot see each other.

`max_agents` is read once per loop, at `LaunchPlan(candidates, running, l.cfg.MaxAgents)`. It is
the only cap on concurrent agents, so N instances give N times the budget with nothing aware of the
total. Bounding how many agents run at once is most of what the app is for.

The wanted behaviour, "I am in `support-app`, show me `support-app`'s work", needs none of that. It
is a question about what renders.

## Decision

One daemon, one database, every configured repo. Narrowing is a **scope**: a query parameter the
board reads and the loop ignores. Every ticket is observed, derived and acted on whatever the scope
says.

Two axes. `?feature=` is primary and matches CONTEXT.md's definition of a feature as the unit the
board scopes to. `?repo=` is secondary, and exists because a working directory can imply a repo but
never a feature. `cc open` resolves the cwd's git origin against the configured remotes and opens
the board at `?repo=<name>`, which is the whole of what running from inside a repo means.

**A scope admits a group whole.** Filtering is applied to groups after `groupRows`, keeping any
group where the root or any child matches. Filtering rows instead would leave `rootRow := byURL[root]`
missing its root and render a zero-value row; filtering in `store.Tickets` would reproduce the
`plan.Unlocked` refusal this ADR exists to avoid.

## Consequences

A scoped board shows tickets outside its scope: the blocker gating your work stays visible with its
own feature or repo named on it. That reads as a bug until you know the rule, which is why it is
written here and in CONTEXT.md under **Scope**.

The band counts the scoped rows, so `yours` stays honest. Because `band.tmpl` is deliberately
outside the board's five-second swap, scope controls are plain links and a scope change is a full
page load. Live agents stays in the masthead and stays global: it is a process fact bounded by
`max_agents`, not a board fact.

Nothing stops a second instance against a second `CC_DATA_DIR` and a second database. The flock is
per data directory and will not notice. That is not supported and buys nothing, since the scope
gives the same view without splitting the DAG or doubling the agent budget.

If the app ever needs to serve repos that must not see each other, this is the ADR to revisit, and
the change is a real one: an instance column, a per-instance flock and a config split, not a filter.
