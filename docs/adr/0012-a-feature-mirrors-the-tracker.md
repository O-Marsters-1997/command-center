# 12. A feature mirrors the tracker, and scoping to one pulls in its blockers

**Date:** 2026-09-14 · **Status:** proposed

## Context

`CONTEXT.md` has defined **feature** since the glossary was written: "the tracker's own grouping of
tickets, and the unit the board scopes to. A ticket belongs to at most one feature." The code never
followed. `tracker.Group`, `ImportGroup` and `tickets.group_key` all use the word the glossary
reserved for the board's blocker-and-waiters layout, and the board has no feature scope at all.

What exists is half of it. `GET /import` lists every `project:` label across the configured repos
with the tickets it would bring in; `POST /import` takes a whole label. `tickets.group_key` holds
`project:x` as a denormalised string. Missing: per-ticket selection at import, a feature the board
can scope to, and cleanup of a feature as a unit.

Two questions have to be answered before any of that is built.

**Does selection define the feature?** If unchecking a ticket at import means it is not in the
feature, then a feature is a subset chosen at a moment in time — which is what a **slice** already
is, and the glossary pushed the two terms apart deliberately (each appears in the other's `_Avoid_`
list). It would also leave "the tracker added an eighth ticket" with no answer.

**What happens to a blocker outside the scope?** The board's entire layout is groups: one blocker
and the tickets waiting on it. Nothing stops a ticket in feature A being blocked by one in feature
B. A `WHERE feature_id = ?` board renders three `queued` rows whose blocker is off-screen — exactly
the confusion the glossary's example dialogue exists to prevent.

## Decision

**A feature mirrors the tracker.** Import checkboxes gate which tickets are pulled in; they do not
define membership. Re-importing later picks up tickets added since. The glossary entry stands as
written.

**A feature is a row with a surrogate id, and its label is mutable.** `tracker.Group` becomes
`tracker.Feature{ID, Name}`, where `ID` is whatever the tracker calls stable — GitHub's label node
id from `gh label list --json id,name`, Linear's project UUID when that source exists. `ID` is
opaque above `internal/tracker`, so nothing upstream learns that GitHub has node ids. `features`
keys on it, so renaming `project:x` to `project:y` updates a label rather than orphaning a feature
and creating a second one. `tickets.group_key` becomes `tickets.feature_id`, a foreign key.

**Scoping pulls in the closure.** `?feature=x` renders every ticket in x plus every ticket outside
x that blocks one of them. The foreign ones render in their group, marked, and are not counted by
the band. `CONTEXT.md` carries **foreign** as a term.

**The band follows the scope.** Scoped, the first card takes the feature's name instead of STATE OF
THE FLEET, and spend is the feature's spend. Per-feature spend is the figure the scope exists to
produce.

**Cleanup is all-or-nothing.** A feature-wide cleanup pre-checks every ticket and refuses the whole
thing, naming what blocked it. That requires the app to run `git status --porcelain` per worktree
itself, because `removeWorktreeOne` delegates the dirty check to `tp remove`, which runs after the
app has already removed the earlier worktrees. The `features` row is deleted with its last ticket.

## Consequences

Row count and feature size stop matching whenever a scope pulls in a foreign blocker. That is
deliberate, and it is the reason this is written down: the closure looks like a bug from inside the
query and will be "fixed" into a `WHERE` clause by anyone who has not read this.

The app now checks worktree dirtiness itself, duplicating a check `tp remove` also performs. The
two can disagree — on untracked files, or submodules — and the app's answer is the one that
refuses. The alternative was a cleanup that pre-checks, passes, and then leaves three worktrees
removed and four in place.

`tracker.Source` widens. A source that has no stable identity for its groupings cannot be added
without answering this, which is the right place for that cost to land.

Renaming `group` to `feature` through `internal/tracker`, `internal/cc` and the migrations settles
the last of `CONTEXT.md`'s flagged "group" ambiguity. sqlc has already generated identifiers from
`group_key`, so the rename now carries a regeneration with it: the column, the query files and
`internal/cc/ccdb` move together or the package does not compile. ADR 8's `ticket`/`task` rename
got in ahead of the first `generate`. This one did not, and pays a little more for it.
