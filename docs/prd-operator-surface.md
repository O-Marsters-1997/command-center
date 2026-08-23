# The operator surface

**Status:** WIP · **Date:** 2026-08-23 · **Shape:** change doc

**Reads with:** [`prd-command-centre.md`](prd-command-centre.md) (the product doc this extends,
whose § The page is the one paragraph it replaces), [`command-centre-design.md`](command-centre-design.md)
(the design, revision 4, **normative** for the state list in §5, the consent binding in §4b, the
config and schema in §8, and the invariants in §9).

**Not a number.** Two phase numberings are already live in this repo and they collide.
`prd-command-centre.md` has product phases 1, 2 and 3. GitHub issues #2 to #7 are titled "Phase 1"
through "Phase 6" and are build steps inside product Phase 1. This doc uses neither. It has two
parts, A and B, and says under each where it lands.

## TL;DR

The page renders one row per task and stops there. You read it, then you open a terminal. This doc
turns it into the thing you work from. Select a slice, read the preview, authorise it, kill a wedged
agent, retry a push, and see four rows stuck behind one failure as one problem instead of four.

It splits in two. **Part A lands inside product Phase 1**, because Phase 1's own exit criteria
currently need `curl`. **Part B lands after product Phase 3**, and is where htmx and the first
JavaScript island arrive.

No prototype yet. The layout claims in Part A are the ones worth prototyping before building, and
that is called out under [Open questions](#open-questions).

## Contents

- [Current state](#current-state)
- [Why does this matter?](#why-does-this-matter)
- [Goal](#goal)
- [Proposed experience](#proposed-experience)
- [What we already have (reuse)](#what-we-already-have-reuse)
- [Important technical details](#important-technical-details)
- [Part A: the working page](#part-a-the-working-page)
- [Part B: the operator surface](#part-b-the-operator-surface)
- [Dependencies](#dependencies)
- [Out of scope](#out-of-scope)
- [Open questions](#open-questions)

## Current state

`internal/cc/page.tmpl` is twenty lines. A heading, the observe age, the last tick error, and one
flat table: ticket, state, why, branch, base, worktree, PR. No CSS, no JavaScript, no form, no
button. `internal/cc/testdata/page.golden.html` pins the render.

Three routes exist. `GET /` renders that table. `GET /preview` returns the launch preview as JSON.
`POST /launch` queues one `intents` row per task behind `requireBrowserOrigin`.

So the plumbing under the page is further along than the page. Four things are missing, and they are
different kinds of missing.

**The loop has no hands.** `POST /launch` works and nothing calls it. Authorising a slice today
means composing a `curl` with the right `Origin` header. Every other verb in v1 §5, `kill`,
`re-run`, `retry push`, `close PR`, `cancel`, `remove worktree`, has no route at all. Phase 1's own
requirement is "the states above on one page, mutating verbs POST-only", and the states are on the
page while the verbs are not.

**The preview is JSON.** v1 §4b makes the preview the moment a wrong base becomes a line you read.
A JSON array is not that. The whole consent mechanism, hashing what the operator previewed and
refusing to spawn on mismatch, is built and correct, and the operator cannot see what they are
consenting to.

**Two layout requirements from the product doc are unmet, and no issue owns them.** "Four `queued`
rows behind one `failed` blocker must read as one problem, not four", and "stack order must be
obvious, because you merge bottom-up and the app never merges". A flat table satisfies neither. Both
are claims about layout, so no amount of correct derivation fixes them.

**There is no live refresh.** The page is a static render against a fifteen second tick, so you
press F5 and guess.

## Why does this matter?

**A control plane you cannot press is a status page.** The product argument in
`prd-command-centre.md` is that the expensive thing is the attention between agents. Every verb that
lives only in a terminal puts that attention back. `kill` is the sharp one. v1 §13 limit 2 says the
app cannot tell a wedged agent from a working one and hands the judgement to you, which is fine
right up to the point where acting on that judgement means finding the pgid and typing `kill -TERM`
against a negative number.

**The preview is the only place bounded autonomy is visible.** Consent binds to content and the tick
refuses on mismatch. That machinery exists to make one thing true: your yes attached to a plan you
read. It is currently a yes attached to a JSON blob you did not look at.

**Grouping is not decoration, it is the difference between one problem and four.** A slice of five
with a failed root shows five red-ish rows in a flat table. It is one failed run. The information
that collapses them is already computed: `plan.Unlock.Blocking` names the blockers of every waiting
row, on every render. Nothing needs deriving. It needs laying out.

**Honest about Part A: it finishes Phase 1 rather than adding to it.** Part A ships no new
capability. Every state it renders, every verb it exposes and every reason it prints is already
specified and mostly already computed. It is the difference between Phase 1 being demonstrable and
Phase 1 being demonstrable by its author with a shell open. The payoff argument belongs to Part B
and to product Phase 2's fan-out.

## Goal

**You drive a two-ticket slice start to finish without opening a terminal.** One countable thing:
the number of shell commands needed to take a slice from `ready` to two open PRs, target zero. The
terminal stays in the loop for reading a worktree and tailing a log, which is why v1 §5 requires the
worktree path and pgid as copy-pasteable text rather than pretending otherwise.

## Proposed experience

### Reading the board

The board is still one HTML page at `127.0.0.1:7777` and still one row per task. What changes is
that rows with a shared cause render together.

A task waiting on blockers renders indented under the row it is waiting for, with the blocker's own
state on the group line. Five tasks whose root has `failed` read as one failed row with four
`queued` rows beneath it, each still printing its own reason. The grouping key is
`plan.Unlock.Blocking`, which the page already receives.

Stack order renders as depth, ordered so the row you merge first is the top row of its group. The
app never merges, so the order is information you act on rather than a thing the app respects.

Tick age and the last tick error stay where they are, at the top. They are the only signal that
separates a quiet loop from a dead one, and v1 §8 makes them requirements rather than polish.

Every row keeps the worktree path, pgid, base and elapsed time as plain selectable text.

### Selecting a slice and authorising it

A checkbox on every row that could be authorised, one submit at the foot of the board. Submitting
navigates to `/preview` with the selected tasks in the query string.

`/preview` renders HTML from the same `[]previewRow` the JSON path already builds. Each row reads as
one of `now`, `on unlock` or `refused`, with the base it would be cut from and the reason. A refused
row names the blocker sitting outside the slice. This is the layout `prd-command-centre.md` sketches
under § A launch, and building it is transcription rather than design.

The page carries `[ authorise ]` and `[ cancel ]`. Authorise is a POST to the existing `/launch`,
carrying the prompt hash the preview rendered. On success you land back on the board with the slice
`queued`.

Consent binding does not change. The hash the preview computed is the hash the member row stores,
and the tick recomposes at spawn time and refuses on mismatch (v1 §4b). What changes is that a human
saw the composition before saying yes.

### Running a verb

Every verb is a form with one button, POSTing to a per-verb route behind `requireBrowserOrigin`.
Which buttons a row shows comes from v1 §5's verb column for that row's state, so the state function
gains a `Verbs()` method and the template renders what it returns. No verb button appears on a row
whose state does not list it.

A verb POST writes an `intents` row and returns a redirect back to the board. **The response carries
no new state and cannot.** Only the loop writes the database (v1 §9 invariant 9), so the tick applies
the intent up to fifteen seconds later. The board says so: a row with an unconsumed intent renders
the queued verb next to its state, which is honest and also stops you pressing `kill` four times.

### Seeing detail on a row

Not in Part A. A row's detail, the log tail, the check list, the base SHA, is exactly the case where
plain HTML runs out: `<details>` cannot fetch, and rendering every row's detail inline is
unreadable at a dozen rows. Part B does it with `hx-get` into a sibling row. Until then the row
prints the log path and you `tail` it.

### What live means

Part A adds one line to the template:

```html
<meta http-equiv="refresh" content="5">
```

A full reload every five seconds against a fifteen second tick. It is correct precisely because
there is nothing on the page a reload can destroy. It comes out the moment the first island mounts,
and that is the trigger, not a date.

## What we already have (reuse)

| Exists | This doc's use of it |
|---|---|
| `plan.Status(Facts)` | every row's state and its reason sentence, already derived per render (`internal/cc/server.go:146`). The verb list attaches to the same enum |
| `plan.Unlock.Blocking` | the grouping key. The blockers of every waiting row, computed on every render, currently thrown away after producing a reason string |
| `plan.Preview` and `plan.ProspectiveBase` | the three preview labels and the base an `on unlock` row would get. `GET /preview` builds the rows already, it just encodes them as JSON |
| `plan.Compose` and `plan.Hash` | the prompt and the consent binding. The preview page renders the composition it hashes |
| `POST /launch` and `requireBrowserOrigin` | authorisation and invariant 18. Every new verb route wraps the same handler, which the comment on it already anticipates |
| `intents` table | every verb. Columns `task_id`, `verb`, `payload`, `consumed_at` are in schema v1 and only the launch path uses them |
| `runs.proc_started_at`, `runs.log_path`, `runs.pgid` | elapsed time, the log path and the pgid on a `running` row |
| `page.golden.html` | the render test. Every layout change in Part A is a diff to one golden file, which is the strongest single argument for keeping the board in HTML |

**Net-new.** A `Verbs()` method on the state enum. A verb route per v1 §5 verb, each queuing an
intent. An HTML render of the preview. A grouped board template. A CSS file, embedded, because a
grouped table with depth is not legible unstyled. In Part B: htmx, a detail fragment route, and the
`web/` and Vite build described below.

## Important technical details

### The layer split

```
Layer 1   Go html/template          every page, every form
Layer 2   htmx                      fragment swaps, row detail, out-of-band banners
Layer 3   Preact or React islands   graph, command palette, rich diff
```

Part A is layer 1 only. Part B adds 2 and 3.

The build path when layer 3 arrives: `web/` holds island entrypoints, Vite writes to
`internal/cc/assets/dist/`, that directory is `go:embed`ed, and `just build` runs the JS build
before `go build`. One binary out. Nothing about layer 1 or 2 needs it.

### Why the page is Go templates, and why that is settled

The tick already computes every value the UI shows, freshly, every fifteen seconds, by one pure
function. The server is the state manager. A client framework means a second state model mirroring
the first over a JSON wire, and the second one goes stale.

React SSR is out because it needs a JavaScript runtime at request time, which means Node beside Go,
which ends the single binary and the single process. Astro is out for the same reason plus one more:
its static generation cannot prerender per-request derived state like "PLA-12 running, fourteen
minutes elapsed". Both become worth revisiting only if the loop moves off the laptop, and that is a
different question with a different answer (see [Out of scope](#out-of-scope)).

Islands rather than hydration, when layer 3 comes. Hydration needs Go templates and JSX to emit
byte-identical DOM, so every view is written twice and kept in sync forever. Islands mount into empty
divs that Go never renders inside, so there is no parity to maintain.

### Two rules that keep the split honest

**One derivation.** Any JSON endpoint an island calls serves the same view struct the template
renders. `plan.Unlocked` and `plan.Status` are called in exactly one place. No unlock logic in
TypeScript, ever.

**The board stays HTML.** Islands are for direct manipulation only, the pointer-and-frame kind. If a
proposed island turns out to be a form, it is a form.

### Verbs cannot be optimistic

A POST writes an `intents` row. The loop applies it on the next tick, up to fifteen seconds later.
This is deliberate (v1 §8, only the loop writes) and it is what makes `cancel` race-free against a
launch. It caps what any amount of frontend can do for a verb: an acknowledgement swap is the
ceiling, and pretending otherwise would show state the database does not hold.

### Every island container needs `hx-preserve` and a stable id

htmx swapping any ancestor of an island destroys and remounts it, losing a half-panned graph or a
mid-search palette. This is the failure mode that bites people who combine the two, and it is an
ongoing constraint on every swap target in Part B, not a one-off.

### No new tables

`internal/cc/schema.sql` is version 1 and creates every table Phases 1 to 6 need, deliberately,
because there is no migration code and `OpenStore` refuses a version mismatch. Later phases add
rows, not tables. Anything in this doc requiring a new table bricks every existing developer
database, which is most of why the inbox and prompt editing are out of scope below.

## Part A: the working page

**Lands inside product Phase 1**, alongside GitHub issues #4 to #7. It is a requirement of Phase 1's
own stated exit, not an addition to it.

**Requirements.**

- A checkbox on every authorisable row and one submit that navigates to `/preview`.
- `/preview` renders HTML from the same rows the JSON path builds: `now`, `on unlock` or `refused`,
  the base, the reason, and a refused row naming the blocker outside the slice. `[ authorise ]`
  POSTs to `/launch` with the hash the page rendered.
- One POST route per v1 §5 verb reachable in Phase 1: `launch`, `cancel`, `kill`, `re-run`,
  `retry push`, `close PR`, `remove worktree`. Each behind `requireBrowserOrigin`, each writing an
  `intents` row, each redirecting back to the board. `refresh`, `abort` and `re-check` belong to
  states Phase 1 cannot reach, so they are not built.
- Which verb buttons a row shows comes from the state, as a method on the state enum, so v1 §5's
  table has exactly one implementation.
- A row with an unconsumed intent shows the queued verb beside its state.
- Waiting rows render grouped under the blocker they wait on, keyed on `plan.Unlock.Blocking`, with
  the blocker's state on the group line and each row keeping its own reason.
- Stack order renders as depth, merge-first at the top of its group.
- `<meta http-equiv="refresh" content="5">`.
- Worktree path, pgid, base and elapsed time as plain selectable text on every row that has them.
- Embedded CSS, enough to make depth and grouping legible. No framework.
- The golden file covers the grouped layout and every verb button's presence per state.

**Nice to haves.** Elapsed time as a live counter, which needs JavaScript and therefore waits for
Part B. The run log tailed inline rather than a path to paste, same. Both are already listed as
Phase 1 nice-to-haves in `prd-command-centre.md` and neither has moved.

**Out of scope for Part A.**

- **Row detail expansion.** Needs a fetch, so it needs htmx. Part B.
- **Anything that survives a full page reload badly.** There is nothing, by construction, which is
  what makes the meta refresh correct rather than lazy.

## Part B: the operator surface

**Lands after product Phase 3.** By then stacking, fan-out, `max_agents > 1` and seams exist, which
is when a dozen rows with cross-repo edges stop fitting in a table you read top to bottom.

**Requirements.**

- **htmx enters here**, and its trigger is the first island, not a date. Before an island exists the
  meta refresh covers liveness and there is nothing a full reload breaks. htmx becomes correct at
  the moment a full refresh would destroy something mounted.
- Row detail via `hx-get="/task/{id}/detail"` into a sibling row: log tail, check list, base SHA,
  elapsed, worktree path.
- Table refresh on `hx-trigger="every 5s"`, replacing the meta refresh and leaving islands mounted.
- Tick age and last tick error via `hx-swap-oob`, so one response updates disjoint regions.
- Run log tailing over the SSE extension, `sse-swap` appending into a `<pre>`.
- The dependency graph as the first island: pannable, click to build a slice. Continuous pointer
  state with no server round trip per frame, which is the test an island has to pass.
- Every island container carries `hx-preserve="true"` and a stable `id`.
- `web/`, Vite, `internal/cc/assets/dist/` embedded, and `just build` running the JS build first.

**Nice to haves.** A keyboard-driven command palette, fuzzy over the task list. It passes the island
test on client-side filter state and sub-frame latency, and it is worth nothing at a dozen rows. It
becomes worth building when the board no longer fits on a screen.

**Out of scope for Part B.**

- **A rich diff viewer.** Start diff hunks as htmx expansion. Promote to an island only if it grows
  inline comments and syntax highlighting, and not before.
- **The Preact against React decision.** Roughly 3 KB against 40 KB, and none of the candidate
  islands need the React ecosystem. Decide it when the graph is actually being written. The one real
  input is that shadcn is React-only, so it matters only if a component library ever does.

## Dependencies

| Thing | State |
|---|---|
| Product Phase 1, issues #4 to #7 | Part A's verb set assumes `running`, `checking`, `needs you`, `failed` and `push failed` are reachable, which they are not until spawn, push and verdict land. Part A's board and preview can be built against the states that exist today; the verb routes want the states to be real |
| Product Phases 2 and 3 | Part B only. Stacking, fan-out and seams are what make the graph worth drawing |
| Node and Vite on the build machine | Part B only, and only for layer 3. A contributor building Part A needs neither |

Nothing outside this repo blocks either part.

## Out of scope

Each with a re-entry trigger, in the style of v1 §12.

- **The inbox.** A `waiting on you` state, the question and reply as rows, answering resumes the
  run. It is a UI surface, and its cost is not UI. It is the app's first tool grant, so invariant 17
  flips from a denylist to an explicit allowlist on the same change, and it needs tables that schema
  v1 does not have in a database with no migration path. The v1 §12 row stands with its own trigger:
  an agent guesses wrong, or a `failed` run turns out to have been an unanswerable question, more
  than once.
- **Editing a prompt or picking skills in the app.** The composed prompt is `/implement
  <ticket_url>` plus each seam file's content (`internal/plan/compose.go:13`). Nothing in v1 makes it
  authorable, and making it so needs a per-task override column, which schema v1 does not have. It
  also moves planning into the control plane, and v1 §1 puts planning in the planning skills.
  Re-enter when you edit the composed prompt by hand more than twice, and then as a stored override
  with its own hash, not as a free textarea.
- **Optimistic verb responses.** Structurally impossible while only the loop writes. Re-enter never,
  or when that invariant changes, which would be a different design.
- **A TUI.** v1 §12 already holds this, with the trigger "the HTTP page is demonstrably the wrong
  shape". Part A is most of the evidence either way.
- **Moving the loop off the laptop.** It reads like a UI decision and is not one. Remote the loop and
  `kill(-pgid, 0)` stops working, which is the app's most defensible mechanism, and v1 §12 says
  multi-machine is "never, different product, different auth". If that changes it gets its own
  decision rather than drifting in through a frontend choice.
- **Slack or OTel egress.** v1 §12 holds it. Tick age and last error on the page are the Phase 1
  answer.

## Open questions

**Product**

- **Does the board group by blocker, or sort by it?** Grouping reads better for a fan-out and worse
  for a flat list of unrelated tasks, which is what the board looks like most of the time today.
  Worth settling in a prototype against real row counts before building. Leaning: group, because the
  case the product doc names is the fan-out.
- **Does a verb need a confirmation step?** `kill` and `remove worktree` destroy work, and both sit
  one click away on a machine that also runs a browser. Invariant 18 is a defence against another
  origin, not against you. Leaning: a confirm on those two only, as a second page, because an
  in-place confirm is layer 2 and Part A is layer 1.
- **Where does intake come from in Phase 1?** Still open from `prd-command-centre.md`, and it is now
  a UI question too. A hand-inserted row, a file the app reads, or a form. `POST /tasks` is deferred
  in v1 §12 with the trigger "you want to add a ticket without re-running `to-tickets`".

**Design**

- **How much CSS.** A grouped table with depth needs some. The failure mode is discovering a design
  system halfway through Part A. Leaning: one embedded stylesheet under a hundred lines, no
  framework, and no component library until an island needs one.
- **What a `refused` preview row looks like.** It is the line that stops an afternoon of wasted agent
  work, and "refused, blocked by PLA-09 which has no open or merged pull request outside this slice"
  is accurate and long.

<details>
<summary><b>Resolved</b></summary>

- **Go templates, React SSR, Astro, a TUI or Electron?** Go `html/template`. The tick is the state
  manager and a client framework means a second state model over a JSON wire. React SSR and Astro
  both need a JavaScript runtime at request time, which ends the single binary.
- **Hydration or islands?** Islands. Hydration needs Go and JSX to emit byte-identical DOM, so every
  view is written twice forever. Islands mount into divs Go never renders inside, so there is no
  parity to maintain. htmx's own docs recommend embedding islands in a hypermedia app rather than
  the reverse.
- **When does htmx go in?** At the first island, not before. Until something is mounted, a full
  reload breaks nothing and a meta refresh is one line.
- **Is the preview and authorise flow an htmx swap?** No, and never. Consent binds to a hash of what
  the operator previewed (v1 §4b), which wants a real navigation and one audit point, not an
  in-place swap that can partially fail.
- **Does the board itself become an island?** No. Islands are for direct manipulation only. Keeping
  the board in HTML is also what keeps every layout change testable by golden file, which is worth
  more than it sounds.

</details>
