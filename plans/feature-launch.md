# Plan: feature-scoped launch

> Source: the grilling session of 2026-09-20. Decisions recorded in
> `docs/adr/0013-a-feature-is-closed-under-blocked-by.md`,
> `docs/adr/0014-opening-the-launch-modal-imports-the-feature.md`, and in `CONTEXT.md` under
> **Feature** and **Launch modal**.

Launching is per-ticket today: you tick rows on the board, land on `GET /preview`, and authorise.
The DAG the app exists to drive is a property of a feature, so the selection is busywork that
reconstructs by hand something the tracker already knows. This plan makes the feature the unit of
launch and deletes the per-ticket preview page, keeping a per-ticket entry point into the same
modal.

## Technical design decisions

**Routes.**

| Route | Purpose |
| --- | --- |
| `GET /features` | Every feature the tracker offers, imported or not. `?q=` filters by name. |
| `GET /features/{feature}` | 303 to `/?feature={feature}`. A permalink, not a page. |
| `POST /launch/open` | Queues the import intent, nudges the loop, returns the modal shell. |
| `GET /launch/candidates` | The candidate set for a feature or a ticket list. Fragment in phase 3, JSON from phase 4. |
| `POST /launch` | Unchanged. |

`{feature}` is the tracker's label name, percent-escaped, which is exactly what `tickets.feature`
holds, what `?feature=` already matches and what `gh --label` already takes. No id, no slug, no
lookup table.

`GET /launch/candidates` takes either `?feature=<name>` or repeated `?ticket=<url>`, the latter
being the parameter `GET /preview` takes today. That is what lets phase 5 point the board's
checkboxes at the same modal. `POST /launch/open` takes the same two shapes.

`GET /import`, `POST /import`, `GET /preview` and their templates are deleted. The `import` intent
verb and `applyImportIntents` stay: they are how the modal imports. Re-importing a feature to pick
up new tracker tickets is opening its modal.

**Schema.** No change. `tickets.blocked_by` is already the DAG, `tickets.feature` is already the
grouping, and `launches` plus `launch_members` already record consent. Nothing in this plan writes
a column that does not exist.

**Key models.**

`ImportFeature` (`internal/cc/import.go`) loses its `Tickets` field. The features list shows a name
and whether it is imported, so `ImportFeatures` drops the per-feature `src.Tickets` call and costs
one `gh label list` per repo rather than one plus one per feature.

`candidate` is the new carrier, one per ticket a launch could cover: `URL`, `Ref`, `Title`,
`Repo`, `Feature`, `Label` (`now` / `on unlock` / `refused`), `Reason`, `Base`, `BaseVerdict`,
`PromptHash`, `BlockedBy []string`. Its json tags are the island's only view of the data, following
the precedent `row` sets for `GET /graph.json`. It carries `BlockedBy` directly rather than the
root-and-children shape `group` uses, because the modal needs the edge set to run the untick guard.

`FeatureClosureError` joins `FeatureConflictError` in `internal/cc/store.go`, naming the ticket,
its outside blocker and that blocker's feature. `RecordImportRefusal` widens from taking a
`*FeatureConflictError` to taking any import refusal.

**Module boundaries.**

`internal/plan` is untouched. Closure is a property of what gets imported, not a decision, and
`plan.Preview` already returns everything a candidate's label needs.

`internal/tracker` is untouched. It already returns `BlockedBy` per ticket.

Closure is one function in `internal/cc` with two callers, `ImportTickets` and
`applyEditTicketIntents`. It takes the feature's intended ticket set, a lookup for a ticket's
current feature, and a predicate for "this blocker has merged", and returns the first violation. It
is table-testable without a database, which is the point of separating it from either caller.

The candidate set is one function taking a ticket selection and the facts `loadTicketFacts` already
gathers, returning `[]candidate`. Both the fragment (phase 3) and the JSON route (phase 4) call it,
and it inherits most of `handlePreview`'s body, which is deleted rather than rewritten.

The graph layout splits. `web/src/layout.ts` takes nodes and edges and returns positions;
`graph.tsx` builds nodes and edges from `[]group`, and the modal builds them from each candidate's
`BlockedBy`. Neither island imports the other.

**The loop nudge.** `Loop` gains an unexported `chan struct{}` buffered to one and an exported
`Nudge()` that sends without blocking, so a nudge arriving mid-tick is dropped rather than queued.
`Run` selects on it beside the ticker. `Server` holds a `nudge func()`, wired by `App.New`, so the
server keeps knowing nothing about the loop. `RunOnce` is already idempotent and already called
off-schedule by `App.RunOnce`; the nudge adds a caller, not a mode.

**Key flow: opening the modal.** `POST /launch/open` queues an import intent for the feature,
calls `nudge`, and returns the modal shell with a pending body. The body polls
`GET /launch/candidates`, which answers one of three things: still pending, because the tick has
not imported yet; refused, read from `store.LastImportError` where the refusal names this feature;
or the candidate set. The board's `import_refused` banner keeps its own job of reporting a refusal
you walked away from.

**Key flow: the untick guard.** Unticking a candidate that another ticked candidate is blocked by
is refused, naming the dependents. The user clears them bottom-up, so the confirmed set is always
closed under blockers. That closure is why every candidate's `Label` can be computed once,
server-side: with no blocker outside the set, `plan.Preview` cannot return `Refused` for
`blocker outside this slice`, and its remaining refusals (an active launch, a conflicted base) do
not depend on what else is selected. The island therefore toggles with no round trip.

**Integration points.** `gh label list` per repo for the features page. `gh issue list` plus one
`gh api .../dependencies/blocked_by` per issue when the modal opens, which is the existing cost of
`githubSource.Tickets` and is not addressed here.

**Authorisation.** Unchanged. `POST /launch/open` and `POST /launch` are wrapped in
`requireBrowserOrigin` like every other mutating handler. Opening the modal writes, so it is a
`POST` behind a button, never a `GET` behind a link. Consent stays bound to content: the candidate
carries the prompt hash the modal posts back, and `handleLaunch` still refuses a mismatch with 409.

---

## Phase 1: A feature refuses to import unless it is closed

**Covers**: launching a feature has a closed answer. A ticket blocked from outside its own feature
is tracker data the app refuses rather than resolves, the way a two-feature ticket already is.

### What to build

The closure function and its two callers. A feature is closed when every blocker of every ticket it
holds is itself in the feature, unless that blocker's pull request has merged.

`ImportTickets` runs it inside its existing transaction and fails the whole import on a violation,
naming the ticket, its outside blocker and that blocker's feature. It already reads the last
observation for the withdrawal logic, which is where the merged exemption comes from; a blocker it
has never seen at all is outside and unmerged, so it refuses. `applyEditTicketIntents` runs the
same function against an edited `blocked_by` and refuses the edit.

`RecordImportRefusal` widens to carry either refusal, so both land in the `import_refused` event
and the banner that already reads it.

Nothing imports differently when the data is good. This phase only adds a refusal.

### Acceptance criteria

- [ ] Importing a feature whose ticket is blocked by a ticket in another feature refuses whole, and no row from that import lands
- [ ] The refusal names the ticket, the blocker and the blocker's feature, and renders in the existing banner
- [ ] A blocker outside the feature whose pull request has merged does not refuse
- [ ] A blocker the app has never imported refuses
- [ ] `POST /ticket` cannot edit `blocked_by` into another feature, and the refusal reads the same way
- [ ] The closure function is table-tested without a database
- [ ] `internal/plan` and `internal/tracker` are unchanged

---

## Phase 2: The features page replaces the import page

**Covers**: view and search every feature the tracker offers. Open one and see its tickets.

### What to build

`GET /features`, listing every feature across the configured repos with its name and whether it is
imported, `?q=` filtering by name, case-insensitive substring. Imported-or-not comes from the
distinct features among the stored tickets, which `render` already has in hand. `ImportFeatures`
drops its per-feature ticket fetch.

`GET /features/{feature}` 303s to `/?feature={feature}`. One handler, no template, and the scoped
board's five-second swap and graph view come free.

`GET /import`, `POST /import` and `import.tmpl` are deleted, along with the launch-shaped parts of
neither: the `import` intent verb and `applyImportIntents` stay untouched, because phase 3 uses
them.

The masthead gains a link to `/features`.

### Acceptance criteria

- [ ] `GET /features` lists every feature the configured repos' trackers offer, imported or not
- [ ] A feature with no imported tickets is listed and marked as such
- [ ] `?q=` filters by name and survives a reload, like `?feature=`, `?repo=` and `?view=`
- [ ] Rendering the page makes one `gh label list` per repo and no per-feature call
- [ ] `GET /features/{feature}` 303s to the board scoped to that feature, with the label name escaped
- [ ] `GET /import` and `POST /import` are gone; nothing links to them
- [ ] An e2e test walks the features page to a scoped board

---

## Phase 3: Launch a whole feature from its row

**Covers**: launch from the features list, confirm before anything spawns, see what would start and
off what base. Agents run at the end of this phase.

### What to build

The loop nudge: the channel, `Nudge()`, the `select` in `Run`, and the `nudge func()` on `Server`
wired by `App.New`.

`POST /launch/open` with a feature, queueing the import intent and nudging. It returns a modal
shell whose body polls `GET /launch/candidates`, which answers pending, refused or the candidate
set. Refused reads `store.LastImportError` and shows the reason in place of the list.

The candidate set is `handlePreview`'s body, moved: per ticket, `plan.Preview` over
`plan.Unlocked`, the base or `plan.ProspectiveBase`, the base verdict, and the composed prompt's
hash. `GET /preview` and `preview.tmpl` stay for now; phase 5 deletes them.

The candidate list renders server-side, every ticket shown, no toggles. Confirm posts the whole set
to the existing `POST /launch` with its `ticket` and `hash` fields.

### Acceptance criteria

- [ ] Launching a feature never imported imports it and shows its candidates without waiting for the tick period
- [ ] A nudge arriving while a tick is running is dropped, not queued, and the intent is still applied
- [ ] An import refused for closure or for a two-feature ticket replaces the pending body with the reason
- [ ] Every candidate shows its label, reason, base and base verdict, so "you are about to build on a red parent" survives the deletion of `/preview`
- [ ] Confirming spawns agents for the unlocked tickets and queues the rest, with the same launch row and prompt hashes a per-ticket launch produces
- [ ] Relaunching a feature with an active launch shows its existing members refused as already authorised, and confirms only the rest
- [ ] `max_agents` still bounds concurrency: a twenty-ticket feature does not spawn twenty agents
- [ ] An e2e test covers open, import, confirm and spawn

---

## Phase 4: The launch modal island

**Covers**: untick individual tickets, see the DAG reshape, and be told what depends on a ticket
before you can drop it.

### What to build

`web/src/layout.ts`, taking nodes and edges and returning positions, extracted from `graph.tsx`'s
`layoutGroups` and `edgesFor` with `graph.tsx` rewritten to feed it from `[]group`. The existing
graph must render identically afterwards.

`web/src/launch-modal.tsx`, a `solid-element` island in a native `<dialog>`, mounted on `/features`
and never inside `#board`. It fetches `GET /launch/candidates` once as JSON, then holds the ticked
set locally. Unticking a candidate another ticked candidate is blocked by is refused, naming the
dependents. The DAG redraws from the ticked set on every toggle, with no fetch.

`GET /launch/candidates` serves JSON. Phase 3's fragment template is deleted.

Confirm posts the ticked set and its hashes to `POST /launch`.

`just assets` runs and the built sheet is committed.

### Acceptance criteria

- [ ] The graph view renders identically before and after the layout extraction
- [ ] The modal fetches once and makes no further request until confirm
- [ ] Unticking a ticket nothing depends on redraws the DAG without it
- [ ] Unticking a ticket others depend on is refused and names them
- [ ] Clearing dependents bottom-up then unticking the blocker works
- [ ] The confirmed set is always closed under blockers, so no candidate is ever confirmed with a blocker outside it
- [ ] The island mounts outside `#board` and survives the board's five-second swap
- [ ] No new dependency in `web/package.json`
- [ ] `internal/cc/assets/dist/app.css` is rebuilt and committed

---

## Phase 5: One launch path

**Covers**: the per-ticket entry point survives, the per-ticket preview page does not.

### What to build

The board's row checkboxes and the graph island's selection open the same modal, posting their
ticket list to `POST /launch/open` and reading `GET /launch/candidates?ticket=...`. A selection
spanning two features still works: closure is a property of a feature, and a hand-picked slice is
not one, so `plan.Preview` keeps its `blocker outside this slice` refusal for exactly this case.

`GET /preview`, `handlePreview` and `preview.tmpl` are deleted.

Golden files regenerate and the diff is read.

### Acceptance criteria

- [ ] Board checkboxes open the modal with the ticked rows as candidates
- [ ] The graph island's selection opens the same modal
- [ ] A selection spanning two features is launchable, and a member whose blocker sits outside it renders refused with its reason
- [ ] `GET /preview` returns 404 and nothing links to it
- [ ] There is one candidate-set function and one confirm path in the codebase
- [ ] Golden files in `internal/cc/testdata` are regenerated and the diff reviewed
