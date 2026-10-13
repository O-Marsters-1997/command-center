# 1. One global stylesheet, light only, shared with the islands

**Date:** 2026-10-09 · **Status:** accepted

## Context

`solid-element` wraps each island in a shadow root by default. Custom properties cross that boundary
but stylesheet rules do not, so islands would need a second sheet and the shared grammar authored
twice. Shadow DOM also hides content from htmx. Nothing on the page is third-party.

Tailwind purges class names not written literally in scanned source, and Go composes some class names
at render time (`"glyph glyph-" + word`). Those must be named classes already in the sheet.

The Signal design system is light only, and the app is reached from a phone over a tailnet, so a page
must not depend on a third-party origin.

## Decision

- The compiled stylesheet is served from `/assets/app.css`.
- Every island calls `noShadowDOM()` first, so one global sheet styles Go- and Solid-rendered markup.
- Templates use utilities for layout and spacing. The state grammar is named classes in
  `@layer components`: the `glyph` family, `ribbon`, `meter`, `meter-fill`, `segbar-segment`,
  `banner`, `flag`, `flag-warning`, `avatar`, the `line-*` family, and the `[data-glyph]` and
  `[data-hue]` mappings. Go never returns a utility string ([ADR 14](0014-glyph-is-the-only-colour-key.md)).
- Colour is an oklch token in `@theme`. There is one theme, light, and a token has no dark entry.
- Inter and JetBrains Mono are woff2 files under `internal/web/assets`, embedded in the binary and
  declared with `@font-face`. No font package enters `web/package.json`.

## Consequences

A glyph is byte-identical whether Go or Solid drew it. Island styles are page styles and can collide;
a sheet per island gets harder with each island. Revisit only if untrusted markup ever mounts on the
page. A new piece of derived state needs a new named class first.

Dark mode, once a dark Signal exists, is one token block. The repo carries roughly 400KB of fonts,
and pages render offline exactly as on the desk.
