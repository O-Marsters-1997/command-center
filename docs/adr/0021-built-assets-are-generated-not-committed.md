# 21. Built assets are generated, not committed

**Date:** 2026-10-11 · **Status:** accepted

## Context

The keyboard model (`docs/plans/keyboard-and-search.md`) needs one script that presses whatever
carries a `data-key`. Hand-written JavaScript would put untyped, untestable logic (platform detection,
chord timing, field guards) in the one file every page loads, outside Biome, `tsc` and `bun test`.

Everything in `internal/web/assets/dist` is derived: the stylesheet from the templates plus
`web/app.css`, the scripts from `web/src`. A committed copy is a second source of truth that conflicts
in every stacked PR and drifts with the toolchain: the same source builds differently under a
different bun or `solid-js` resolution.

## Decision

- The dispatcher is `web/src/keys.ts`, strict TypeScript, with pure logic (key to binding, chord state
  machine, target resolution) separate from the DOM listener and unit-tested like `layout.ts`.
- Vite builds `keys.js`, `graph.js`, `launch-modal.js` and their shared chunk, and Tailwind builds
  `app.css`, all into `internal/web/assets/dist` with hash-free names. `just assets` generates the
  directory and git ignores everything in it except `.gitkeep`.
- `just build`, `just run` and `just demo` depend on `assets`. Go tests that read a built file skip when
  it is absent, so `test`, `e2e` and `lint` stay Go-only.
- CI's `assets` job builds the directory, so a type error or a broken Tailwind scan fails CI.
- `data-key` stays the only contract between Go templates and the script (one binding per element,
  `mod+` for the platform modifier, a space for a chord).

## Consequences

A binary built with plain `go build` and no `just assets` serves 404s for `/assets/app.css` and the
scripts: it renders unstyled and has no keys or islands. Release builds must run `just assets` first.
Template and `web/src` edits touch no committed build output. Single-character shortcuts still owe WCAG
2.1.4 a way to switch them off; that is a separate decision.
