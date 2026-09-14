# Plan: the operator surface

> Source: [`docs/prds/prd-operator-surface.md`](../docs/prds/prd-operator-surface.md).
> Normative for the state list, the consent binding, the config and schema, and the invariants:
> [`docs/designs/command-centre-design.md`](../docs/designs/command-centre-design.md) revision 4,
> cited as `§n` and `inv. n`.
>
> Predecessors: [`plans/command-centre-phase-1.md`](command-centre-phase-1.md),
> [`command-centre-phase-2.md`](command-centre-phase-2.md),
> [`command-centre-phase-3.md`](command-centre-phase-3.md). Their Go conventions, testing layers,
> on-disk layout and schema hold unchanged and are not restated.

**Not a phase number.** Two numberings are already live in this repo and they collide. This plan
uses its own 1 to 8. Phases 1 to 5 are the PRD's Part A, 6 to 8 its Part B.

Goal, and it is countable: **zero shell commands to take a slice from `ready` to two open PRs.**
The terminal keeps reading worktrees and tailing logs, which is why the worktree path, pgid and
log path stay on the row as selectable text.

## The starting point

Phases 1 to 3 of the command centre have landed, so the plumbing under the page is complete and
the page is the thin part. What exists:

- `page.tmpl`, 54 lines, one flat table of 18 columns. A per-row checkbox joined to a slice-wide
  launch form by `form="launch"`, and a per-row single-button form posting to `/verb`.
- `plan.Verbs(State)` (`internal/plan/verbs.go`), the single implementation of §5's verb column,
  table-tested and rendered by the template.
- `POST /verb` accepting all ten verbs, with appliers in `internal/cc/verbs.go` for re-run,
  re-check, close-pr, remove-worktree, cancel and abort, plus kill in `loop.go`, retry-push in
  `push.go` and refresh in `refresh.go`. Every state those verbs attach to is reachable.
- `POST /launch` composing, hashing and queueing one `intents` row per task behind
  `requireBrowserOrigin`.
- `GET /preview` building `[]previewRow` from `plan.Preview` and `plan.ProspectiveBase`.
- `StackDepth` and `MergeOrder` on every row, as numeric columns.
- `testdata/page.golden.html` pinning the whole render.

What this plan builds, in one sentence each:

1. `GET /preview` encodes JSON, and the board's launch form posts straight past it to `/launch`.
2. `handleLaunch` and `handleVerb` both end `w.WriteHeader(http.StatusAccepted)`, so pressing a
   verb button leaves you on a blank 202.
3. `/launch` recomposes and hashes on its own, and nothing carries the hash the operator read.
4. Nothing on the board shows an unconsumed intent.
5. The board is flat, unstyled and static.

## Technical design decisions

### Routes

| Method and path | Exists | This plan |
|---|---|---|
| `GET /{$}` | yes | grouped board, phase 4 |
| `GET /preview?task=…` | yes, JSON | HTML, phase 3. Content negotiation is not added: the JSON shape has no consumer, so the handler stops encoding it |
| `GET /confirm?verb=…&task=…` | no | phase 5, `kill` and `remove-worktree` only |
| `GET /task/{task}/detail` | no | phase 6 fragment |
| `GET /task/{task}/log` | no | phase 7, SSE |
| `POST /launch` | yes | takes `hash` per task, phase 3; redirects, phase 1 |
| `POST /verb` | yes | redirects, phase 1 |
| `GET /events` | yes | untouched |

`{task}` is a ticket URL and therefore contains `://`. It goes in the path segment
percent-encoded, matched by `GET /task/{task}/detail` and decoded with `url.PathUnescape`. The
alternative, a query parameter, reads worse in an `hx-get` and buys nothing.

### Schema: no change, and one new read

`internal/cc/schema.sql` stays at version 1. `OpenStore` refuses a mismatch and there is no
migration code, so a new table bricks every developer database. Phase 2 needs one new query, not
one new column:

```go
// PendingIntentsByTask is every unconsumed intent, keyed by task, most recent last.
func (s *Store) PendingIntentsByTask(ctx context.Context) (map[string][]string, error)
```

`SELECT task_id, verb FROM intents WHERE consumed_at IS NULL ORDER BY id`. It sits beside the
existing per-verb `PendingVerbIntents` (`internal/cc/runs.go:221`) rather than replacing it: the
loop wants one verb's intents and the page wants all of them, and collapsing both into one call
would make the loop filter in Go what SQL already filters.

### The row model, and where grouping lives

`row` (`internal/cc/server.go:83`) gains two fields and no more:

```go
PendingVerbs []string // phase 2, from PendingIntentsByTask
Blocking     []string // phase 4, plan.Unlock.Blocking, already computed and currently discarded
```

Grouping is a **third pass in `derive`**, after the existing base-verdict and depth pass, and it
returns a tree rather than mutating rows:

```go
// group is one blocker and the rows waiting on it. Rows with no blocker in the task set are
// their own group with an empty Root.
type group struct {
    Root     *row  // the blocker, nil for an ungrouped row
    Children []row // merge-first, so Children[0] merges next after Root
}
```

`pageView.Groups []group` replaces `pageView.Rows`. Keeping grouping out of `plan` is deliberate:
it is layout over a value `plan` already returns, and `internal/plan` stays free of anything a
template shape dictates.

**A row belongs to at most one group.** `Unlock.Blocking` can name several blockers (the
multi-blocker arm, `internal/plan/plan.go:119`). The group is the *first* blocker in `Blocking`
order, and the row's own reason still names all of them, so nothing is hidden by the choice.

### Consent: the hash travels, and `/launch` is still the authority

Phase 3 adds a `hash` field per task to the `POST /launch` body. `handleLaunch` recomposes as it
does today and compares. On mismatch it refuses with `409 Conflict` and names the task.

This is a second gate, not a replacement. The tick recomposes at spawn time and refuses on
mismatch (§4b), and that stays the load-bearing one. What the submitted hash catches is narrower
and worth catching: a seam file edited between reading the preview and pressing authorise.

### Verbs: one implementation, already

`plan.Verbs(State)` is the single implementation of §5's verb column and neither part changes it.
Phase 5's confirm step is a property of the *verb*, not the state, so it is a package-level set in
`internal/cc`:

```go
var destructiveVerbs = map[string]bool{killVerb: true, removeWorktreeVerb: true}
```

`kill` sends a signal to a live process group and `remove-worktree` deletes a directory. Neither is
recoverable, and inv. 18 defends against another origin, not against you.

### CSS, and how much

One embedded stylesheet, `internal/cc/page.css`, `go:embed`ed and inlined into a `<style>` block
by the template. Under a hundred lines. No framework and no component library until an island
needs one. Inlining rather than serving `/page.css` keeps the route table shorter and the whole
render golden-testable in one file, which is the point of keeping the board in HTML.

### Liveness: meta refresh, then htmx, and the trigger is the island

Phase 1 adds `<meta http-equiv="refresh" content="5">`. It is correct because there is nothing on
the page a reload can destroy. Phase 6 removes it and replaces it with `hx-trigger="every 5s"` on
the table, and the reason it happens in phase 6 rather than phase 8 is that row detail needs a
fetch, which needs htmx. Once htmx is in, the meta refresh is a full reload competing with a
fragment swap, so it goes on the same change.

### Verbs cannot be optimistic, and the page says so

A POST writes an `intents` row. The loop applies it up to fifteen seconds later (inv. 9). No
amount of frontend changes that. Phase 2's queued-verb badge is the honest rendering of the gap,
and an acknowledgement swap is the ceiling for phase 6.

### Layer 3 build

`web/` holds island entrypoints. Vite writes `internal/cc/assets/dist/`, that directory is
`go:embed`ed, and `just build` runs the JS build before `go build`. One binary out. Phases 1 to 7
need neither Node nor Vite.

### Every island container carries `hx-preserve` and a stable id

htmx swapping any ancestor of an island destroys and remounts it, losing a half-panned graph. This
is an ongoing constraint on every swap target from phase 6 on, not a phase 8 detail.

### Testing

Three layers, as the predecessor plans have them.

- **`internal/plan`**: pure, table-driven. Nothing in this plan adds to it.
- **`internal/cc`**: `testdata/page.golden.html` via `go test ./internal/cc -update`. Every layout
  change here is a diff to that one file. Named tests beside it for anything a golden diff states
  weakly: redirect targets, hash mismatch, the confirm gate.
- **`e2e/`**: testscript over the fake `gh` and fake agents. The zero-shell-commands claim is an
  e2e assertion, not a unit one.

---

## Phase 1: the verb loop closes

**User stories**: run a verb without a terminal; "what live means".

### What to build

`handleLaunch` and `handleVerb` stop returning `202 Accepted` and return `303 See Other` with
`Location: /`. 303 rather than 302 so the browser follows with GET, which is what makes the
back button behave and what stops a reload re-posting a kill.

Seven assertions on `http.StatusAccepted` across `server_test.go` (three),
`server_verb_test.go` (three) and `server_forms_test.go` (one) change with it. `http.Client`
follows a 303 by default and would turn every one of them into a 200 on the board, which asserts
nothing about the POST. Each moves to a client with
`CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }`
and asserts the 303 plus its `Location`.

`e2e/register/register.go`'s `request` subcommand needs the same `CheckRedirect`, and this is the
one change that keeps the txtar suite untouched. It drives 34 POST calls across 19 script files
with `server.Client().Do(req)` and copies the response body to stdout. Following the redirect would
make every one of those print the whole board instead of the POST's own empty body. Nothing asserts
on a POST's stdout today, because each of those scripts follows with an explicit
`exec cc request GET /` and testscript scopes `stdout` to the last command, so this is noise rather
than breakage. It is still worth fixing at the source instead of leaving 34 calls silently
rendering a board nobody reads.

`page.tmpl` gains `<meta http-equiv="refresh" content="5">` and a `<style>` block inlining the new
embedded `page.css`. The stylesheet is the minimum that makes 18 columns readable: a monospace
stack, collapsed borders, cell padding, and a muted colour for the empty-string columns. No
grouping styles yet, they arrive with phase 4.

### Acceptance criteria

- [ ] `POST /verb` returns 303 with `Location: /`; a browser lands back on the board
- [ ] `POST /launch` returns 303 with `Location: /`
- [ ] A reload after a verb POST does not re-queue the intent
- [ ] Both handlers still reject a missing or foreign `Origin` with 403 before doing anything
- [ ] The board carries a five second meta refresh
- [ ] `page.css` is embedded and inlined; no second HTTP route serves it
- [ ] `page.golden.html` regenerated and reviewed

---

## Phase 2: the queued verb on the row

**User stories**: a row with an unconsumed intent shows the queued verb beside its state.

### What to build

`Store.PendingIntentsByTask` as specified above. `render` calls it once and passes the map into
`derive`, which fills `row.PendingVerbs`. The template renders them next to the state, as
`running · kill queued`.

A row with a queued verb still renders its verb buttons. The button is not disabled: the loop may
refuse an intent (`eventReRunRefused`, `eventRemoveWorktreeRefused` in `internal/cc/verbs.go`), so
a queued intent is not a promise, and disabling the button would strand the operator if it is
refused. The badge is information, not a lock.

**A pending `launch` intent is the badge that matters most, so it is not filtered out.**
`plan.Status` reads `Authorised` from `LaunchMemberships`, which selects from `launch_members`
joined to `launches` (`internal/cc/launch.go:150`), and only the tick's `ApplyLaunchIntents` writes
those rows. So for up to fifteen seconds after pressing authorise, the task is still `ready` or
`blocked` on the board with nothing to show a launch is coming. That is precisely the window in
which an operator presses authorise twice. A `ready · launch queued` badge closes it.

### Acceptance criteria

- [ ] `PendingIntentsByTask` returns only rows with `consumed_at IS NULL`, keyed by task
- [ ] A row with a pending `kill` renders the queued verb beside its state
- [ ] The next tick consumes the intent and the badge disappears without a schema read change
- [ ] `launch` intents produce no badge
- [ ] A row with two pending verbs renders both
- [ ] Verb buttons stay enabled on a row with a pending intent
- [ ] `page.golden.html` covers a row with a queued verb

---

## Phase 3: the preview becomes a page you read

**User stories**: select a slice, read the preview, authorise it; consent bound to what a human read.

### What to build

The board's slice submit changes from `POST /launch` to `GET /preview`, so the launch form becomes
`<form id="launch" method="get" action="/preview">` and the checkboxes are unchanged. Selecting
three rows and submitting navigates to `/preview?task=…&task=…&task=…`.

`handlePreview` stops encoding JSON and renders a new embedded `preview.tmpl` over the
`[]previewRow` it already builds. Per the PRD's § A launch: the label (`now`, `on unlock` or
`refused`), the base with its `origin/` prefix, the reason, and the base's own verdict where it has
one. A `refused` row names the blocker sitting outside the slice, which `plan.Preview` already
writes into the reason. The composed prompt renders in a `<details>` per row, because it is long
and it is the thing the hash is over.

The page carries `[ authorise ]` and `[ cancel ]`. Authorise is a `POST /launch` carrying one
`task` and one `hash` per non-refused row. Cancel is a link back to `/`. Refused rows render with
no checkbox and are not submitted.

`handleLaunch` reads `r.Form["hash"]` paired positionally with `r.Form["task"]`, recomposes, and
compares. Mismatch is `409 Conflict` naming the task and both hashes. A body with no `hash` fields
at all is still accepted, which keeps the hand-built `POST /launch?task=…` path in `e2e/` working
and keeps the tick as the sole load-bearing gate.

### Acceptance criteria

- [ ] Submitting the board's checkboxes navigates to `GET /preview` with one `task` per selection
- [ ] `/preview` returns HTML; the JSON encoding is gone
- [ ] Each row shows its label, `origin/`-prefixed base, reason and base verdict
- [ ] A row blocked by a task outside the slice renders `refused` and names that blocker
- [ ] A row already in an active launch renders `refused` naming the launch id
- [ ] The composed prompt is on the page, under the hash it produced
- [ ] `[ authorise ]` POSTs one `task` and one `hash` per non-refused row and lands back on the board
- [ ] A `hash` that no longer matches a recomposition returns 409 and queues nothing
- [ ] A `POST /launch` with no `hash` fields still works
- [ ] A slice of five with a mid-stack blocker outside it renders three `on unlock` and two `refused`
- [ ] The preview render has its own golden file

---

## Phase 4: the grouped board

**User stories**: four `queued` rows behind one `failed` blocker read as one problem, not four;
stack order is obvious because you merge bottom-up and the app never merges.

### What to build

`row.Blocking` is filled from `plan.Unlock.Blocking`, which `derive` already computes and throws
away after producing a reason string. `derive` gains a third pass building `[]group` as specified
above, and `pageView.Groups` replaces `pageView.Rows`.

Ordering, and it is the whole point of the phase:

- Groups sort by root ticket URL, so the board is stable across ticks.
- Within a group, children sort by `MergeOrder` ascending, then ticket URL. `Children[0]` is the
  next thing you merge after the root.
- An ungrouped row is a group with a nil `Root` and one child. They render after every real group,
  so the fan-out is at the top where the attention is.

The template renders a group as a header row carrying the root's ticket, state and reason,
then its children indented by `StackDepth` via a `data-depth` attribute the stylesheet keys off.
Indentation is CSS `padding-left`, not `&nbsp;`, so a copy-paste of the ticket column stays clean.
Every child keeps its own state, reason and verb buttons. Nothing is collapsed away.

`page.css` grows the grouping rules: a rule per depth up to four, a left border on the group to
make the span visible, and a muted group header. Still under a hundred lines.

### Acceptance criteria

- [ ] A root with four dependents renders as one group of five, not five sibling rows
- [ ] The group line carries the blocker's own state and reason
- [ ] Each child still renders its own state, reason and verb buttons
- [ ] A root that is `failed` with four `queued` children reads as one failure
- [ ] Children order by merge order, `Children[0]` merging next after the root
- [ ] A row whose `Blocking` names two blockers appears exactly once, under the first
- [ ] A task set with no blockers at all renders as a flat list of one-child groups
- [ ] Indentation is CSS-driven; the ticket column copy-pastes without leading whitespace
- [ ] Group order is stable across two renders of unchanged data
- [ ] `page.golden.html` covers a five-row fan-out, an ungrouped row, and every verb button per state
- [ ] The txtar suite's `<td>state</td>` assertions still hold, or are updated to the grouped markup

---

## Phase 5: confirm on the two destructive verbs

**User stories**: `kill` and `remove-worktree` destroy work and both sit one click away.

### What to build

`GET /confirm?verb=…&task=…` renders a one-question page: what the verb does to this task, the
concrete thing at risk (the pgid for `kill`, the worktree path for `remove-worktree`), and
`[ confirm ]` posting to `/verb` beside a link back to `/`.

The board's verb form for a verb in `destructiveVerbs` becomes a `GET /confirm` submit rather than
a `POST /verb`. Every other verb is unchanged. `/verb` itself gains no gate: it stays the one blind
INSERT, and the confirm page is a step in front of it, not a guard inside it. A hand-built
`POST /verb?verb=kill` still works, which is what keeps `e2e/` unchanged.

A second page rather than an in-place confirm, because an in-place confirm is layer 2 and Part A is
layer 1. Phase 6 does not revisit it: a full-page confirm for something irreversible is the right
shape whether or not htmx is available.

### Acceptance criteria

- [ ] `kill` and `remove-worktree` on the board submit to `GET /confirm`
- [ ] `/confirm` names the task, the verb, and the pgid or worktree path at risk
- [ ] `[ confirm ]` POSTs to `/verb` and redirects to the board
- [ ] The cancel link queues nothing
- [ ] `/confirm` for a non-destructive verb is a 400
- [ ] `/confirm` for an unknown task is a 400
- [ ] `POST /verb?verb=kill&task=…` still queues directly, no confirm required
- [ ] Every other verb still posts straight to `/verb` from the board

---

## Phase 6: htmx enters, and row detail with it

**User stories**: see a row's detail without leaving the board; liveness that does not destroy
what is mounted.

### What to build

htmx vendored into `internal/cc/assets/` and `go:embed`ed, served from one route. Vendored rather
than a CDN link because the binary is the deliverable and the loop runs on a laptop that may be
offline.

`GET /task/{task}/detail` renders a fragment: the log tail (last fifty lines of `runs.log_path`),
the check list from `obs.PRs[branch].Checks` (`map[string]gh.CheckState`, `internal/gh/gh.go:60`),
the base SHA from `runs.baseline_sha`, elapsed, and the worktree path from `obs.Worktrees[branch]`. A `detail`
button per row does `hx-get` with `hx-target` on a sibling `<tr>` and `hx-swap="innerHTML"`.

The table gains `hx-trigger="every 5s"` with `hx-get="/"` and `hx-select` on the table, and the
meta refresh comes out on this same change. Tick age and last tick error move into an
`hx-swap-oob` region so one response updates the header and the table without two requests.

The fragment reuses `derive`'s output. `render` is refactored so a single-task view is the same
value the board row is, per the PRD's one-derivation rule: any endpoint an island calls serves the
same view struct the template renders, and `plan.Unlocked` and `plan.Status` stay called in
exactly one place.

### Acceptance criteria

- [ ] htmx is embedded in the binary and served locally; the page loads with no network
- [ ] `GET /task/{task}/detail` returns a fragment, not a full page
- [ ] The fragment carries log tail, check list, base SHA, elapsed and worktree path
- [ ] A percent-encoded ticket URL in the path resolves to the right task
- [ ] An unknown task is a 404
- [ ] Expanding a row does not reload the board
- [ ] The table swaps every five seconds and the meta refresh is gone
- [ ] Tick age and last error update out of band on the same response
- [ ] Detail values come from the same derivation the board row uses, asserted by test
- [ ] With JavaScript disabled the board still renders and every verb form still works

---

## Phase 7: the log tail over SSE

**User stories**: tail a run's log without a terminal.

### What to build

`GET /task/{task}/log` as an SSE stream over the run's `log_path`, one event per line, and the
htmx SSE extension `sse-swap` appending into a `<pre>` inside the detail fragment.

The stream reads the file, it does not touch the agent process. The loop owns the process (inv. 9)
and a reader that could affect it would break that. Closing the connection when the run ends is a
matter of the file no longer growing plus `runs.ended_at` being set, not of watching the pgid.

One bounded concern worth naming: an unbounded `<pre>` grows without limit over a long run. The
client keeps the last thousand lines and drops from the front.

### Acceptance criteria

- [ ] `GET /task/{task}/log` streams `text/event-stream`, one event per line
- [ ] New lines appear in the `<pre>` without a page reload
- [ ] The stream closes when the run ends
- [ ] A task with no run yet gets an empty stream, not an error
- [ ] Closing the browser tab does not affect the running agent
- [ ] The `<pre>` holds at most a thousand lines

---

## Phase 8: the dependency graph as the first island

**User stories**: read a dozen rows of cross-repo edges as a graph; click to build a slice.

### What to build

`web/` with the graph entrypoint. Vite writes `internal/cc/assets/dist/`, that directory is
`go:embed`ed, and `just build` runs the JS build before `go build`.

The graph mounts into an empty div Go never renders inside. It carries `hx-preserve="true"` and a
stable `id`, so the phase 6 table swap leaves a half-panned graph alone. It reads the same view
struct the board renders, over a JSON route that serves it verbatim. No unlock logic in TypeScript,
ever.

Panning and zooming are client-side with no round trip per frame, which is the test an island has
to pass. Clicking nodes builds a selection and submitting it navigates to `GET /preview` with the
same query string the board's checkboxes produce, so the graph is a second way to select a slice
and not a second way to launch one.

Preact against React is decided here rather than in advance, and the one real input is that shadcn
is React-only, which matters only if a component library ever does.

### Acceptance criteria

- [ ] `just build` runs the JS build then `go build` and emits one binary
- [ ] The graph renders every task as a node and every `blocked_by` and seam edge as an edge
- [ ] Pan and zoom do no server round trip
- [ ] The five second table swap leaves a panned graph in place
- [ ] Clicking nodes builds a selection; submitting navigates to `/preview` with the same query the checkboxes produce
- [ ] The graph reads the same view struct the board renders, over a route that serves it verbatim
- [ ] No unlock or status derivation exists in TypeScript
- [ ] A build with no Node present fails with a message naming Vite, not a stack trace

---

## Open questions this plan does not settle

- **Where intake comes from.** `POST /tasks` is deferred in §12 with the trigger "you want to add a
  ticket without re-running `to-tickets`". Nothing in these eight phases needs it.
- **What a `refused` preview row reads like.** Phase 3 renders `plan.Preview`'s reason verbatim,
  which is accurate and long: "blocked by PLA-09, which has no open or merged pull request outside
  this slice". Shortening it is a copy change against a golden file, not a design change.
