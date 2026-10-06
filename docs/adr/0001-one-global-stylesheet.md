# 1. One global stylesheet: served, shared with the islands, utilities for layout and grammar for state

**Date:** 2026-08-26 · **Status:** accepted

## Context

The compiled Tailwind sheet is far larger than the 84-line one `page.tmpl` used to inline. Inlined,
it lands in `testdata/page.golden.html` and buries layout diffs. `/assets/` already serves htmx.

`solid-element` wraps each island in a shadow root by default. Custom properties cross that boundary
but stylesheet rules do not, so islands would need a second sheet and the shared grammar authored
twice. Shadow DOM also hides content from htmx. Nothing on the page is third-party.

Tailwind purges class names not written literally in scanned source, and Go composes some class names
at render time (`"pill pill-" + tone`). Those must be named classes already in the sheet.

## Decision

- The compiled stylesheet is served from `/assets/app.css`; golden files carry markup only.
- Every island calls `noShadowDOM()` first, so one global sheet styles Go- and Solid-rendered markup.
- Templates use utilities for layout and spacing. The state grammar stays named classes in
  `@layer components`: `pill` (with `pill-disc`, `-ring`, `-pulse` and the tones `-done`, `-live`,
  `-wait`, `-stop`, `-idle`), `ribbon`, `meter`, `meter-fill`, `segbar-segment`, `banner`, `flag`,
  `flag-warning`, the `line` family, and the `[data-tone]` mapping.
- Go never returns a utility string. `plan.Tone` returns one of five words and the template composes
  the class.
- `data-depth` indent rules stay CSS attribute selectors: a depth is an integer, and a class per depth
  would mean Go emitting `pl-6`.

## Consequences

A pill is byte-identical whether Go or Solid drew it. Island styles are page styles and can collide;
reverting means a sheet per island, which gets harder with each island. Revisit only if untrusted
markup ever mounts on the page. A new piece of derived state needs a new named class first.
