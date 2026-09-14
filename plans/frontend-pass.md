# Plan: the frontend pass — phases 5 and 6

> Source: the single `Now` card in `ideas/roadmap.html`, accepted in `ideas/CONTEXT.md` on
> 2026-09-13. No PRD; this plan is the design document.

## What already landed

Phases 1 to 4 shipped: the shell (#169), the analytics band (#170), the board (#171), the launch
path pages (#174) and the run log's first styling (#172). The policy became
[ADR 7](../docs/adr/0007-utilities-for-layout-grammar-for-state.md), and `graph.tsx`'s `graph-*`
family converted in #173.

What is left: issue #158 (the `@layer components` sweep, the tail of phase 5) and issue #159 (the
visual pass).

## The rule everything here obeys

Go composes class names from derived state at render time, and Tailwind's purge cannot see a string
built at runtime. So **Go never returns a utility string**, and the state grammar stays a small set
of named classes. ADR 7 is the statement of record.

**Stays a named class**: `pill`, `pill-disc`, `pill-ring`, `pill-pulse`,
`pill-{done,live,wait,stop,idle}`, the `[data-tone="…"]` attribute mapping, `ribbon`, `meter`,
`meter-fill`, `segbar-segment`, `banner`, `flag`, `flag-warning`, `phase-header`, `line`,
`line-{skill,file,tool,fail,pass}`, `line-label`.

**Two attribute-selector exceptions** stay in the sheet rather than becoming classes. The
`data-depth` indent rules, because a depth is an integer rather than one of five words, so a class
per depth would force Go to emit `pl-6`. And the `group-head` border.

Everything else is a utility in the template.

### The visual pass aims to be replaced

A fresh Claude Design pass is expected after this plan lands. Phase 6 therefore fixes hierarchy,
density, spacing, contrast and focus, and does not build bespoke components or elaborate motion.
The test for phase 6 is whether a later redesign can be done by editing utility strings in eight
template files, without reading any CSS. Every rule that goes back into `@layer components` works
against that, so phase 6 adds none.

### Tests: what churns, and what must not

The six goldens in `internal/cc/testdata/` churn freely; that is what `go test ./internal/cc
-update` is for, and the diff is the review.

`pill_test.go` must not change. It asserts `class="pill pill-live pill-disc pill-pulse"`,
`class="pill pill-idle pill-ring"` and `class="pill pill-wait pill-ring"`, which are state grammar.
If a phase makes one of these fail, the phase has converted something it should not have.

Where a remaining assertion exists only to find an element, replace the class hook with the
semantic anchor the test actually cares about: `aria-current="true"`, `href="#first-fail"`, the
element's text. A test that asserts a utility string breaks on every restyle, which is the churn
this plan is trying to end.

### The build does not change

`just assets` runs `bun install && bun run build` in `web/`, which is `tailwindcss -i ./app.css -o
../internal/cc/assets/dist/app.css --minify`, then `tsc --noEmit`, then `vite build`. The built
`app.css` is committed and CI diffs it. Go stays independent of Node.

### Module boundaries

No Go changes. No schema changes. No route changes. No new dependencies.

What is left touches `web/app.css`, `internal/cc/*.tmpl`, the class assertions still standing in
`internal/cc`, and the goldens. The derivation layer (`internal/plan`), the store, and every verb
are out of scope entirely.

### Out of scope

- **Expanding islands.** The roadmap card for it depends on this one. Phase 6 will surface which
  regions are genuinely stateful; converting them is a separate effort.
- **sqlc.** Unrelated, and planned separately in `plans/sqlc-migration.md`.
- **Redesigning the launch flow.** `preview`, `confirm` and `import` keep their terse bracket look
  (`[ authorise ]`, `[ cancel ]`).
- **Replacing htmx or changing the polling model.** The board's five-second `hx-swap="outerHTML"`
  and the `hx-preserve` detail row stay exactly as they are.

---

## Phase 5: the sweep

**Covers**: proving the migration is complete. Issue #158.

### What to build

`@layer components` should now contain only the state grammar listed in ADR 7, plus the two
attribute-selector exceptions and `cc-graph { display: block }`, which the custom element needs
before Solid upgrades. Anything else left in it is either a missed conversion or a rule nothing
uses; resolve each one before the phase closes.

Ten selectors were emitted from `web/src/graph.tsx` rather than from a template, so a sweep that
greps templates alone would delete live rules by mistake. #173 converted that family, but check the
island before deleting anything, not just `internal/cc/*.tmpl`.

### Acceptance criteria

- [ ] `@layer components` contains only: the pill family, `ribbon`, `meter`, `meter-fill`,
      `segbar-segment`, `banner`, `flag`, `flag-warning`, the `line-*` family, the `[data-tone]`
      mapping, the `data-depth` rules, the `group-head` border and `cc-graph { display: block }`
- [ ] The graph pans, zooms, selects and lights edges exactly as before
- [ ] All three launch-path pages keep their bracket labels
- [ ] `just assets` builds clean and CI's asset diff is empty after committing the built sheet
- [ ] `go test ./...` and the e2e suite pass

---

## Phase 6: the visual pass

**Covers**: the whole surface, now in one idiom. Issue #159.

### What to build

The redesign. Everything before this changed no pixels; this phase changes only pixels.

Work in the browser against a running app with real fleet data, not against static markup. Fix, in
rough priority: the information hierarchy of the board, so state and ticket read before the
supporting columns; the density of the band against the board, which currently compete; spacing
rhythm, which is ad hoc per region because it accumulated per feature; contrast in both themes,
against the oklch tokens already in `@theme`; and focus treatment, which is a single global
`:focus-visible` outline today.

**Leave the launch preview cheap to change.** `docs/designs/cloud-agents.md` §5 and §7 put two new
controls on that screen if any of it is built: the local-or-cloud choice for the slice, and the
shared-context note that folds into `prompt_hash`. The design is a sketch and may never be built,
but a preview page designed tightly around today's two fields is a page that gets redesigned twice.

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
