# 16. Built assets are generated, not committed

**Date:** 2026-10-11 · **Status:** accepted

## Context

Everything in `internal/web/assets/dist` is derived: the stylesheet from the templates plus
`web/app.css`, the scripts from `web/src`. A committed copy is a second source of truth that conflicts
in every stacked PR and drifts with the toolchain: the same source builds differently under a
different bun or `solid-js` resolution.

Logic that runs on every page (the keyboard dispatcher's platform detection, chord timing and field
guards) belongs inside Biome, `tsc` and `bun test`, not in hand-written JavaScript.

## Decision

- Client code is strict TypeScript in `web/src`, with pure logic separate from the DOM listener and
  unit-tested. The keyboard dispatcher is `web/src/keys.ts`; page-wide glue (relative times, the
  rail cookie, the current-link marks, the home page's desktop redirect) is `web/src/glue.ts`.
- Templates carry no inline `<script>`, `on*` attribute, `hx-on` or JavaScript trigger filter. A
  template hands data to the glue through `data-*` attributes, and htmx runs with `allowEval:false`.
- Vite builds the scripts and their shared chunk, and Tailwind builds `app.css`, all into
  `internal/web/assets/dist` with hash-free names. `just assets` generates the directory and git
  ignores everything in it except `.gitkeep`.
- `just build`, `just run` and `just demo` depend on `assets`. Go tests that read a built file skip
  when it is absent, so `test`, `e2e` and `lint` stay Go-only.
- CI's `assets` job builds the directory, so a type error or a broken Tailwind scan fails CI.
- `data-key` is the only contract between Go templates and the dispatcher (one binding per element,
  `mod+` for the platform modifier, a space for a chord).

## Consequences

A binary built with plain `go build` and no `just assets` serves 404s for `/assets/app.css` and the
scripts: it renders unstyled and has no keys, islands, live times or desktop redirect from `/`. Release builds run `just assets` first.
Template and `web/src` edits touch no committed build output. Single-character shortcuts still owe
WCAG 2.1.4 a way to switch them off.
