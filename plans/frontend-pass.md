# Plan: the frontend pass

> Source: the single `Now` card in `ideas/roadmap.html`, accepted in `ideas/CONTEXT.md` on
> 2026-09-13. No PRD; this plan is the design document.

## The starting point

The roadmap carried "Migrate styling to Tailwind" and "UI redesign of Command Centre" as two
ongoing efforts. They are one, and the first of them is already half done.

`plans/fleet-view.md` § "Tailwind, and what Go is allowed to emit" settled the policy before any of
the fleet view shipped:

> Templates use utilities for layout and spacing. The state grammar — pill, ribbon, meter, phase
> header — stays a handful of semantic component classes, because Go emits class names dynamically
> and Tailwind's purge eats exactly that. **Go never returns a utility string.**

Fleet-view phases 1 to 6 shipped the half of that policy that Go depends on. `web/app.css` opens
with `@import "tailwindcss"`, an `@theme` block holding every colour token in oklch, and a
`[data-theme="dark"]` block overriding them. The state grammar is real: `plan.Tone` returns one of
five words and `board.tmpl` composes `class="pill pill-{{.Tone}}..."` from it.

The other half never happened. `@layer components` is 468 lines and about 90 selectors, and most of
it is layout and spacing written as bespoke classes: `.masthead`, `.band`, `.card`, `.legend`,
`.histogram`, `.view-toggle`, `.graph-viewport` and the rest. Every fleet-view phase added more.

So this is not a new decision. It is finishing a migration whose rules are already written down,
and doing the visual pass in the same effort rather than touching every template twice.

Three things turned up while reading the code that are not migrations at all:

- **The run log has never been styled.** `detail.tmpl` and `logline.tmpl` between them use
  `run-log`, `run-log-header`, `run-log-path`, `run-log-meta`, `run-log-live`, `run-log-result`,
  `log-filters`, `jump-first-fail`, `active`, `phase`, `phase-skill`, `phase-at`, `line`,
  `line-<kind>` and `line-label`. Of those, `@layer components` defines `.phase-header` and nothing
  else. The rendered run log that shipped in #107 is an undifferentiated block of text.
- **`preview.tmpl` has no stylesheet link.** It renders as raw unstyled HTML, and it sits on the
  launch path between selecting a slice and authorising it. `confirm.tmpl` and `import.tmpl` link
  the sheet but carry no classes at all, so they get base element styling only.
- **Ten selectors in `@layer components` are dead against the templates** because the graph island
  emits them from `web/src/graph.tsx` instead. They are live, but they live somewhere else, and any
  sweep that greps templates alone will delete them by mistake.

---

## Technical design decisions

### The policy is promoted from a plan to an ADR

The rule currently lives in a shipped plan file, which is where decisions go to be forgotten. It
now governs ongoing work, so it becomes `docs/adr/0008-utilities-for-layout-grammar-for-state.md`,
joining ADR 1 (serve the stylesheet) and ADR 2 (islands opt out of shadow DOM), both of which it
depends on.

The ADR states the rule and the reason Tailwind's purge forces it: Go composes class names from
derived state at render time, and a purge that scans source files cannot see a string Go builds at
runtime. It does not restate the migration; it states the rule that outlives it.

### What counts as state grammar, and what does not

The dividing line is whether Go chooses the class. This is the whole of it, and every phase applies
it mechanically:

**Stays a named class** (Go composes it, or a token feeds it through `--tone`):

`pill`, `pill-disc`, `pill-ring`, `pill-pulse`, `pill-{done,live,wait,stop,idle}`, the
`[data-tone="…"]` attribute mapping, `ribbon`, `meter`, `meter-fill`, `segbar-segment`, `banner`,
`flag`, `flag-warning`, `phase-header`, `line`, `line-{skill,file,tool,fail,pass}`, `line-label`.

**Becomes utilities in the template**:

everything else, including `skip-link`, `masthead`, `workspace`, `theme-toggle`, `view-toggle`,
`band`, `card`, `headline`, `empty`, `segbar` (the container, not its segments), `legend`,
`histogram`, `ticket-link`, `reason`, `verbs-cell`, `detail`, `title`, `group-head`, the
`data-depth` indent rules, and the `graph-*` family.

Two rules need care rather than a straight conversion:

- **The depth indent.** `tr[data-depth="1"] td:nth-child(3)` through `="4"` set `padding-left` in
  1.5rem steps. Go knows the depth, so this is state, but it is expressed as a CSS attribute
  selector rather than a class. It stays in the sheet, as an attribute selector, and the ADR names
  it as the one exception: a depth is an integer, not one of five words, so composing a class per
  depth would need Go to emit `pl-6`, which the policy forbids outright.
- **`.verbs-cell`.** Its opacity transition is driven by `tr:hover`, `tr:focus-within` and
  `tr.selected`, three parent states. Tailwind's `group-hover:` and `group-focus-within:` cover the
  first two from the template; `tr.selected` needs a `group-[.selected]:` variant. Convert it, but
  it is the one conversion that gets harder rather than easier, so verify it by hand.

### Convert first, redesign last

Phases 1 to 5 change no pixels on purpose. Each converts one region to utilities, deletes the rules
it replaced, and proves the region renders identically. Phase 6 is the visual pass over the whole
surface at once.

This costs a second pass over each template. It buys two things that matter more here. A conversion
diff is reviewable against the goldens, because any visual change is a bug by definition, and a
redesign that follows sees the entire app in one consistent idiom instead of restyling regions that
have not been converted yet. It also gives a clean stopping point: if the redesign is deferred,
phases 1 to 5 still leave the codebase better than they found it.

### The visual pass aims to be replaced

A fresh Claude Design pass is expected after this plan lands. Phase 6 therefore fixes hierarchy,
density, spacing, contrast and focus, and does not build bespoke components or elaborate motion.
The test for phase 6 is whether a later redesign can be done by editing utility strings in eight
template files, without reading any CSS. Every rule that goes back into `@layer components` works
against that, so phase 6 adds none.

### Tests: what churns, and what must not

Sixteen `class="…"` assertions across seven files in `internal/cc`, plus six golden HTML files
regenerated with `go test ./internal/cc -update`.

The goldens churn freely; that is what `-update` is for, and the diff is the review. The
assertions split cleanly along the policy line, which is a useful check that the line is in the
right place:

- **Must not change.** `pill_test.go` asserts `class="pill pill-live pill-disc pill-pulse"`,
  `class="pill pill-idle pill-ring"` and `class="pill pill-wait pill-ring"`. Those are state
  grammar. If a phase makes one of these fail, the phase has converted something it should not
  have.
- **Changes with its phase.** `shell_test.go` asserts `<span class="workspace">fleet-hq</span>`
  (phase 1). `band_page_test.go` asserts `class="headline"` three times (phase 2).
  `group_page_test.go` asserts `class="group-head"` twice and `launch_cancel_test.go` asserts
  `class="ticket-link"` and `class="title"` (phase 3). `detail_test.go` asserts
  `class="active" aria-current="true"` and `<a href="#first-fail" class="jump-first-fail">`, and
  `logstream_test.go` asserts `class="line line-tool"` and `class="line-label"` (phase 4).

Where an assertion exists only to find an element, replace the class hook with the semantic anchor
the test actually cares about: `aria-current="true"`, `href="#first-fail"`, the element's text. A
test that asserts a utility string is a test that breaks on every restyle, which is exactly the
churn this plan is trying to end.

### The build does not change

`just assets` runs `bun install && bun run build` in `web/`, which is `tailwindcss -i ./app.css -o
../internal/cc/assets/dist/app.css --minify`, then `tsc --noEmit`, then `vite build`. The built
`app.css` is committed and CI diffs it, exactly as `plans/fleet-view.md` § "The build, and how Go
stays independent of it" set up. Go stays independent of Node.

One consequence worth stating because it will bite otherwise: **utilities used only in Go templates
must be visible to Tailwind's source scanning.** Tailwind v4 scans the project by default, so
`internal/cc/*.tmpl` is already covered, but the first phase must confirm this rather than assume
it. If it is not covered, `@source "../internal/cc"` in `app.css` fixes it, and phase 1 owns that
check.

### The run log is not a migration

Phase 4 writes styling that has never existed. Under convert-first, its job is structure and
legibility only: the log should read as phases containing lines, with the label, the tool and the
detail distinguishable, and the first failure findable. Colour, density and rhythm land in phase 6
with everything else. The kind classes (`line-fail`, `line-pass`) are state grammar and stay named,
so they get tone hooks here and their actual colours in phase 6.

### Module boundaries

No Go changes. No schema changes. No route changes. No new dependencies.

This plan touches `web/app.css`, the eight files in `internal/cc/*.tmpl`, `web/src/graph.tsx`, the
class assertions in seven `_test.go` files, the six goldens in `internal/cc/testdata/`, and adds one
ADR. The derivation layer (`internal/plan`), the store, and every verb are out of scope entirely.

### Out of scope

- **Expanding islands.** The roadmap card for it correctly depends on this one. Phase 6 will
  surface which regions are genuinely stateful; converting them is a separate effort.
- **sqlc.** Unrelated, and still carrying two open questions on the roadmap.
- **Redesigning the launch flow.** `preview`, `confirm` and `import` keep their terse bracket look
  (`[ authorise ]`, `[ cancel ]`). Phase 5 makes them legible and consistent, not designed.
- **Replacing htmx or changing the polling model.** The board's five-second `hx-swap="outerHTML"`
  and the `hx-preserve` detail row stay exactly as they are, and phase 3 must not disturb them.

---

## Phase 1: the shell converts, and the policy becomes an ADR

**Covers**: the masthead, the stale banner, the skip link, the view toggle, the theme toggle.

### What to build

Write `docs/adr/0008-utilities-for-layout-grammar-for-state.md` stating the rule from
`plans/fleet-view.md`, the purge reason behind it, and the `data-depth` exception. Reference ADR 1
and ADR 2.

Confirm Tailwind's source scanning reaches `internal/cc/*.tmpl`. Build with a utility that appears
only in a template and check it lands in the output sheet. Add `@source "../internal/cc"` to
`app.css` if it does not.

Convert the shell region in `page.tmpl` to utilities, then delete `.skip-link`, `.masthead`,
`.masthead h1`, `.workspace`, `.theme-toggle`, `.view-toggle` and `.view-toggle a` from
`@layer components`. `.banner` and `.ribbon` stay: both consume `--tone`.

Replace the `class="workspace"` hook in `shell_test.go` with an assertion on the workspace text in
its element.

This is the smallest region that exercises every part of the approach, so anything wrong with the
policy shows up here rather than in the middle of the board.

### Acceptance criteria

- [ ] `docs/adr/0008-*.md` exists, is accepted, and states the rule, the purge reason and the
      `data-depth` exception
- [ ] A utility used only in a `.tmpl` file appears in the built `internal/cc/assets/dist/app.css`
- [ ] The seven shell selectors are gone from `@layer components`
- [ ] `.banner` and `.ribbon` remain
- [ ] `shell.golden.html` regenerated; the diff shows class attributes changing and nothing else
- [ ] `go test ./...` passes, `pill_test.go` untouched
- [ ] The masthead renders identically: same layout, same spacing, both themes

---

## Phase 2: the band converts

**Covers**: the four analytics cards, the segbar, the legend, the histogram.

### What to build

Convert `band.tmpl` to utilities. `.band`, `.card`, `.card h2`, `.card .headline`, `.card .empty`,
`.segbar`, `.legend`, `.legend li`, `.histogram` and `.histogram li` all go.

`.segbar-segment` stays, because it reads `--tone` from `data-tone`, and the inline `flex-grow` Go
writes per segment stays inline; it is a computed number, not a class.

`.meter` and `.meter-fill` stay named and are left alone here, though the histogram uses them. They
belong to phase 3's region by ownership but are shared, so phase 2 must not delete them.

Update the three `class="headline"` assertions in `band_page_test.go` to assert the headline text
instead.

### Acceptance criteria

- [ ] The ten band selectors are gone from `@layer components`
- [ ] `.segbar-segment`, `.meter` and `.meter-fill` remain
- [ ] `band_page_test.go` asserts content, not class names, and passes
- [ ] The band renders identically in both themes at desktop width, including the empty states for
      all four cards
- [ ] `go test ./...` passes

---

## Phase 3: the board converts

**Covers**: the table, rows, group heads, depth indent, the ticket link, the reason line, the verbs
cell, the detail row's container.

### What to build

The largest conversion. `board.tmpl` and the container half of `detail.tmpl`.

Convert `.ticket-link`, `.reason`, `.group-head`, `td.detail dl`, `td.detail dt`, `td.detail dd`
and `td.detail pre` to utilities.

Convert `.verbs-cell` to `group-hover:`, `group-focus-within:` and a `group-[.selected]:` variant,
with `motion-reduce:` replacing the hand-written `prefers-reduced-motion` block. Verify by hand
that hovering a row, tabbing into it, and selecting it each reveal the verbs.

Keep the `tr[data-depth]` padding rules and the `tr.group-head td:nth-child(3)` border as attribute
selectors in the sheet, per the ADR exception. Keep `.pill` and its whole family untouched. Keep
`.flag` and `.flag-warning`, both state. Keep `.meter` and `.meter-fill`.

Leave every `hx-` attribute, the `id="board"` target and the `hx-preserve` detail row exactly as
they are.

Replace the `class="group-head"` hooks in `group_page_test.go` and the `class="ticket-link"` and
`class="title"` hooks in `launch_cancel_test.go` with assertions on the row content they are
identifying.

### Acceptance criteria

- [ ] The seven board selectors are gone from `@layer components`
- [ ] The `data-depth` rules, the pill family, `flag`, `flag-warning`, `meter` and `meter-fill`
      remain
- [ ] The verbs cell reveals on hover, on focus-within and on selection, and does not animate under
      `prefers-reduced-motion: reduce`
- [ ] `pill_test.go` passes unchanged
- [ ] Four board goldens regenerated; diffs show class attributes only, with every `hx-` attribute
      byte-identical
- [ ] A selected row's detail still survives the five-second board swap
- [ ] `go test ./...` and the e2e suite pass, including `row_detail.txtar` and `board_route.txtar`

---

## Phase 4: the run log gets structure

**Covers**: the rendered run log in the detail panel, its filters, its phases, its lines, and the
jump to first failure.

### What to build

Styling for fifteen classes that have never had any. Not a conversion; there is nothing to delete
except `.phase-header`, which is converted alongside the rest.

Give the log a readable structure with utilities in `detail.tmpl` and `logline.tmpl`: the header
row with its path, streaming pill and filters; the meta line; phases as visually separated blocks;
lines as a monospace list where the label, tool and detail are distinguishable.

`line`, `line-{skill,file,tool,fail,pass}` and `line-label` stay named. Give the kind classes their
`--tone` hooks now and leave the actual colour choices to phase 6.

Confirm the SSE tail still appends into `.run-log-live` and that the 1000-line trim in `page.tmpl`
still fires.

Replace the `class="active"` and `class="jump-first-fail"` hooks in `detail_test.go` with the
`aria-current="true"` and `href="#first-fail"` anchors already in those assertions.
`logstream_test.go` keeps asserting `line line-tool` and `line-label`, which are state grammar.

### Acceptance criteria

- [ ] `.phase-header` is the only selector removed; the fifteen log classes are styled
- [ ] `line-*` and `line-label` are still named classes and `logstream_test.go` passes unchanged
- [ ] `detail_test.go` finds the active filter by `aria-current` and the jump link by `href`
- [ ] A run log renders as distinguishable phases and lines, with the first failure reachable by
      the anchor
- [ ] The SSE tail appends live and trims past 1000 lines
- [ ] `go test ./...` passes

---

## Phase 5: the island, the orphan pages, and the sweep

**Covers**: the graph island, `preview.tmpl`, `confirm.tmpl`, `import.tmpl`, and the proof the
migration is complete.

### What to build

Convert the `graph-*` family in `web/src/graph.tsx` to utilities. TSX takes utility strings more
naturally than Go templates do, and ADR 2 means these rules are page-global, so they leave
`@layer components` with everything else. `cc-graph { display: block }` stays, since the custom
element needs it before Solid upgrades. The Go-rendered fallback banner inside `<cc-graph>` uses
`.banner` and `.ribbon`, which are staying anyway.

Add the missing `<link rel="stylesheet" href="/assets/app.css">` to `preview.tmpl`. Give all three
pages enough utilities to be legible and consistent with the board: readable table, clear headings,
visible buttons. Keep the bracket look. `[ authorise ]` and `[ cancel ]` stay as they are written.

Then the sweep. `@layer components` should now contain only the state grammar listed in the ADR,
plus the two attribute-selector exceptions. Anything else left in it is either a missed conversion
or a rule nothing uses; resolve each one before the phase closes.

### Acceptance criteria

- [ ] `@layer components` contains only: the pill family, `ribbon`, `meter`, `meter-fill`,
      `segbar-segment`, `banner`, `flag`, `flag-warning`, the `line-*` family, the `[data-tone]`
      mapping, the `data-depth` rules, the `group-head` border and `cc-graph { display: block }`
- [ ] The graph pans, zooms, selects and lights edges exactly as before
- [ ] `preview.tmpl` serves with a stylesheet and is legible
- [ ] All three launch-path pages keep their bracket labels
- [ ] `just assets` builds clean and CI's asset diff is empty after committing the built sheet
- [ ] `go test ./...` and the e2e suite pass

---

## Phase 6: the visual pass

**Covers**: the whole surface, now in one idiom.

### What to build

The redesign. Everything before this changed no pixels; this phase changes only pixels.

Work in the browser against a running app with real fleet data, not against static markup. Fix, in
rough priority: the information hierarchy of the board, so state and ticket read before the
supporting columns; the density of the band against the board, which currently compete; spacing
rhythm, which is ad hoc per region because it accumulated per feature; contrast in both themes,
against the oklch tokens already in `@theme`; and focus treatment, which is a single global
`:focus-visible` outline today.

Extend `@theme` with whatever spacing, radius and type-scale tokens the design needs, so the next
pass has more than colour to work from.

**Add nothing to `@layer components`.** If a pattern wants a component class, it is either state
grammar, in which case it already exists, or it is a repetition that a later redesign will want to
break apart, in which case the repetition is cheaper than the abstraction.

Close with an accessibility and responsive check: keyboard path through the board, focus visible on
every interactive element, reduced-motion honoured by the pulse and the verbs transition, and no
horizontal scroll on a narrow window.

### Acceptance criteria

- [ ] `@layer components` has grown by zero selectors against phase 5
- [ ] New tokens live in `@theme` and both theme blocks stay in sync
- [ ] The board can be restyled by editing utility strings in the templates, with no CSS file open
- [ ] Every interactive element shows a visible focus ring; the board is fully keyboard navigable
- [ ] The pill pulse and the verbs transition stop under `prefers-reduced-motion: reduce`
- [ ] No horizontal scroll at 1024px; the band wraps rather than overflowing
- [ ] Both themes pass contrast on body text, muted text and every tone against its background
- [ ] `go test ./...` and the e2e suite pass; goldens regenerated once at the end
