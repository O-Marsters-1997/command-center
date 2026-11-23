---
name: Command Centre
description: Signal, a light, quiet, dense control surface for a fleet of coding agents.
colors:
  canvas: "oklch(0.968 0.002 90)"
  surface: "oklch(0.995 0.001 90)"
  surface-2: "oklch(0.955 0.002 90)"
  surface-3: "oklch(0.925 0.003 90)"
  fg: "oklch(0.22 0.004 270)"
  fg-2: "oklch(0.38 0.004 270)"
  muted: "oklch(0.5 0.005 270)"
  meta: "oklch(0.6 0.005 270)"
  border: "oklch(0.905 0.003 90)"
  border-soft: "oklch(0.935 0.002 90)"
  border-strong: "oklch(0.85 0.004 90)"
  accent: "oklch(0.56 0.15 276)"
  accent-on: "oklch(0.995 0.001 90)"
  accent-hover: "color-mix(in oklab, oklch(0.56 0.15 276), black 8%)"
  accent-soft: "color-mix(in oklab, oklch(0.56 0.15 276), oklch(0.995 0.001 90) 90%)"
  accent-line: "color-mix(in oklab, oklch(0.56 0.15 276), oklch(0.995 0.001 90) 55%)"
  s-queued: "oklch(0.72 0.005 270)"
  s-running: "oklch(0.76 0.15 75)"
  s-attention: "oklch(0.56 0.15 276)"
  s-done: "oklch(0.6 0.14 155)"
  s-failed: "oklch(0.6 0.2 27)"
  s-running-soft: "color-mix(in oklab, oklch(0.76 0.15 75), oklch(0.995 0.001 90) 86%)"
  s-done-soft: "color-mix(in oklab, oklch(0.6 0.14 155), oklch(0.995 0.001 90) 88%)"
  s-failed-soft: "color-mix(in oklab, oklch(0.6 0.2 27), oklch(0.995 0.001 90) 90%)"
  viz: "oklch(0.58 0.2 295)"
  viz-soft: "color-mix(in oklab, oklch(0.58 0.2 295), oklch(0.995 0.001 90) 88%)"
  hue-1: "oklch(0.93 0.045 200)"
  hue-1-ink: "oklch(0.42 0.1 200)"
  hue-2: "oklch(0.93 0.045 320)"
  hue-2-ink: "oklch(0.42 0.1 320)"
  hue-3: "oklch(0.93 0.045 145)"
  hue-3-ink: "oklch(0.42 0.1 145)"
typography:
  display:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 590
    lineHeight: 1.25
    letterSpacing: "-0.016em"
    fontFeature: "\"cv01\", \"ss03\""
  headline:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "18px"
    fontWeight: 510
    lineHeight: 1.3
    letterSpacing: "-0.016em"
  title:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 590
    lineHeight: 1.45
    letterSpacing: "-0.016em"
  body:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "-0.006em"
    fontFeature: "\"cv01\", \"ss03\""
  body-read:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "-0.006em"
  label:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 510
    lineHeight: 1.45
    letterSpacing: "-0.006em"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "0.9em"
    fontWeight: 400
    letterSpacing: "0"
rounded:
  xs: "4px"
  sm: "6px"
  lg: "10px"
  xl: "16px"
  card: "14px"
  pill: "9999px"
spacing:
  "1": "4px"
  "2": "8px"
  "3": "12px"
  "4": "16px"
  "5": "20px"
  "6": "24px"
  "8": "32px"
  "10": "40px"
components:
  button:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "28px"
    padding: "0 10px"
  button-hover:
    backgroundColor: "{colors.surface-2}"
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-on}"
    rounded: "{rounded.sm}"
    height: "28px"
    padding: "0 10px"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    rounded: "{rounded.sm}"
    height: "28px"
  button-sm:
    typography: "{typography.label}"
    height: "24px"
    padding: "0 8px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    rounded: "{rounded.sm}"
    height: "30px"
    padding: "0 10px"
  chip:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg-2}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    height: "22px"
    padding: "0 8px"
  badge-attention:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
    height: "22px"
  tab:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    rounded: "{rounded.pill}"
    height: "26px"
    padding: "0 10px"
  tab-selected:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
  nav-item:
    textColor: "{colors.fg-2}"
    rounded: "{rounded.sm}"
    height: "28px"
    padding: "0 8px"
  nav-item-selected:
    backgroundColor: "{colors.surface-3}"
    textColor: "{colors.fg}"
  rail-card:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "9px 10px 10px"
  rail-row:
    textColor: "{colors.muted}"
    rounded: "{rounded.sm}"
    height: "30px"
  composer:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "10px 10px 8px 14px"
  send:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-on}"
    rounded: "{rounded.pill}"
    size: "30px"
  toast:
    backgroundColor: "{colors.fg}"
    textColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    height: "38px"
---

# Design System: Command Centre

<!-- impeccable:design-map -->
Name: Command Centre
Colors: canvas oklch(0.968 0.002 90), surface oklch(0.995 0.001 90), surface-2 oklch(0.955 0.002 90), surface-3 oklch(0.925 0.003 90), fg oklch(0.22 0.004 270), fg-2 oklch(0.38 0.004 270), muted oklch(0.5 0.005 270) (+25 more in the frontmatter)
Fonts: Inter (display, headline, title, body, body-read, label); JetBrains Mono (mono)
Spacing: 1 4px, 2 8px, 3 12px, 4 16px, 5 20px, 6 24px, 8 32px, 10 40px
Radius: xs 4px, sm 6px, lg 10px, xl 16px, card 14px, pill 9999px
North Star: "Signal"
Rules:
- The Your-Move Rule. Indigo marks the next action and nothing else.
- The State-or-Nothing Rule. A colour is a state, a project's hue, or your next move; on Insights charts one violet `viz` hue also marks data.
- The No-Literal Rule. Templates and islands never carry a colour literal.
- The Sentence-Case Rule. Every heading, label and group head is sentence case.
- The Mono-Is-Data Rule. Mono marks machine text.
- The Flat-By-Default Rule. Nothing in the page flow carries a shadow.
Elevation:
- Float: 0 0 0 1px var(--color-border), 0 1px 1px rgb(20 20 24 / 0.03), 0 6px 20px -6px rgb(20 20 24 / 0.14)
- Overlay: 0 0 0 1px var(--color-border), 0 1px 2px rgb(20 20 24 / 0.05), 0 20px 44px -12px rgb(20 20 24 / 0.24)
- Focus ring: 0 0 0 2px var(--color-surface), 0 0 0 4px var(--color-accent-line)
Don't:
- Don't use ticket cards in lists, uppercase labels, gradients or a coloured sidebar.
- Don't use `viz` for state, action or chrome, or a gradient, sparkline or 14px card outside
  Insights charts ([ADR 28](docs/adr/0028-charts-get-one-data-hue.md)).
- Don't add a token outside the frontmatter.
- Don't colour `running` indigo or `checking` teal.
- Don't draw full table grids, tinted table heads or `[bracket]` buttons.
- Don't use mono as decoration, or a colour that is not a state, a project hue, your move or chart data (`viz`).
- Don't add a dark theme, a component library, or a third-party font request.
Do:
- Do compose state colour through the named classes (`glyph`, `meter`, `meter-fill`,
- Do keep `viz` to Insights chart marks: one violet hue, gradient area fills, sparklines, green/red
  change chips (`s-done`, `s-failed`) and bordered 14px `rounded.card` cards with no shadow.
- Do pair every glyph with its state word somewhere on screen.
- Do keep one primary button per screen, on the owner's next move.
- Do check every changed page at 1440px and 390px.
- (+1 more)
Map: docs/adr/0001-one-global-stylesheet.md
Map: web/app.css
Map: internal/web/assets
Map: docs/adr/0015-pages-are-routes-beside-a-polling-rail.md
Map: docs/adr/0026-feature-and-ticket-pages-share-one-tabbed-sidebar.md
Map: docs/adr/0014-glyph-is-the-only-colour-key.md
Map: docs/adr/0022-import-is-per-repo.md
Map: docs/adr/0006-scope-is-a-view-concern.md
Full: .impeccable/design-full.md (read a section when you need its detail)
