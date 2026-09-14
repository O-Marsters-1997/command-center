# Plan: the fleet view

> Source: [`docs/prds/prd-fleet-view.md`](../docs/prds/prd-fleet-view.md).
> Normative for the state list, the config and schema, and the invariants:
> [`docs/designs/command-centre-design.md`](../docs/designs/command-centre-design.md) revision 4,
> cited as `§n` and `inv. n`. Vocabulary:
> [`CONTEXT.md`](../CONTEXT.md). Decisions already recorded:
> [ADR 1](../docs/adr/0001-serve-the-stylesheet-instead-of-inlining-it.md),
> [ADR 2](../docs/adr/0002-islands-opt-out-of-shadow-dom.md).
>
> Predecessor: [`plans/operator-surface.md`](operator-surface.md), whose Part B this replaces. Its
> Go conventions, testing layers, on-disk layout and schema hold unchanged and are not restated.

**Not a phase number.** Three numberings are already live in this repo. This plan uses its own 1 to
6, and they mean nothing outside it. Phases 2 and 3 are the PRD's phase 2, split.

Goal, and it is countable: **zero rows opened to find the one that is stuck.** Today it is however
many rows are on the board.

## The starting point

The operator surface landed. Nine routes, eighteen states, ten verbs, one derivation feeding the
board and the detail fragment, htmx swapping the board every five seconds, a run's log tailing over
SSE. What is thin is everything above `derive`:

- `page.tmpl`, 85 lines. No `<html>`, `<head>` or `<body>` — it opens `<!doctype html>` and reaches
  `<h1>` four lines later. Nineteen `<th>`s, one flat `row` define.
- `page.css`, 84 lines, inlined through a `css` template func. Monospace, collapsed borders,
  `td:empty::after { content: "–" }`.
- `row` (`internal/cc/server.go:122`) carries 22 fields and no title, no liveness boolean, no spend.
- `logstream.go:81` HTML-escapes each `runs/<id>.jsonl` line and wraps it in a `<div>`.
- `#board` polls `hx-get="/" hx-select="#board"`, re-rendering the whole document three times per
  tick to swap one table.
- `testdata/page*.golden.html`, three files, each containing the whole inlined stylesheet.

## Technical design decisions

Durable across every phase. Where the PRD settles a concept, this carries it through rather than
re-deciding it.

### Routes

Nine today. This plan ends with ten: two added, one deleted.

| Route | Change |
|---|---|
| `GET /{$}` | the shell, rendered once. Nests the board fragment |
| `GET /board` | **new.** The `#board` table alone, carrying the view parameters forward in its own `hx-get` |
| `GET /graph.json` | **new.** `pageView.Groups` marshalled verbatim |
| `GET /task/{task}/detail` | **deleted** in phase 2. `?sel=` renders the detail inside the board, so this is a second render path for the same fragment |
| `GET /task/{task}/log` | unchanged offset-resume semantics; only the per-line render changes (phase 5) |
| `GET /assets/` | unchanged. Serves `app.css` and, from phase 6, the island bundle |
| `GET /preview`, `GET /events`, `GET /confirm`, `POST /launch`, `POST /verb` | unchanged |

Every mutating route keeps `requireBrowserOrigin`. Verbs stay POST-only and non-optimistic.

### View state lives in the URL

`/?sel=<ticket-url>&task=<ticket-url>&task=<ticket-url>&view=graph&log=fails`

| Parameter | Meaning |
|---|---|
| `sel` | the one expanded row. At most one; the first wins |
| `task` | repeated — the checked launch slice. **Exactly the field name `GET /preview` and `POST /launch` already take**, so the launch form needs no translation and the graph's own selection produces the same query string |
| `view` | `board` (default) or `graph` |
| `log` | `all` (default), `skills`, `tools`, `fails` — the selected row's log filter |

`localStorage` holds theme and nothing else, because theme lives on `<html>` where no swap reaches
it. There is no other client state and nothing to re-apply after a swap.

**The launch checkboxes join it.** They reset on every swap today. A checkbox toggle issues
`hx-get="/board?…&task=…"` with `hx-push-url`, and the server renders it `checked`, so a slice of
four survives the poll, a reload and the back button. This is the same mechanism as `sel`, not a
second one.

### The `hx-preserve` id is conditional on selection, and this is load-bearing

htmx reads `hx-preserve` off the **incoming** node: if an element with that id already exists, it
keeps the existing one and discards the incoming. Today every row renders an always-present empty
`<tr id="detail-…" hx-preserve="true">`, which works only because a separate route fills it.

Once the board renders the detail itself, that shape breaks silently: the click's freshly-rendered
detail would be discarded in favour of the empty `<tr>` already mounted.

**The rule: the detail `<tr>` is rendered only for the row named by `?sel=`.** Unselected rows
render no detail row at all. Then:

- selecting a row — no node with that id exists, so the incoming expanded detail lands
- the five-second poll while selected — the id exists, so the mounted node is kept and the SSE
  connection keeps its byte offset
- selecting a different row, or deselecting — the old id is absent from the incoming board, so the
  old detail is removed and the new one lands

Pinned by a table test over `(previously selected, now selected)` asserting which ids carry
`hx-preserve`.

### Templates split three ways

| File | Rendered by | Holds |
|---|---|---|
| `page.tmpl` | `GET /{$}` | `<html>`, `<head>`, header, stale banner, analytics band, `<form id="launch">`, `<cc-graph>`, and `<div id="board">` nesting the board |
| `board.tmpl` | `GET /{$}` and `GET /board` | the table, the `row` define, the detail `<tr>` for the selected row |
| `detail.tmpl` | nested in `board.tmpl` | the detail panel and the log |

The board fragment's own `hx-get` carries the current view parameters, so the next swap perpetuates
them without the shell being involved.

### Schema: no change

Version 1 holds. `OpenStore` still refuses a mismatch and there is still no migration code.

- **Titles** ride on `Observation`, which is already a persisted JSON blob in `meta`. A restart
  still renders them; a failed tick shows the last good ones like every other fact.
- **Spend** is derived at render from `runs.log_path`. No cost column, no `num_turns` column.
- **Selection and theme** are the URL and `localStorage`.

### `Observation.Titles`, and where the read lives

```go
// Titles is each tracked ticket's issue title, keyed by ticket URL — the board's widest column
// is 25 near-identical URLs without it.
Titles map[string]string `json:"titles"`
```

Filled in `NewObserver`, one call per configured repo per tick, after the PR read:

```go
// internal/gh — still the only place that knows the CLI's JSON shape.
func IssueTitles(ctx context.Context, repoPath string) (map[string]string, error)
```

`gh issue list --json number,title,url --limit 100`, keyed on **`url`**, not reconstructed from
`number` — a task holds the full ticket URL, so the URL is the honest join key and needs no
guessing about the repo's own host.

A failed read fails the tick like every other observe read, so the page falls back to the last good
observation. `row.Title` empty renders the ticket number alone, which covers a fresh DB and an
issue outside the 100-row limit.

### The row model

`row` gains eight fields and loses one. Every one is filled by the existing `derive`; nothing new
is computed anywhere else.

| Field | Source |
|---|---|
| `Title string` | `obs.Titles[t.TicketURL]` |
| `Alive bool` | `obs.Runs[t.TicketURL].Alive` — the pulse. Today liveness is inferred from `Elapsed` being non-empty, which is a formatted string standing in for a boolean |
| `Tone string` | `plan.Tone(state)` |
| `Unattended bool` | `state.Unattended()` |
| `ElapsedSeconds int` | replaces the formatted-only `Elapsed`; the string stays for display |
| `ElapsedPercent int` | second pass in `derive`, against the longest run on the board |
| `PRNumber int`, `PRState string` | replaces `PR string`, so the template can render `no pr` without parsing its own output |
| `SpendTokens int`, `SpendUSD float64`, `SpendSettled bool` | `internal/agentlog`, via the accumulator below |

`row.Ticket()` returns `#33` off the URL's last path segment — a method, not a stored field.

### Three decisions per state, all in `internal/plan`, all table-tested

`plan.Verbs(State)` set the precedent: which verbs a state offers is a decision, so it lives beside
`plan.Status` and is table-tested rather than spelled out per state in a template. Two more join it
in `internal/plan/verbs.go`:

```go
// Unattended reports whether the loop advances this state without you: the disc-against-ring
// question, and the one fact the board exists to communicate.
func (s State) Unattended() bool

// Tone is the state's health band, named for the prototype's own five tokens. Go never returns a
// utility class; this is one of five words the stylesheet knows.
func Tone(s State) string // "done" | "live" | "wait" | "stop" | "idle"
```

**Unattended, all eighteen.** Disc: `blocked`, `queued`, `running`, `push_pending`, `checking`,
`base_moved`, `waiting_on_producer_deploy`. Ring: `ready`, `failed`, `cut_failed`, `needs_you`,
`push_failed`, `review_me`, `merged`, `pr_closed_unmerged`, `base_gone`, `cancelled`,
`refresh_conflicted`.

Two of these depart from the prototype's hand-written five, and both departures are this codebase's
facts rather than taste. `base_moved` is a disc because `autoRefresh` (`internal/cc/refresh.go:145`)
sweeps every pushed, base-moved row each tick. `merged` is a ring because only you can remove the
worktree — `plan.Verbs(PRMerged)` returns exactly `remove-worktree`.

`waiting_on_producer_deploy` is a disc: the verdict re-evaluates every tick, so the loop does move
it without you. It is the one state a third shape would serve better, and a third shape is out of
scope until seams land and a second state qualifies.

**Tone, all eighteen.** `done`: `review_me`, `merged`. `live`: `running`, `push_pending`. `wait`:
`blocked`, `queued`, `checking`, `base_moved`, `waiting_on_producer_deploy`. `stop`: `failed`,
`cut_failed`, `needs_you`, `push_failed`, `refresh_conflicted`, `pr_closed_unmerged`, `base_gone`.
`idle`: `ready`, `cancelled`.

The table test enumerates all eighteen and fails on a new state, which is how the nineteenth gets
its ring and its tone for free.

**Verbs need no truncation.** `plan.Verbs` already returns at most three for every state
(`needs_you` is the only three). The table test asserts the cap rather than the template clipping it.

### `internal/agentlog`, the one new deep module

Stdlib only, so `Parse` is a table test over fixture files with no store, no server and no HTTP.
This is the package the PRD calls the phase's one new derivation.

```go
type Kind int // Skill, File, Tool, Fail, Pass

type Event struct {
    At     time.Duration // off the first event's own timestamp
    Kind   Kind
    Tool   string        // "Bash", "Edit", …
    Detail string        // the tool's primary input, or the line's own text
}

type Phase struct {          // a run phase — always qualified (CONTEXT.md)
    Skill  string
    Note   string
    At     time.Duration
    Events []Event
}

type Result struct {
    Outcome  string
    Duration time.Duration
    Turns    int
    CostUSD  float64
}

type Run struct {
    Phases []Phase
    Result *Result          // nil while the run is alive
    Lines  int
}

func Parse(r io.Reader) (Run, error)
func ParseLine(line []byte) (Event, bool)   // false for a dropped kind

// Accumulator is the incremental read spend needs: advance from a byte offset, never rescan.
type Accumulator struct{ /* offset, in/out tokens, settled *Result */ }
func (a *Accumulator) Advance(path string) error
func (a *Accumulator) Spend() (tokens int, usd float64, settled bool)
```

Rules the parser owns: phases cut at each `Skill` tool use; `system`, `rate_limit_event` and
`thinking` dropped (roughly sixty per cent of the file); timestamps relative to the first event;
tool lines rendered as name plus primary input; a trailing partial line is not an error.

**The two-`result` trap.** Run 27 carries two `result` events with different `duration_ms` and the
same `total_cost_usd`, almost certainly a subagent's followed by the main one. **Take the last.**
The fixture includes both, and the test asserts which one survives.

### Spend, and what it costs to compute

`total_cost_usd` exists only on `result`, at the very end, so a dollar column would be empty for
exactly the rows you are watching. Every `assistant` event carries `message.usage`, so tokens
accrue live and need no price table. The column reads tokens while a run is alive and settles to
dollars when it ends. No model pricing map in Go, ever.

Twenty-five log files re-scanned three times per fifteen-second tick is real CPU for a number that
only grows, so `internal/cc/spend.go` keeps one accumulator per log path and advances each from its
own byte offset — the same resume-by-offset shape `logstream.go` already proved. Once a run has
ended and a `result` has been seen the entry is frozen and never read again. A re-run's new log
path is a new key, so nothing needs invalidating.

```go
// ponytail: one mutex over the whole map. Per-path locks if 25 rows ever becomes 250.
type spendCache struct {
    mu sync.Mutex
    by map[string]*agentlog.Accumulator // keyed by log path
}
```

### Tailwind, and what Go is allowed to emit

The prototype's `:root` and `[data-theme=dark]` blocks port verbatim into a Tailwind v4 `@theme`
block — `--bg`, `--fg`, `--muted`, `--border`, `--s-done`/`-live`/`-wait`/`-stop`/`-idle` and the
rest, oklch and all — so every token is both a CSS variable and a utility.

Templates use utilities for layout and spacing. The state grammar — pill, ribbon, meter, phase
header — stays a handful of semantic component classes, because Go emits class names dynamically
and Tailwind's purge eats exactly that. **Go never returns a utility string.** `plan.Tone` returns
one of five words and the template composes:

```
class="pill pill-{{.Tone}}{{if .Unattended}} pill-disc{{else}} pill-ring{{end}}{{if .Alive}} pill-pulse{{end}}"
```

The sheet is served from `/assets/app.css`, not inlined, so golden files hold markup only
([ADR 1](../docs/adr/0001-serve-the-stylesheet-instead-of-inlining-it.md)). The `css` template func
and `page.css` both go.

### The build, and how Go stays independent of it

`//go:embed assets/dist` is a compile error when the directory is absent, which would make all five
CI jobs and every local `go test` depend on a JS build. So:

- `internal/cc/assets/dist/.gitkeep` is committed and the directive is `//go:embed all:assets/dist`
- **the built `app.css` is committed too**, the same trade the vendored `htmx.min.js` in
  `internal/cc/assets/` already makes. Otherwise a fresh clone serves a 404 for its own stylesheet
  and only the `build` job would notice
- a new `assets` CI job runs the build and `git diff --exit-code`s the output, mirroring the
  existing `tidy` job — that is what keeps a committed artifact honest
- Node enters `build` and `assets` only. `test`, `e2e` and `lint` stay Go-only

`<cc-graph>` carries light-DOM fallback content, so a Go-only build renders a message rather than an
empty box.

### Islands: the boundary, and the test

Phase 6 only, and one island. `noShadowDOM()` is every island's first statement
([ADR 2](../docs/adr/0002-islands-opt-out-of-shadow-dom.md)).

**An island is never a descendant of a swap target.** A custom element removed from the DOM fires
`disconnectedCallback` and Solid disposes it, so a half-panned graph inside `#board` would reset
every five seconds. `hx-preserve` mitigates this only where `Node.moveBefore()` exists — Chrome and
Edge 133+ yes, Safari and Firefox no — so it is not a boundary to design against.

The test is one line because the templates are already split: render `board.tmpl` alone and assert
no hyphenated tag name appears in the output.

### One derivation, still

`GET /graph.json` marshals the same `[]group` the board template ranges over. `row` and `group`
gain json tags; there is no bespoke view shape. `plan.Unlocked`, `plan.Status`, `plan.Verbs`,
`State.Unattended` and `plan.Tone` are each called in exactly one place. **No unlock logic in
TypeScript.** The island computes layout, which is geometry, not derivation.

Pinned by `testdata/graph.golden.json`, so the island's contract has a Go-side test.

### Testing

The existing layers hold. What changes:

- `internal/plan` — table tests for `Unattended` and `Tone`, exhaustive over all eighteen states
- `internal/agentlog` — table tests over `testdata/*.jsonl`, a trimmed real run included, covering
  the two-`result` trap, a truncated final line, and a run with no `result` yet
- `internal/cc` goldens split per fragment and regenerated markup-only:
  `shell.golden.html`, `board.golden.html`, `board_grouped.golden.html`, `board_verbs.golden.html`,
  `board_selected.golden.html`, `preview.golden.html`, `graph.golden.json`. `-update` unchanged
- e2e — one added script asserting `GET /board` returns the table and nothing above it

---

## Phase 1: the design system and the shell

**User stories**: "I look at the board and it reads as a designed surface." "I can tell a quiet loop
from a dead one." "The theme follows my system and I can override it."

### What to build

`web/app.css` with the prototype's oklch tokens in a Tailwind v4 `@theme` block, light and dark,
plus the semantic component classes the state grammar will need. Built to
`internal/cc/assets/dist/app.css`, committed, served from `/assets/app.css`. `page.css` and the
`css` template func are deleted.

`page.tmpl` gains a real `<html lang="en">`, `<head>` and `<body>`, and `data-theme` on the root
element — outside every swap target, so nothing can knock it out. A four-line inline script reads
`localStorage` before first paint to avoid a flash, and the toggle writes it back.

The header, full width: brand, workspace name, live-agent count, observe chip, theme toggle. No
project rail — one repo, twenty-five tasks, no seams, so a rail would render one entry.

`pageView` gains `ObserveStale bool` and `LiveAgents int`. A template cannot compare durations, so
staleness is decided in Go against a twenty-second threshold; `never` counts as stale. Under the
threshold the chip reads `observed 2s ago` quietly; past it, it turns amber, relabels to
`last good observe`, and the stale banner opens beneath the header naming what failed and saying
outright that nothing below has been re-derived since. The existing `#liveness` `<div>` and its
`Last successful observe:` prose go.

Goldens regenerated as markup only — all three currently carry the whole inlined stylesheet.

The board keeps its nineteen columns and its `hx-get="/"` poll. Nothing below the header changes.

The `assets` CI job lands here, since this is the first committed build output.

**Nice to haves ✨.** The skip link and `focus-visible` rings the prototype already carries.

### Acceptance criteria

- [ ] `GET /assets/app.css` serves the built sheet; `page.css` and the `css` func no longer exist
- [ ] the rendered page is a well-formed document with `<html>`, `<head>`, `<title>` and `<body>`
- [ ] `data-theme` sits on the root element, outside `#board`
- [ ] the theme toggle persists to `localStorage` and survives a reload with no flash of the wrong theme
- [ ] the observe chip reads quietly under twenty seconds and amber past it; `never` reads as stale
- [ ] the stale banner renders only when the last tick failed, and names the failure
- [ ] the live-agent count equals the number of rows with a live run
- [ ] every golden file contains markup only, no stylesheet
- [ ] `just ci` passes with no Node installed; the `assets` job catches a stale committed `app.css`

---

## Phase 2: `GET /board`, and view state in the URL

**User stories**: "The board swaps without re-rendering the whole document." "A row I expanded is
still expanded after a reload, and the back button works." "A slice I checked survives the poll."

No visual change. This is the plumbing phase 3 lands on, split out because the `hx-preserve`
interaction above is subtle enough to want its own goldens before eight columns sit on top of it.

### What to build

Split `page.tmpl` into `page.tmpl` + `board.tmpl` per the table above, and nest `detail.tmpl` in
`board.tmpl`. `GET /board` renders `board.tmpl` alone; the poll narrows onto it and the fragment's
own `hx-get` carries the view parameters forward.

`?sel=`, `?task=` and `?view=` land. `render` takes the parsed parameters, marks the selected row,
renders `checked` on every `task` the query names, and reads the log tail for the selected row only
— one file per render, not twenty-five.

The detail `<tr>` becomes conditional on `?sel=` and carries `hx-preserve` only there.
`GET /task/{task}/detail` and `handleDetail` are deleted; `tailLog` and `logStreamPath` move to the
board render unchanged.

The row's select control and every checkbox become `hx-get` on `/board` with `hx-push-url`, so the
URL and the DOM never disagree.

### Acceptance criteria

- [ ] `GET /board` returns the table and nothing above it; `GET /{$}` nests the same fragment
- [ ] the poll targets `/board`, and the swapped fragment carries `sel`, `task` and `view` forward
- [ ] `/?sel=<url>` renders that row expanded on a cold load, with no JavaScript involved
- [ ] `/?task=<a>&task=<b>` renders exactly those two checkboxes checked
- [ ] the launch form still posts `task` to `/preview` unchanged, and the previewed hash still travels
- [ ] only the selected row's detail `<tr>` exists, and only it carries `hx-preserve`
- [ ] a table test over `(previously selected, now selected)` pins which ids are preserved
- [ ] the SSE connection and its byte offset survive at least three consecutive board swaps
- [ ] selecting a second row removes the first row's detail
- [ ] `GET /task/{task}/detail` returns 404; no test still references it
- [ ] goldens split per fragment, `-update` unchanged

---

## Phase 3: titles, and the redesigned board

**User stories**: "Every row names its ticket instead of repeating a URL." "I can see who a row is
waiting on without reading it." "The verbs are there when I want them and out of the way when I
don't."

### What to build

`gh.IssueTitles` and the per-repo per-tick read onto `Observation.Titles`. `State.Unattended()` and
`plan.Tone()` beside `plan.Verbs()`, each table-tested across all eighteen states.

Nineteen columns become eight: ☐, state, ticket, task, stack, checks, pr, elapsed, spend, verbs —
spend rendering empty until phase 5 fills it.

The state pill carries three things at once: tone across five colours, shape for who it is waiting
on (filled disc unattended, hollow ring parked), and a pulse while a process is alive. So `running`
is a pulsing blue disc, `needs_you` a red ring, `merged` a green ring.

Flags become marks beside the pill, each with a `title`, their full prose demoted to the detail
panel: seam-changed, draft, base verdict, and invariant 2's ready-to-merge warning. **Nothing
safety-relevant leaves the row** — the warning is visible while you scan, which is the only time it
can save you.

Verbs are dimmed until the row is hovered, focused or selected, and revealed on all three. Launch
stays a real checkbox; destructive verbs stay behind `GET /confirm`; every other verb stays a
POST-only form behind `requireBrowserOrigin`, non-optimistic, showing the queued verb beside the
state until a tick consumes it.

Grouping is preserved and restyled — `groupRows` is untouched. Column heads are labels, not sort
controls: grouping and sorting are mutually exclusive layouts and merge order is information rather
than a preference. `Children[0]` is what you merge next.

`row.PR` splits into `PRNumber` and `PRState` so the template renders `no pr` without parsing its
own output. `ElapsedSeconds` and `ElapsedPercent` land, the latter in `derive`'s existing second
pass against the longest run on the board. The empty state gains the prototype's written copy —
`no worktree has been cut`, not `–`.

**Nice to haves ✨.** `j`/`k`/enter row navigation. The elapsed clock ticking between swaps.

### Acceptance criteria

- [ ] a task's issue title renders on its row; a title-less task renders its number alone
- [ ] a failed `gh issue list` fails the tick and the page shows the last good titles
- [ ] `Unattended` and `Tone` are each table-tested over all eighteen states and fail on a nineteenth
- [ ] `base_moved` is a disc and `merged` is a ring, each with the test naming why
- [ ] `plan.Verbs` returns at most three verbs for every state, asserted
- [ ] the board renders eight columns
- [ ] a live run's pill pulses; an ended run's does not
- [ ] all four flags render as marks with `title` text, and their prose appears in the detail panel
- [ ] the ready-to-merge warning is visible on the row without expanding it
- [ ] verbs are present in the markup at all times and revealed on hover, focus and selection
- [ ] a five-row fan-out still renders as one group, children in merge order
- [ ] every empty cell reads as written copy, and `td:empty::after` is gone
- [ ] Go emits no Tailwind utility class: `plan.Tone` returns one of five words

---

## Phase 4: the analytics band

**User stories**: "I can see the shape of the fleet without reading every row." "I know how deep the
stacks are and how many branches have reported."

### What to build

Four cards above the board, counted off `pageView.Groups` as already derived this render. No new
derivation, no new storage, nothing persisted.

- **State of the fleet** — a stacked ribbon by state with a legend, headline `n/N` for how many rows
  are yours, which is `!Unattended` summed
- **Stack depth** — a histogram by `StackDepth`, headline the deepest stack
- **Gating checks** — green against red off `row.Checks`, and how many branches have reported at all
- **Spend** — total across completed runs this session, count, failures and an average. Renders its
  empty state until phase 5 fills the numbers

Each card gets written empty-state copy, not a blank frame. The band sits in `page.tmpl`, outside
`#board`, so it re-renders on the shell's own load rather than every five seconds.

### Acceptance criteria

- [ ] four cards render above the board
- [ ] every number is countable off the rows in the same render — no second query, no stored total
- [ ] the "yours" headline equals the count of rows whose state is not unattended
- [ ] a project with no worktree cut reads `no worktree has been cut`, not `–`
- [ ] the ribbon's segments sum to the row count
- [ ] the band is outside `#board` and not part of the five-second swap

---

## Phase 5: the run log

**User stories**: "I can read what a run did without reading JSON." "I can jump to the first
failure." "I know what this cost."

### What to build

`internal/agentlog` per the contract above: five kinds, phases cut at each `Skill`, real timestamps
off the first event's own, tool lines as name plus primary input, `system` / `rate_limit_event` /
`thinking` dropped.

The detail panel renders the parsed run: a header naming the log path, streaming state, filter
buttons and a jump-to-first-failure control; phase headers with their skill and note; event lines;
and a closing result line — `success · 17m 11s · 129 turns · $8.29`.

Filters are `?log=` on the board route, four server round trips, which on loopback is
imperceptible. Jump-to-first-failure is a plain `<a href="#first-fail">` against an anchor on the
first `Fail` event. Zero JavaScript for either.

**One template renders a log line, and both callers use it.** `sendLines` parses each whole line
into an `agentlog.Event` and renders it through the same `logline` define the detail render uses, so
a streamed line and a server-rendered line are byte-identical and the log does not visibly change
shape at the streaming boundary. Dropped kinds are skipped rather than escaped. The offset-resume
semantics, the `Last-Event-ID` override, the `end` sentinel and the thousand-line cap are all
unchanged.

`spend.go` and the accumulator land. The spend column fills — tokens while alive, dollars once
ended — and the band's spend card fills with it.

`config.go`'s `defaultAgentCommand` gains `--output-format stream-json --verbose`. The live config
already carries them; the packaged default does not, and without them the parser has nothing to
parse.

The panel stays the `hx-preserve`d `<tr>` it is today, so promoting it to an island later is a tag
swap in one template line.

**Nice to haves ✨.** Collapsing a run phase.

### Acceptance criteria

- [ ] `agentlog.Parse` is table-tested over a real trimmed run, with no store, server or HTTP involved
- [ ] a run with two `result` events reports the **last** one's duration, turns and cost
- [ ] a truncated final line parses as the lines before it, not an error
- [ ] `system`, `rate_limit_event` and `thinking` never reach the render
- [ ] phases cut at each `Skill` tool use, headed by its skill name
- [ ] timestamps are relative to the first event and monotonic
- [ ] each of the four filters is a URL parameter, and a filtered panel survives a board swap
- [ ] jump-to-first-failure is an anchor and works with JavaScript off
- [ ] a streamed line and a server-rendered line for the same log line are byte-identical
- [ ] the spend column reads tokens while alive and dollars once ended
- [ ] no model pricing table exists anywhere in Go
- [ ] a log path's accumulator is read once after settling and never rescanned
- [ ] `defaultAgentCommand` carries `--output-format stream-json --verbose`

---

## Phase 6: the graph island

**User stories**: "I can see the dependency graph." "I can pan and zoom it without the page
flickering." "I can select a slice from the graph and preview it."

### What to build

`web/` with Vite and `solid-element`, output to `internal/cc/assets/dist/`, `//go:embed all:`, the
`.gitkeep` already committed, Node in the `build` and `assets` jobs only.

`GET /graph.json` serving `pageView.Groups` verbatim — json tags on `row` and `group`, no bespoke
shape, goldened as `testdata/graph.golden.json`.

`<cc-graph>` in `page.tmpl`, **outside `#board`**, with light-DOM fallback content. `noShadowDOM()`
as its first statement. It fetches on mount and polls itself at five seconds. Nodes laid out in
depth columns, `blocked_by` and seam edges as curves, the selected node's chain lit up. Pan and zoom
are continuous and do no round trip, which is the test an island has to pass.

Clicking nodes builds a selection; submitting navigates to `/preview` with the same `?task=` query
string phase 2 already gave the checkboxes, so the graph is a second way to **select** a slice and
never a second way to launch one.

`?view=graph` switches which of board and graph is shown. Issue #78 is closed against this doc
rather than built alongside it.

**Nice to haves ✨.** Zoom to cursor. Keyboard focus between nodes.

### Acceptance criteria

- [ ] `GET /graph.json` marshals the same `[]group` the board ranges over, goldened
- [ ] no unlock, status or verb logic exists in TypeScript; layout only
- [ ] `<cc-graph>` renders outside `#board`, and a test asserts no hyphenated tag appears in `board.tmpl`'s output
- [ ] every island calls `noShadowDOM()` first, and one global sheet styles a pill identically in Go- and Solid-rendered markup
- [ ] pan and zoom issue no network request
- [ ] a Go-only build with no Node renders the fallback message, and `go test ./...` passes
- [ ] a graph selection submits the same query string the board's checkboxes produce
- [ ] `?view=graph` survives a reload and the back button
