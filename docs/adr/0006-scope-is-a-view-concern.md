# 6. Scope is a view concern, not an instance boundary

**Date:** 2026-10-11 · **Status:** accepted

## Context

Running `cc` once per repo would break three things. A cross-repo blocker would be unknown to its
consumer's instance, so the consumer would park permanently. The schema has no instance column, so
instances would share or split the DAG. And `max_agents` is the only cap on concurrent agents, so N
instances give N times the budget.

"Show me this repo's work" is a question about what renders.

## Decision

One daemon, one database, every repo. A **scope** narrows what a page renders and never what the loop
reconciles. There are three:

- **Feature**, the primary scope (`/f/{feature}`).
- **Repo** (`/repos/{owner}/{name}`). `cc open` opens it for the cwd's git origin.
- **Project**, a named set of repos. A repo belongs to at most one project. Nothing infers a project,
  not a shared owner, a label or a path: it exists because the operator grouped repos on the Repos
  page. Projects are configuration ([ADR 9](0009-the-loop-owns-reconciled-state.md)).

**A scope admits a blocker chain whole.** Groups are filtered after they are built, keeping any whose
root or child matches, so an out-of-scope blocker still renders, named with its own feature or repo.

The tracker's label prefix `project:<slug>` marks a feature. It is an outside string the fleet, the
`to-tickets` skill and the tracker decoder read, and the app does not rename it, so code that reads a
label says feature and code that reads a repo grouping says project.

## Consequences

Live agents stay global, bounded by `max_agents`. Deleting a project loosens its repos and never
deletes them. Because nothing derives a project, `plan` knows nothing of them.

A second instance on another `CC_DATA_DIR` is unsupported; the flock will not notice. Serving repos
that must not see each other needs an instance column, a per-instance flock and a config split.
