# 7. Utilities for layout, grammar for state

**Date:** 2026-09-13 · **Status:** accepted

**Supersedes:** the styling policy in
[`docs/prds/prd-fleet-view.md`](../prds/prd-fleet-view.md) § "Tailwind, and what Go is allowed to
emit".

**Depends on:** [ADR 1](0001-serve-the-stylesheet-instead-of-inlining-it.md) (the compiled sheet
this rule governs) and [ADR 2](0002-islands-opt-out-of-shadow-dom.md) (the shared global grammar an
island must also honour).

## Context

The fleet view shipped half a styling policy. `plan.Tone` returns one of five words and `board.tmpl`
composes `class="pill pill-{{.Tone}}..."` from it, so the state grammar side of the rule is real. The
layout side never landed: `@layer components` grew to roughly ninety selectors as `.masthead`,
`.band`, `.card`, `.histogram` and the rest were added alongside each feature instead of written as
utilities.

The rule was only ever recorded in a shipped plan file, which is where decisions go to be forgotten
once the plan that stated them ships. It still governs every template touched from here on, so it
needs a home that outlives the plan.

The reason behind the rule is a Tailwind constraint, not a style preference. Tailwind's build scans
source files for class names and purges anything it does not find written literally. Go composes
some class names from state at render time — `"pill pill-" + tone` — so the composed name never
appears as a literal string anywhere Tailwind scans. A class Go is allowed to build this way must
already exist in the sheet under a name Tailwind can see, which means it cannot be a one-off
utility; it has to be a named component class the template selects between.

## Decision

Templates use Tailwind utilities for layout and spacing. The state grammar — the small set of
classes a template selects between based on runtime state — stays named component classes in
`@layer components`. Go never returns a utility string; it returns one of a fixed set of words, and
the template composes the class name from it.

The state-grammar classes are:

- **`pill`** and its modifiers: `pill-disc`, `pill-ring`, `pill-pulse`, and the tone classes
  `pill-done`, `pill-live`, `pill-wait`, `pill-stop`, `pill-idle`
- **`ribbon`**
- **`meter`** and **`meter-fill`**
- **`segbar-segment`**
- **`banner`**
- **`flag`** and **`flag-warning`**
- **`line`** and its kind modifiers `line-skill`, `line-file`, `line-tool`, `line-fail`,
  `line-pass`, plus **`line-label`**
- the **`[data-tone="…"]`** attribute mapping that feeds `--tone` to the classes above

Everything else a template needs for layout, spacing or one-off appearance is a utility string
written in the template, not a new selector in `@layer components`.

### Exception: `data-depth`

The board's indent rules (`tr[data-depth="1"]` through `="4"`, stepping `padding-left` by 1.5rem)
are state: Go knows the row's depth and the rule depends on it. They stay CSS attribute selectors
rather than named classes, because a depth is an integer, not one of five words. Composing a class
per depth would mean Go emitting a utility name such as `pl-6`, which the rule above forbids
outright. The attribute selector lets the sheet express depth-dependent state without Go ever
returning a utility string.

## Consequences

A conversion diff is reviewable: a template that composes only these named classes plus utility
strings can be checked against this list mechanically, without re-deriving the purge reasoning each
time. `@layer components` stops growing with every feature and instead holds only what state
genuinely requires. A future redesign works by editing utility strings in templates, not by adding
selectors to the sheet.

The cost is that a new piece of derived state needs a new named class before a template can use it,
rather than an inline utility. That friction is the point: it is the same purge constraint that
made the pill grammar necessary in the first place, just applied consistently instead of on a
case-by-case basis.
