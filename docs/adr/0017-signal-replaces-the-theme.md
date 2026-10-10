# 17. Signal replaces the theme: light only, fonts vendored

**Date:** 2026-10-09 · **Status:** accepted · **Supersedes:** the dark-entry rule in `CLAUDE.md`

## Context

The owner approved the Signal design system (`command-center-design/design-system/`) for the new
session-rail UI. Signal is light only by the owner's choice, and its `tokens.css` imports Inter and
JetBrains Mono from Google Fonts. `web/app.css` carries a `[data-theme="dark"]` block, `layout.tmpl`
a theme toggle and a pre-paint theme script, and `CLAUDE.md` requires every colour token to have a
dark entry. The app makes no third-party request today and is reached from a phone through Caddy.

## Decision

- Signal's values replace the `@theme` block outright, as oklch tokens. There is one theme, light.
- The dark block, the theme toggle and the pre-paint theme script are deleted. A colour token no
  longer needs a dark entry.
- Inter (variable) and JetBrains Mono are committed as woff2 under `internal/web/assets`, embedded in
  the binary and declared with `@font-face`. No font package enters `web/package.json`, and no page
  requests a third-party origin.

## Consequences

Dark mode returns as one token block once a dark Signal is designed, since colour stays in tokens.
The repo carries roughly 400KB of font files. Pages render offline and over a tailnet exactly as on
the desk.
