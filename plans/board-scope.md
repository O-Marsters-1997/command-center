# Plan: board scope

> Source: the grilling session of 2026-09-19. Decisions recorded in
> `docs/adr/0011-scope-is-a-view-concern.md` and in `CONTEXT.md` under **Scope**, **Repo** and
> **Band**. Phase 1 discharges the prerequisite ADR 4 named and left undone.

## Technical design decisions

**Routes.** `GET /` and `GET /board` gain two optional query parameters, `feature` and `repo`.
Both are view state and join `sel`, `ticket`, `view` and `log` in `viewParams`, so `query()`
renders them into `boardPath()` and `pagePath()` and the board's own five-second `hx-get`
perpetuates the scope without the shell being involved. `GET /graph.json` inherits both, since it
serves `pageView.Groups` verbatim through the same `parseViewParams`.

An unrecognised value is silently the default, following `normalizeLogFilter`: a query string is
user input, and an unknown scope shows the unscoped board rather than an error. `repo` validates
against the configured repo names; `feature` validates against the distinct features among the
loaded tickets, which `render` already has in hand from `store.Tickets`.

Scope is URL state only. Nothing is remembered between sessions, matching every other view
parameter. `cc open` is what makes it sticky in practice.

**Schema.** Migration `0003` renames `tickets.group_key` to `tickets.feature`. `0002_withdrawal.sql`
landed on main first (#214), so this phase's migration takes `0003` rather than the `0002` the
phase originally assumed.

No other schema change. Scope stores nothing.

**Observation keys.** Every branch-keyed map in `Observation` becomes keyed by
`repo + "//" + branch`, extending the convention `mainTipKey` (`server.go:713`) already set for
`main`. Seven maps: `PRs`, `Worktrees`, `BranchTips`, `MidMerge`, `ConflictsWithBase`,
`ConflictedPaths` and `ConflictsWithPeer`, the last keyed that way at both levels. `MergifyHash`
(repo name), `Titles` (issue URL) and `Runs` (ticket id) are already unambiguous and do not move.

The observation is persisted, so this is a format change. A stored observation written under the
old keys reads as a tick that observed nothing, which the page already renders honestly as stale
facts and the next tick replaces. No migration, no compatibility shim.

**Key models.**

`viewParams` (`internal/cc/viewstate.go`) gains `Feature string` and `Repo string`. It stays the
single carrier of view state, and `toggleSel` and `toggleTicket` inherit both by copying the
struct, as they already do for `Log` and `View`.

`row` (`internal/cc/server.go:163`) gains `Repo string` and `Feature string`, both from the ticket,
both json-tagged so the graph island sees what the board sees.

`pageView` gains the scope itself plus the two link sets the masthead renders.

**Module boundaries.** `internal/gh` and the git helpers stay repo-agnostic: `gh.List` and
`Worktrees` keep returning plain branch names against a checkout path they are handed, and
`observe.go` applies the repo-qualified key at each copy site. `recordPeerConflicts` takes the repo
name as a parameter rather than deriving one, because it writes into the shared map directly.

`internal/plan` and `internal/verdict` are untouched by the whole plan. Scope never reaches a
decision, and the keying change is a map key, not a fact.

Scope filtering is one function over `[]group`, applied in `render` between `groupRows` and the
view. It has no other caller and needs no type of its own.

**Key flow: a scope admits a group whole.** A group is kept when its root or any child matches the
scope. This is why filtering happens after `groupRows` and not before: `groupRows` resolves a root
with `rootRow := byURL[root]` and no ok check, so a filtered-out root renders as a zero-value row.
Filtering in `store.Tickets` is worse still, because `plan.Unlocked` builds `byURL` from what was
loaded and refuses any blocker missing from it.

**Integration points.** `cc open` shells out to `git remote get-url origin` through the existing
`originURL`, and opens a URL with `open` on darwin and `xdg-open` elsewhere. Failure to resolve a
repo is not an error: it opens the unscoped board and says why on stderr.

---

## Phase 1: Observation keys carry the repo

**Covers**: two repos holding the same branch name stop contaminating each other. Discharges the
prerequisite ADR 4 records ("Fix the branch keying in `observe.go` first. Without that a second
repo does not work at all"), and closes the wrong-repo write before #129 hands the operator an
editable branch field.

### What to build

A key helper beside `mainTipKey`, taking a repo name and a branch. Every write in `observe.go`'s
per-repo loop uses it, and every read site that currently indexes by `t.Branch` uses it with
`t.Repo`. The reads that index by a base branch rather than a ticket branch take the repo from the
ticket whose base it is.

`recordPeerConflicts` gains a repo-name parameter and keys both levels of `ConflictsWithPeer` with
it. Its per-repo call site already passes only that repo's branches, so the computation was always
repo-local; only the storage was not.

Nothing about what is observed changes. This is the same facts under unambiguous keys.

### Acceptance criteria

- [ ] Every map in `Observation` is keyed by something that cannot name two repos at once
- [ ] Two configured repos each holding a ticket on the same branch name render independent rows, with their own PR, worktree, tip and conflict facts
- [ ] An e2e test covers exactly that collision, and fails against the previous keying
- [ ] `gh.List` and `Worktrees` still return plain branch names; the repo qualification is applied by `observe.go`
- [ ] `internal/plan` and `internal/verdict` are unchanged
- [ ] An observation persisted under the old keys does not panic or mis-render; the next tick replaces it

---

## Phase 2: The tracker's grouping is a feature

**Covers**: the codebase says what CONTEXT.md says. A feature is the tracker's grouping; a group is
the board's layout of a blocker and its waiters.

### What to build

A rename with no behaviour change. `tracker.Group` becomes `tracker.Feature`, `Source.Groups`
becomes `Source.Features`, `ImportGroup` and `ImportGroups` become `ImportFeature` and
`ImportFeatures`, and `POST /import`'s form field changes from `group` to `feature`. Migration
`0003` renames the column, and sqlc regenerates.

`groupRows`, `[]group`, `board.tmpl`'s `.Groups` and the graph island's `Group` type all stay: they
name the board layout, which is the usage CONTEXT.md keeps.

Every e2e txtar carries `cc request -form 'group=project:...' POST /import` and needs the field
renamed. That is the bulk of the diff and none of the risk.

### Acceptance criteria

- [ ] No identifier in `internal/tracker` or the import path calls a feature a group
- [ ] `tickets.feature` replaces `tickets.group_key`, via migration `0003`
- [ ] `POST /import` takes `feature=`
- [ ] The board layout is still called a group, in Go, in the templates and in `web/src`
- [ ] Golden files and e2e tests pass with no change other than the renamed field
- [ ] `git log -p` for this phase shows no behaviour change

---

## Phase 3: A ticket in two features is refused

**Covers**: CONTEXT.md says a ticket the tracker puts in two features "is a tracker problem the app
refuses rather than resolves". The upsert silently reassigns instead, which under a feature scope
is a row that disappears with no trace.

### What to build

`Store.ImportTickets` reads each ticket's stored feature before writing. Non-empty and different
from the feature being imported fails the whole transaction, with an error naming the ticket and
both features. The import page surfaces it as the import's own failure, not a tick error.

Re-importing the same feature stays idempotent, which is the existing behaviour and what the upsert
was for.

### Acceptance criteria

- [ ] Importing a feature whose ticket already belongs to a different one fails, naming the ticket and both features
- [ ] The transaction rolls back: no ticket from that import lands
- [ ] Re-importing a feature a ticket already belongs to still succeeds and refreshes title, body and status
- [ ] The refusal is visible on the import page without reading `/events`
- [ ] A ticket with no stored feature yet imports normally

---

## Phase 4: The board scopes to a feature

**Covers**: standing in a fleet carrying several features, seeing one of them and the blockers
gating it.

### What to build

`viewParams` gains `Feature`, parsed from `?feature=`, emitted by `query()`. `render` computes the
distinct features among the loaded tickets, blanks an unrecognised value, and filters the groups
after `groupRows`, keeping any group where the root or any child matches.

`deriveBand` takes the filtered rows, so the fleet card's "yours" counts what is on screen. The
masthead's live-agent count stays global and keeps reading `tickets`, not rows, because it is a
process fact bounded by `max_agents`.

The masthead grows a row of feature links, reusing the nav pill grammar already there for board and
graph, plus an "all" link. They are plain anchors, not htmx: `band.tmpl` is deliberately outside the
board's five-second swap (`server.go:49`), so a scope change is a full page load and the band
re-renders with it.

`row` gains `Feature`, so a kept group's out-of-scope member can say which feature it belongs to.

### Acceptance criteria

- [ ] `?feature=X` renders only groups with at least one ticket in X
- [ ] A group whose blocker is in another feature renders whole, with the blocker's own feature named on its row
- [ ] The band counts the scoped rows; the masthead's live-agent count does not
- [ ] An unknown feature renders the unscoped board rather than an error or an empty one
- [ ] The scope survives the five-second poll, expanding a row, and checking a row for launch
- [ ] `/graph.json` returns the same scoped groups the board renders
- [ ] The loop reconciles every ticket regardless of scope, proven by a tick acting on a row the scope hides

---

## Phase 5: The board scopes to a repo

**Covers**: "I am in support-app, show me support-app's work."

### What to build

`viewParams` gains `Repo`, validated against the configured repo names, which `Server` already
holds indexed by `stackingByRepo`. The same group filter takes both parameters and keeps a group
where any member matches the scope. `row` gains `Repo`, and a second link row lists the configured
repos.

Both axes compose: `?feature=X&repo=Y` keeps groups with a member in X and a member in Y.

### Acceptance criteria

- [ ] `?repo=Y` renders only groups with at least one ticket in Y
- [ ] A cross-repo group renders whole under either of its repos, with each row's repo named
- [ ] `?feature=X&repo=Y` composes rather than one overriding the other
- [ ] An unknown repo renders the unscoped board
- [ ] Clearing one axis leaves the other applied

---

## Phase 6: `cc open` scopes from the working directory

**Covers**: the ergonomics the whole plan was for. Type `cc` in a repo, get that repo's board.

### What to build

Export `cc.RepoNameForDir`, wrapping the existing `originURL` and `normaliseRemote` over the
configured repos: the same normalisation `repoForTicketURL` uses, so the ssh and https forms of one
repository resolve to the same configured name.

`cmd/cc/subcmd_prod.go` stops returning nil and resolves one subcommand, `open`. It loads the
config for the port, resolves the cwd, and opens `http://127.0.0.1:<port>/?repo=<name>`. The e2e
build's own `subcmd` must keep resolving `tick` and `request`.

No origin, no match, or no running daemon is not a failure: open the unscoped board, or print the
URL, and say which on stderr.

### Acceptance criteria

- [ ] `cc open` inside a configured repo's checkout opens the board scoped to it
- [ ] It works from a subdirectory and from a worktree, not only the checkout root
- [ ] The ssh and https forms of one remote resolve to the same configured repo
- [ ] A directory matching no configured repo opens the unscoped board and says why
- [ ] `cc` with no subcommand still runs the loop and the page, unchanged
- [ ] `go test -tags=e2e ./e2e/...` still resolves `tick` and `request`
