# The fleet view

**Status:** WIP · **Date:** 2026-08-26 · **Shape:** change doc

**Reads with:** [`prd-operator-surface.md`](prd-operator-surface.md) (the doc this extends, whose
Part B it replaces), [`prd-command-centre.md`](prd-command-centre.md) (the product doc),
[`command-centre-design.md`](../designs/command-centre-design.md) (revision 4, **normative** for the
state list in §5, the config and schema in §8, and the invariants in §9),
[`CONTEXT.md`](../../CONTEXT.md) (the glossary this doc's vocabulary comes from),
[ADR 1](../adr/0001-serve-the-stylesheet-instead-of-inlining-it.md) and
[ADR 2](../adr/0002-islands-opt-out-of-shadow-dom.md).

**Prototype:** `~/open-design/.od/projects/5e8080cc-b58d-4208-b583-b821ab76e616/command-center.html`.
Open it before reading further. Everything below is either transcription of that file, an argument
for departing from it, or a data problem it does not know it has.

**Not a number.** Three phase numberings are already live in this repo. This doc uses none of them.
It has five parts, numbered 1 to 5, and they mean nothing outside this doc.

## TL;DR

The board works and cannot be read. Ten verbs, eighteen states and a grouped layout all ship today,
rendered as nineteen monospace columns of `https://github.com/O-Marsters-1997/command-center/issues/33`
with a raw JSON blob where the log should be. This doc makes it legible: the prototype's design
system, ticket titles, state pills that say who a row is waiting on, a parsed run log, what each run
cost, and the dependency graph as the first Solid island.

## Contents

- [Current state](#current-state)
- [Why does this matter?](#why-does-this-matter)
- [Goal](#goal)
- [Proposed experience](#proposed-experience)
- [What we already have (reuse)](#what-we-already-have-reuse)
- [Important technical details](#important-technical-details)
- [Phases](#phases)
- [Dependencies](#dependencies)
- [Out of scope](#out-of-scope)
- [Open questions](#open-questions)

## Current state

The operator surface landed. `POST /verb` accepts all ten verbs, the preview is a page you read, the
hash you previewed travels to authorise, kill and remove-worktree sit behind a confirm page, the
board groups by blocker, htmx swaps it every five seconds, and a run's log tails over SSE into an
expanded row. Nine routes, eighteen states, one derivation feeding both the board and the detail
fragment. The plumbing is done.

What it looks like is `page.css`, eighty-four lines: a monospace stack, collapsed borders, two
pixels of cell padding, and `td:empty::after { content: "–" }` as the entire empty-state design.

Four things are wrong with what you actually see, and they are different kinds of wrong.

**Every row is a URL.** `tasks` holds `ticket_url` and nothing else, so the widest column on the
board is twenty-five near-identical GitHub URLs differing in their last two characters. The
prototype's most prominent text on a row is the ticket title, and the app has never known one.

**The log is raw JSON.** `agent_command` runs `claude -p --output-format stream-json --verbose`, so
`runs/<id>.jsonl` is genuine Claude Code event JSON. `logstream.go` HTML-escapes each line and wraps
it in a `<div>`. Run 27 is 1,174 lines, of which 604 are `system` hook chatter, and the detail panel
shows you all of them, unparsed, in a `<pre>`.

**Nothing tells you who a row is waiting on.** Eighteen states render as eighteen bare lowercase
words in a `<td>`. `queued` and `ready` look identical, and one of them is the loop's problem while
the other is yours. This is the fact the board exists to communicate and it is the fact the board
communicates worst.

**You cannot see what any of it cost.** The `result` event on every finished run carries
`total_cost_usd`, `num_turns` and `duration_ms`. Run 27 cost **$8.29** across 129 turns. The app
writes that number to disk and never reads it. For a tool whose premise is running three agents at
once, this is the most useful fact it currently throws away.

## Why does this matter?

**A control plane you cannot read is a control plane you do not press.** The operator-surface doc
argued that verbs stuck in a terminal put your attention back into the loop. The same argument
applies one step later: a verb you cannot find is a verb in a terminal. The buttons exist. Nineteen
columns of undifferentiated monospace is where they went.

**The grouping work is already done and currently invisible.** `plan.Unlock.Blocking` groups four
`queued` rows under one `failed` blocker, exactly as the product doc demanded, and the layout that
communicates it is a three-pixel left border and some `padding-left`. The derivation shipped in #84.
The reading of it did not.

**The ring-and-disc distinction is the highest-value thing in the prototype, and it generalises.**
The design draws waiting-on-you as a hollow ring and waiting-on-the-loop as a filled disc. It
implements this as a hand-written set of five state names, and gets two of them wrong for this
codebase: it calls `base_moved` yours when `autoRefresh` sweeps it every tick, and `merged` the
loop's when only you can remove the worktree. The rule underneath is real and derivable — *will the
loop move this without me?* — and once it is a method on the state enum it answers "what needs me"
across all eighteen states at a glance, and across the nineteenth for free.

**Honest about the bet.** This phase adds one new capability and one new derivation. Ticket titles
are the capability; the log parser is the derivation. Everything else is re-expression of what
already ships, and it will not make the loop drive a single ticket it could not drive last week.
That is the whole point. The expensive thing in this product was never throughput, it was the
attention between agents, and attention is spent on reading.

## Goal

**You look at the board and know what needs you, without expanding anything.** One countable thing:
the number of rows you have to open to find the one that is stuck, target zero. Today it is however
many rows are on the board, because the only way to tell `queued` from `ready` is to read the reason
column, and the only way to find out why a run died is to expand it and read JSON.

## Proposed experience

### The shell

One header, full width. The brand, the workspace name, a live-agent count, an observe chip and a
theme toggle. No project rail: you run one repo with twenty-five tasks and no seams, so a rail would
render one entry.

The observe chip is the only thing on the page that separates a quiet loop from a dead one. Under
twenty seconds it reads `observed 2s ago`, quietly. Past twenty it turns amber, relabels to
`last good observe`, and a banner opens beneath the header naming what failed and saying outright
that nothing below has been re-derived since. The current page prints `Last successful observe: 3m`
as ordinary prose at any age.

Theme follows the system by default and the toggle overrides it, persisted in `localStorage`. It
lives on `<html>`, outside every swap target, so nothing can knock it out.

### The board

Eight columns, down from nineteen.

| Column | What's in it |
|---|---|
| ☐ | the launch checkbox, on rows that can be authorised |
| state | the pill, plus any queued verb and any flag marks |
| ticket | `#33` |
| task | the title, with the reason sentence above it |
| stack | `L1 ← #33`, with merge order folded in |
| checks | a per-check meter and `3/4` |
| pr | `#4118`, or `no pr` |
| elapsed | a clock and a bar proportional to the longest run |
| spend | tokens while alive, dollars once ended |
| verbs | at most three, dimmed until the row is hovered, focused or selected |

**The state pill carries three things at once.** Colour says how healthy the row is, across five
tones. Shape says who it is waiting on: a filled disc means the loop advances it unattended, a
hollow ring means it is parked until you act. A pulse means a process is alive right now. So
`running` is a pulsing blue disc, `needs_you` a red ring, and `merged` a green ring — because merged
looks finished and the loop will never clear it.

**Flags become marks, not columns.** Seam-changed, draft, base verdict and the ready-to-merge
warning sit as small marked glyphs beside the pill, each with a `title`, and their full prose moves
to the detail panel. Nothing safety-relevant leaves the row: invariant 2's hazard — a non-`main`
based PR carrying `ready-to-merge` would squash into its parent with the parent's checks unseen — is
visible while you scan, which is the only time it can save you.

**Grouping wins over sorting.** Column heads are labels. A blocker and the rows waiting on it render
as one group, children ordered by merge order, and `Children[0]` is what you merge next. Sorting by
elapsed would tear that apart, and merge order is information rather than a preference.

**A row expands in place.** Selecting one opens the detail beneath it, full width, the way the
prototype draws it. Only one is ever open.

### The analytics band

Three cards above the board, plus a fourth, all counted off the current tick and none of them
storing anything.

- **State of the fleet** — a stacked ribbon by state with a legend, headline `n/N` for how many rows
  are yours.
- **Stack depth** — a histogram by depth, headline the deepest stack.
- **Gating checks** — green against red, and how many branches have reported at all.
- **Spend** — total across completed runs this session, count, failures and an average.

Each has a written empty state rather than a blank frame. A project with no worktree cut reads
`no worktree has been cut`, not `—`.

### The run log

The detail panel's log stops being a JSON dump and becomes the run, read as a narrative.

```
runs/27.jsonl                    ● streaming        all  skills  tools  fails
556 lines · 4 phases                                          [ first failure ]

PHASE 02   skill tdd · writing the failing test first                    +06:52
  +08:35         + internal/cc/derive_test.go
  +10:18   TOOL  Bash  go test ./internal/cc -run Derive
  +12:01   FAIL  FAIL  TestDeriveGroups/blocker_with_four_dependents
  +13:44   FAIL        expected 1 group, got 4
  +15:27   TOOL  Edit  internal/cc/server.go
  +18:53   PASS  ok    internal/cc  0.412s
─────────────────────────────────────────────────────────────────────────────
success · 17m 11s · 129 turns · $8.29
```

Phases cut at each `Skill` tool use. Timestamps are real, off the first event's own `timestamp`.
Filters are four server round trips, which on loopback is imperceptible. `system`,
`rate_limit_event` and `thinking` are dropped, which is roughly sixty per cent of the file.

### The graph

A second view of the same data, at `/?view=graph`, and the first Solid island. Tasks as nodes in
depth columns, `blocked_by` and seam edges as curves, the selected node's chain lit up. Pan and zoom
are continuous and do no round trip, which is the test an island has to pass. Clicking nodes builds
a selection; submitting it navigates to `/preview` with the same query string the board's checkboxes
produce, so the graph is a second way to *select* a slice and never a second way to *launch* one.

## What we already have (reuse)

| Exists | This doc's use of it |
|---|---|
| `derive` and `pageView.Groups` | every value on every row and card. The band counts states, depths and checks off rows already derived per render; nothing new is computed |
| `plan.Verbs(State)` | the verbs cell. §5's verb column keeps exactly one implementation, and `Unattended()` attaches beside it |
| `plan.Unlock.Blocking` | the grouping, shipped in #84. This doc restyles it, it does not re-derive it |
| `row.Checks` | the per-row check meter. Already on the row struct, currently rendered only in the detail fragment |
| `gh.IssueBody` and the `gh` runner | the pattern the bulk title read follows. `internal/gh` stays the only place that knows the CLI's JSON shape |
| `Observation` in the `meta` table | titles ride in as a new field on a value that is already a persisted JSON blob. No schema change, and a restart still renders them |
| `runs.log_path` and the SSE tail | the log source and its resume-by-byte-offset semantics, which are correct and unchanged. Only the parse and the render are new |
| `<tr id="detail-…" hx-preserve="true">` | the detail slot, and the mechanism that keeps a mounted island alive through a board swap |
| `GET /assets/` over `go:embed` | where the stylesheet and the island bundle are served from. htmx and the SSE extension already come from here |
| `testdata/*.golden.html` | the render test. Every layout change in this doc is a diff to a golden file |

**Net-new.** `Observation.Titles` and a bulk `gh issue list` read. A `State.Unattended()` method. A
stream-json parser producing typed log events and run phases. Spend, summed live from
`message.usage` and read settled from `result`. A Tailwind entry point and `@theme` token block. A
`GET /board` fragment route and a `GET /graph.json` route. `web/`, Vite, `solid-element`, and one
island.

## Important technical details

### The vocabulary

Four terms this doc introduces, defined properly in [`CONTEXT.md`](../../CONTEXT.md).

- **Unattended** — a property of a state: the loop advances it without you. Disc against ring.
- **Island** — a Solid component compiled to a custom element. Never inside a swap target.
- **Run phase** — a span of a run's log, cut at a skill boundary. Always qualified, because this
  repo already has two other senses of "phase".
- **Spend** — tokens while a run is alive, dollars once it ends.

One rename: the prototype heads its title column `WORKTREE`, and `row.Worktree` is a filesystem
path. Two things, one word. The column is **task**; the worktree is the path, always.

### What swaps, and what must not

This is the load-bearing constraint of the whole architecture, and getting it wrong is silent.

```mermaid
flowchart TB
    subgraph page["GET / — the shell, rendered once"]
        hdr["header · observe chip · theme<br/>never swapped"]
        graph["&lt;cc-graph&gt; — island<br/>fetches /graph.json, polls itself"]
        band["analytics band"]
        subgraph board["#board — hx-get /board, every 5s"]
            rows["group rows · pills · verbs"]
            det["&lt;tr hx-preserve&gt; detail<br/>survives the swap"]
        end
    end
    board -.->|"swap disposes<br/>everything inside"| rows
    graph -.->|"outside the target,<br/>never disposed"| graph
```

A custom element removed from the DOM fires `disconnectedCallback` and Solid disposes it; inserted,
it fires `connectedCallback` and mounts fresh. That is the whole appeal — the browser runs the
lifecycle, so there is no `MutationObserver`, no htmx event hook and no glue. It is also why a
half-panned graph inside `#board` would reset every five seconds.

**The rule: an island is never a descendant of a swap target.** One grep in a test enforces it.

The one nuance worth writing down. htmx 2.0.7 implements `hx-preserve` as an atomic move where the
browser supports it:

```js
if (e.moveBefore) { /* stash in a pantry, moveBefore back */ }
else { e.parentNode.replaceChild(n, e) }
```

`Node.moveBefore()` preserves state and fires no lifecycle callbacks. Its absence re-parents, which
does. So a preserved island keeps its state on Chrome and Edge 133+, and resets every five seconds
on Safari and Firefox. The app serves 127.0.0.1 on your own laptop, so this is a bounded risk rather
than a bug, but it is the reason the rule above has no exceptions the graph could hide behind.

### The poll narrows

`hx-get="/" hx-select="#board" hx-swap="outerHTML"` re-renders the entire document — inlined
stylesheet and all — three times per fifteen-second tick, to swap one table, twice producing bytes
identical to the last. `GET /board` returns the table alone. It carries the current view parameters
in its own `hx-get`, so the next swap perpetuates them.

### View state lives in the URL

`/?sel=33&view=graph` is the only view state. The server renders the selection expanded; the swapped
fragment carries the parameters forward. Bookmarkable, back-button correct, survives a reload, works
with JavaScript off, and needs no client state and no re-application after a swap. `localStorage`
holds theme and nothing else, because theme lives on `<html>` where no swap reaches it.

### One derivation, still

`GET /graph.json` marshals the same `[]group` the board template ranges over — json tags on the
existing `row`, no bespoke shape. `plan.Unlocked`, `plan.Status` and `State.Unattended()` are called
in one place each. No unlock logic in TypeScript, ever. The island computes layout, which is
geometry rather than derivation.

### Spend, honestly

`total_cost_usd` exists only on the `result` event, at the very end of a run, so a cost column would
be empty for exactly the rows you are watching. Every `assistant` event carries `message.usage`, so
tokens accrue live and need no price table. The column therefore reads tokens while a run is alive
and settles to dollars when it ends. No model pricing map in Go, and nothing to maintain when
pricing changes.

One trap: run 27 carries **two** `result` events with different `duration_ms` and the same
`total_cost_usd`, almost certainly a subagent's followed by the main one. Take the last.

### Tailwind, and what Go is allowed to emit

The prototype's `:root` block ports verbatim into a Tailwind v4 `@theme` block, so every token is
both a CSS variable and a utility. Templates use utilities for layout and spacing. The state grammar
— pill, ribbon, meter, phase header — stays a handful of semantic component classes, because Go
emits class names dynamically and Tailwind's purge eats exactly that. Go never returns a utility
string. The stylesheet is served from `/assets/app.css` rather than inlined, so golden files hold
markup only ([ADR 1](../adr/0001-serve-the-stylesheet-instead-of-inlining-it.md)).

### Islands and shadow DOM

`solid-element` uses a shadow root by default. Custom properties inherit through the boundary;
stylesheet rules do not. Every island calls `noShadowDOM()` as its first statement, so one global
sheet styles Go-rendered and Solid-rendered markup alike and a state pill has one definition
([ADR 2](../adr/0002-islands-opt-out-of-shadow-dom.md)).

### The build, and Go's dependency on it

`//go:embed assets/dist` is a compile error when the directory is absent, which would make all five
CI jobs and every local `go test` depend on a JS build. So `assets/dist/.gitkeep` is committed and
the directive is `//go:embed all:assets/dist`. Node enters the `build` job only; test, e2e and lint
stay Go-only. `<cc-graph>` carries light-DOM fallback content, which a custom element that never
upgrades still renders, so a Go-only build degrades to a message rather than an empty box.

## Phases

Dependency order. Each is one issue and each is demoable on its own.

### Phase 1 — the design system and the shell

**Requirements.** Tailwind v4 with the prototype's oklch tokens in `@theme`, light and dark. Served
from `/assets/app.css`; the `<style>` block goes. `page.tmpl` gains a real `<html>`, `<head>` and
`<body>`, which it currently has none of, and `data-theme` on the root. The header: brand,
workspace, live count, observe chip with the twenty-second stale threshold, theme toggle. The stale
banner. Golden files regenerated as markup-only and split per fragment.

**Nice to haves ✨.** The skip link and focus-visible rings the prototype already carries.

### Phase 2 — titles and the redesigned board

**Requirements.** `gh issue list --json number,title --limit 100` per repo per tick, onto
`Observation.Titles`. `State.Unattended()` beside `plan.Verbs()`, table-tested across all eighteen
states. Eight columns as tabled above. State pills with tone, shape and pulse. Flags as marks, prose
demoted to detail. Verbs cell revealed on hover, focus and selection; launch stays a real checkbox.
Grouping preserved and restyled. View state in the URL. `GET /board`, and the poll narrows onto it.
The empty state gains the prototype's written copy.

**Proposed flow.** Selecting a row sets `?sel=`, which the server renders expanded and the fragment
carries forward. Every verb form is unchanged: still POST-only, still behind `requireBrowserOrigin`,
still non-optimistic, still showing the queued verb beside the state until a tick consumes it.

**Nice to haves ✨.** `j`/`k`/enter row navigation. The elapsed clock ticking between swaps.

### Phase 3 — the analytics band

**Requirements.** Four cards, counted off rows already derived. Written empty states on each. No new
derivation and no new storage.

### Phase 4 — the run log

**Requirements.** A stream-json parser in Go producing typed events and run phases. Five kinds,
phases cut at each `Skill`, real timestamps, tool lines rendered as name plus primary input,
`system` / `rate_limit_event` / `thinking` dropped. Filters as server round trips. Jump to first
failure. A closing result line. Spend on the row and in the band.

**Proposed flow.** The SSE tail keeps its byte-offset resume and its thousand-line cap; only the
parse and the render change. The panel stays the `hx-preserve`d `<tr>` it is today, so promoting it
to an island later is a tag swap in one template line.

**Nice to haves ✨.** Collapsing a run phase.

### Phase 5 — the graph island

**Requirements.** `web/`, Vite, `solid-element`, `noShadowDOM()`. Output to
`internal/cc/assets/dist/`, `go:embed all:`, `.gitkeep` committed, Node in the `build` job.
`GET /graph.json` serving the view struct verbatim. `cc-graph` outside `#board`, fetching on mount
and polling at five seconds. Nodes by depth, curved edges, chain highlight, click to select,
submit to `/preview`. A test asserting no custom element appears under a swap target.

**Nice to haves ✨.** Zoom to cursor. Keyboard focus between nodes.

### Out of scope for Phase 1

Everything in phases 2 to 5, in that order. Nothing here is held back for its own sake; the phases
are a dependency chain and phase 1 is the smallest thing that unblocks the rest.

## Dependencies

| Thing | State |
|---|---|
| Node and Vite | Phase 5 only. Phases 1 to 4 need neither, and the committed `.gitkeep` is what keeps that true |
| `--output-format stream-json` in `agent_command` | Phase 4. Already the case in the live config; the packaged default in `config.go` does not carry it and should |
| Issue #78, "the dependency graph as the first island" | Open. Phase 5 supersedes it — close it against this doc rather than building both |
| Issues #70, #74, #75, #77 | Open but their commits landed. Worth closing before this starts so the board is not planned against a stale ticket list |

Nothing outside this repo blocks any of it.

## Out of scope

Each with a re-entry trigger, in the style of the design's §12.

- **The project rail.** The prototype's six Linear projects with teams and target dates. You run one
  repo, twenty-five tasks and no seams, and neither the config nor the schema holds a project.
  Re-enter at a second repo, or when the board stops fitting on a screen.
- **Column sorting.** Structurally exclusive with grouping, and grouping is what the product doc
  argues for. Re-enter if a flat unrelated task list becomes the common case, which fan-out makes
  less likely rather than more.
- **The log viewer as an island.** Its filters are four buttons over 556 events and a round trip on
  loopback is imperceptible. Phase 4 builds it in a shape that promotes without moving. Re-enter
  when a run's log outgrows a server render, or when the tail wants interaction the server cannot
  answer between frames.
- **The command palette.** Passes the island test on fuzzy-filter latency and is worth nothing at
  twenty-five rows, which the operator-surface plan already said. Re-enter when the board no longer
  fits on a screen — the same trigger as the rail, and probably the same day.
- **A rich diff viewer.** Unchanged from the operator-surface doc: start as htmx hunk expansion,
  promote only if it grows inline comments and syntax highlighting.
- **A third pill shape for "waiting on someone else".** `waiting_on_producer_deploy` is genuinely
  neither yours nor the loop's, and it is one state out of eighteen. Re-enter when seams land and a
  second state qualifies.
- **Per-model cost calculation in Go.** A price table that drifts every time pricing changes, for a
  number `result` hands over free. Re-enter never, unless live dollar cost on a running agent turns
  out to matter more than live tokens.
- **The inbox, prompt editing, optimistic verbs, a TUI, moving the loop off the laptop, Slack and
  OTel egress.** All held by the operator-surface doc with their own triggers. Nothing here changes
  any of them.

## Open questions

**Product**

- **Does the spend column earn its width before the second repo?** Twenty-five tasks at roughly
  $4 to $8 a run is real money and currently invisible, which is the argument for. The argument
  against is that it reads `—` on every row that has not finished, which is most of the board most
  of the time. Leaning: ship it, and cut it if the column is mostly empty in practice.
- **What does a flag mark look like when a row carries three?** Seam-changed, draft and the
  ready-to-merge warning can co-occur. Three glyphs beside a pill is close to the point where they
  stop being scannable and become a puzzle. Worth settling against a real three-flag row before
  building phase 2.

**Design**

- **How does a group read once it exceeds about six children?** The prototype's deepest fan-out is
  three. `fan-out-stacked.toml` goes to five and the real config could go further, and at some
  count the group stops being one problem you can see and becomes a wall.
- **Does the pulse survive a five-second swap without flickering?** The animation restarts on every
  swap because the element is new. Likely imperceptible on a 1.9s cycle, and worth checking rather
  than assuming.

<details>
<summary><b>Resolved</b></summary>

- **Is the prototype the v1 spec?** No. It is normative for visual language — tokens, pill grammar,
  the seven-column table plus progressive disclosure, the band, the phased log, the graph, the
  keyboard model, the empty-state voice. It is not normative for which fields render, which states
  exist, or its own absence of verbs. It shows eleven states and zero buttons; the app has eighteen
  and ten.
- **Where do ticket titles come from?** A bulk `gh issue list` in observe, onto `Observation`, which
  is already a persisted JSON blob in `meta`. No schema change, survives a restart, and a failed
  tick shows the last good titles like everything else.
- **Tailwind or the prototype's own CSS?** Tailwind, with the prototype's tokens in `@theme` and the
  state grammar as semantic classes. Full utilities everywhere was rejected because Go emits class
  names dynamically and the golden files become unreadable.
- **Shadow DOM on islands?** No, `noShadowDOM()` on every one. ADR 2.
- **Inline the stylesheet or serve it?** Serve it. ADR 1 reverses the operator-surface plan's
  decision, because its premise was an eighty-four-line sheet.
- **Where does the island boundary sit?** Outside every swap target, no exceptions, enforced by a
  test. `hx-preserve` is real but browser-dependent, so it is not a boundary to design against.
- **How many islands in v1?** One. Log filters, phases, tinting and sorting are server renders;
  elapsed, theme and row navigation are a few lines of vanilla.
- **Who computes the graph layout?** Solid. The route serves the view struct verbatim, which is what
  plan phase 8 committed to; serving coordinates would break that for a table test.
- **Sorting or grouping?** Grouping. They are mutually exclusive layouts and merge order is
  information, not a preference.
- **Where does view state live?** The URL. No client state, no re-application after a swap, works
  with JavaScript off.
- **How does CI cope with a JS build?** Committed `.gitkeep`, `//go:embed all:`, Node in the `build`
  job only.

</details>
