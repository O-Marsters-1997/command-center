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

## Overview

**Creative North Star: "Signal"**

The owner runs several features across several repos, with under ten agents live at once. The
surface exists to answer one question at a glance: does anything need me? Signal is light, quiet
and dense. It takes Linear's structure and type discipline and T3 Code's agent surfaces: a session
rail, a transcript, a composer, and a "Settled" group for finished work.

While agents work the screen stays still. A running agent is an amber pie and a shimmer line, and
nothing else moves. Colour that pulls the eye is kept for the moments that need a human: a review
waiting, a failure to repair, a question to answer. Indigo always means "your move", so a screen
with nothing to do shows almost no indigo.

**Key Characteristics:**

- Light only. There is no dark theme and no toggle ([ADR 1](docs/adr/0001-one-global-stylesheet.md)).
- Lists, not cards. Rows run 30–46px. Cards appear only for the few sessions that need you.
- Structure comes from surface steps and spacing, with few borders and soft ones.
- One status grammar: eight glyph words, each a 14px ring that fills as work progresses.
- Agent work folds away into a single line and opens on demand.

## Colors

Warm, near-zero-chroma neutrals carry the structure. One indigo accent and four status hues carry
the meaning. Every value is an oklch token in `web/app.css` `@theme`, named `--color-<key>` for
each key in the frontmatter.

### Primary

- **Your-Move Indigo** (`accent`): the primary button, the send button, the focus ring,
  review-ready glyphs, and the selected rail card. It appears at most once or twice per screen.
  It shades to `accent-hover` when pressed, `accent-soft` for selected and attention backgrounds,
  and `accent-line` for selected borders.

### Secondary

- **Working Amber** (`s-running`): running, push-pending and checking glyphs, and the running
  badge on `s-running-soft`. It is quiet by design, never a full-surface fill.
- **Merged Green** (`s-done`): the merged glyph and diff additions. `s-done-soft` is the done
  badge.
- **Repair Red** (`s-failed`): the failed glyph, failing checks, refused callouts and diff
  deletions. `s-failed-soft` is the failed badge and callout ground.
- **Queued Grey** (`s-queued`): the blocked ring and queued segments.

### Tertiary

- **Project hues** (`hue-1`…`hue-3`, each with an `-ink` partner): the project avatar's ground
  and letter. Three soft hues (teal, orchid, sage) tell projects apart. They are never a status.

### Neutral

- **Canvas** (`canvas`): the app background and the session rail. The rail sits one step dimmer
  than the content.
- **Surface** (`surface`): the content pane, panels, inputs, cards and the composer.
- **Surface 2 / 3** (`surface-2`, `surface-3`): hover, then selected or pressed.
- **Ink ramp** (`fg` → `fg-2` → `muted` → `meta`): titles and values, body copy, secondary text,
  then refs, counts and timestamps.
- **Edges** (`border`, `border-soft`, `border-strong`): control edges, row separators, and
  emphasis or hover.

### Named Rules

**The Your-Move Rule.** Indigo marks the next action and nothing else. If a screen has no action
waiting, it should have almost no indigo.

**The State-or-Nothing Rule.** A colour is a state, a project's hue, or your next move.
Nothing else gets colour.

**The No-Literal Rule.** Templates and islands never carry a colour literal. Colour is a token,
composed by a named class.

## Typography

**Display Font:** Inter, variable with optical size 14–32 (system-ui fallback)
**Body Font:** Inter, with `cv01` and `ss03` on
**Label/Mono Font:** JetBrains Mono, for refs, branches, commands and durations only

**Character:** One sans at a 13px UI default, with weight doing the hierarchy and mono kept for
machine text. Both fonts are vendored woff2 under `internal/web/assets`; no page requests Google
Fonts.

### Hierarchy

- **Display** (590, 22px, 1.25, −0.016em): the page title on every route and the session title.
- **Headline** (510, 18px): the repo page's mono repo name and the detail titles.
- **Title** (590, 15px, −0.016em): module headings within a page, and dialog titles.
- **Body** (400, 13px, 1.45, −0.006em): the UI default. Row titles and controls use 510.
- **Body-read** (400, 14px, 1.6): agent messages, prompts and the composer. Max 68ch.
- **Label** (510, 12px): metadata, group heads, nav labels, chips and badges.
- **Mono** (400, 0.9em, 0 tracking, tabular numerals): refs (`#42`), branches, commands and
  durations.

### Named Rules

**The Sentence-Case Rule.** Every heading, label and group head is sentence case. No uppercase,
no eyebrows, no tracking-widened caps.

**The Mono-Is-Data Rule.** Mono marks machine text. It is never decoration and never a heading.

## Layout

The layout uses a 4px grid and a shell with two panes and an optional third.

- **Shell.** A 300px session rail on `canvas`, then the main pane on `surface`. The rail is the
  scroll container that never swaps; `#main` is the swap target
  ([ADR 15](docs/adr/0015-pages-are-routes-beside-a-polling-rail.md)).
- **Rail anatomy.** From the top: a 44px head with the mark and the launch button, a 30px search
  field with a `⌘K` key hint, then the nav. Below that are three sections: Needs you (cards),
  In flight (feature groups with 28px heads and 30px rows), and Settled (collapsed). A footer
  holds the 5h and week meters, and the shortcuts button.
- **Top bar.** 44px: breadcrumbs on the left and at most one secondary action on the right.
- **Page anatomy.** 28px padding. A 22px title, a muted one-line purpose, then modules titled at
  15px with 28px between modules. Content is capped at 1100px, or 880px on narrow pages.
- **Session.** The transcript is centred at max 720px, with the composer floating at the foot.
  A 300px feature-stack panel sits on the right. Below 1180px the panel becomes an overlay that
  opens on demand.
- **Lists.** Every list groups by attention: Needs you → In progress → Ready to launch → Blocked
  → Done ([ADR 14](docs/adr/0014-glyph-is-the-only-colour-key.md)). Group heads are 34px;
  board rows are at least 46px.
- **Phone (≤760px).** The rail is the home screen. Every page opens over it with a back button.
  Rows grow to 40px, dialogs become bottom sheets, and the graph stacks its waves vertically.

### Routes

| App route | Page |
|---|---|
| `/s/{owner}/{name}/{n}` | Session: transcript or raw log, composer, stack panel |
| `/f/{feature}/graph` | Feature graph: dependency waves |
| `/f/{feature}` | Feature board, grouped by attention |
| `/f/{feature}/launch`, `/launch` | Launch dialog, feature picker |
| `/tickets` | All tickets, filter tabs |
| `/insights` | Limits with reset times, spend per merged ticket, spend split, merged tickets; each a module on a 28px rhythm, tables in the bordered frame |
| `/repos`, `/repos/{owner}/{name}` | Tracked repos; repo banner, checks, tickets |

## Elevation & Depth

Signal is flat at rest. Depth comes from tonal layering, with `canvas` under `surface` and
`surface-2`/`surface-3` for interaction. Shadows are reserved for things that float above the
page.

### Shadow Vocabulary

- **Float** (`box-shadow: 0 0 0 1px var(--color-border), 0 1px 1px rgb(20 20 24 / 0.03), 0 6px 20px -6px rgb(20 20 24 / 0.14)`):
  the composer, menus, the track search results and chart tooltips.
- **Overlay** (`box-shadow: 0 0 0 1px var(--color-border), 0 1px 2px rgb(20 20 24 / 0.05), 0 20px 44px -12px rgb(20 20 24 / 0.24)`):
  dialogs, sheets, the stack panel as an overlay, and toasts.
- **Focus ring** (`box-shadow: 0 0 0 2px var(--color-surface), 0 0 0 4px var(--color-accent-line)`):
  every `:focus-visible`. Fields add a 3px `accent-soft` halo.

### Named Rules

**The Flat-By-Default Rule.** Nothing in the page flow carries a shadow. If it does not float, it
is flat.

## Shapes

Small, consistent radii. Use 4px for kbd and avatars, 6px for controls, rows and nav items, 10px
for panels, cards, callouts and graph nodes, 16px for the composer, dialogs and sheets, and a full
pill for tabs, chips and the send button. Borders are 1px hairlines in `border` or `border-soft`.
A blocked node or an empty state uses a dashed border.

## Components

### Status glyph

This is the signature component. It is a 14px ring with a 1.5px stroke whose fill shows
progress, drawn by the named class family `glyph glyph-{word}` with `[data-glyph]`. Go returns the
word through `plan.Glyph`, and the template composes the class
([ADR 14](docs/adr/0014-glyph-is-the-only-colour-key.md)). The words are `plan.Glyph`'s.

| Word | Ring | Covers |
|---|---|---|
| `blocked` | dashed grey ring, empty | blocked, waiting on producer deploy |
| `ready` | solid `fg-2` ring, empty | ready, cancelled |
| `running` | amber, half pie; breathes when live (`glyph-pulse`) | running |
| `pending` | amber, ⅝ pie | push pending, queued, base moved |
| `checking` | amber, ¾ pie | checking |
| `attention` | indigo, ¾ pie | review me, needs you, awaiting input |
| `done` | green disc with a check | merged |
| `failed` | red disc with a bar | every state a human must repair |

In lists the glyph is a bare ring, and the row's text says the state. In a page or session head
it sits inside a **badge**: a 22px tinted pill with a 12px ring and the state word, on the
state's soft colour.

### Buttons

- **Shape:** 28px tall with 6px corners (`rounded.sm`), in 13px/510 type with a faint 1px drop.
- **Primary:** indigo with white text. **One primary per screen**, on the next move.
- **Default / Ghost / Danger:** a bordered surface; a borderless muted button; a default button
  that turns red on hover. `-sm` is 24px; `-icon` is square.
- **Hover / Focus:** a step to `surface-2` in 120ms, then the focus ring.
- Verbs are real buttons. Never a bare link or `[bracket]` text.

### Chips, badges and avatars

- **Chip:** a 22px pill with a hairline border, in 12px/510, for features, branches (mono
  variant) and filters.
- **Badge:** see Status glyph.
- **Project avatar:** a 16px lettered square on a project hue, drawn by the named class `avatar`
  with `[data-hue]`. A project is a named set of repos
  ([ADR 6](docs/adr/0006-scope-is-a-view-concern.md)). The avatar marks the project beside
  every ticket, feature and repo, and in the rail's scope menu (`G 0`–`G 9`). A repo with no
  project gets the neutral `surface-3` square.

### Tabs

Pill tabs in a 2px-gap row, 26px tall, with an optional count in `meta`. The selected tab is a
raised `surface` with a hairline border. They are used for graph/board, transcript/raw, and the
ticket filters. No underline tabs.

### Lists and tables

- **Rows:** borderless grid rows with a `border-soft` rule beneath, hovering to `surface-2`.
  Columns: glyph, mono ref, title over a muted subline, then right-aligned metrics.
- **Tables:** 34px heads in 12px/510 `muted`, 40px cells, `border-soft` rules only, inside a 10px
  `border-soft` frame. No vertical rules and no tinted header.

### Session rail

- **Card** (Needs you only): a 10px panel holding the glyph and title, then repo · feature in
  `muted`, then two clamped lines of the last activity, then one inline `btn-sm` verb (Reply for
  an agent's question). The selected card takes an `accent-line` border and an `accent-soft`
  halo.
- **Feature group:** a 28px head with a chevron, the scope avatar, the name, and merged/total in
  `meta`. Its 30px rows carry the glyph, the title and a mono metric (timer or ref).
- **Settled:** collapsed behind its head, and it reopens on new activity.

### Transcript and composer

- **Head:** a badge, a 22px title, and repo · feature in `muted`. The "Worked for 1m 44s ›" line
  folds the run into one disclosure.
- **Lines:** a grid of a 44px mono timestamp, an 18px icon and the body. Agent messages are
  body-read at 68ch. Tool groups are disclosures with a hairline-indented sublist. Commands sit
  in a bordered block with the exit status. Errors sit on a `s-failed-soft` block. Your messages
  are right-aligned 16px bubbles on `surface-2`.
- **Callouts:** a failed or attention panel on the soft state ground, holding the primary verb.
- **Composer:** a 16px panel with Float, a body-read textarea, and a bar of 26px ghost pickers
  ending in a 30px round indigo send button. The model, effort and worktree pickers are target
  UI with no data yet.

### Inputs

30px fields with 6px corners on `surface`. On focus the border turns `accent-line` with a 3px
`accent-soft` halo. Labels are 12px/510 `fg-2` above the field. The caret is indigo.

### Navigation

28px nav items with a 15px `meta` icon, the label, and a right-aligned count. The current item is
`surface-3` with `fg` text. Nav labels are 12px/510 `meta`, sentence case.

### Dialogs, toasts and the shortcut sheet

- **Dialog:** a scrim at 18% ink, a 620px panel with 16px corners and Overlay, at 12vh. Head,
  scrolling body and footer are split by `border-soft`. On a phone it is a bottom sheet.
- **Toast:** a centred dark pill (`fg` on `surface`), 38px, 72px off the bottom, carrying an
  Undo button. Every triage verb gets one. Undo is target UI with no data yet.
- **Shortcut sheet:** a 480px dialog with grouped `dl` rows of `kbd` keys.

### Motion

Use 120ms for hover and 180ms for state changes, on `cubic-bezier(0.2, 0, 0, 1)`. Only live things
loop: the shimmer on a running line and a 2.4s breathe on a live glyph. Everything respects
`prefers-reduced-motion`.

## Do's and Don'ts

### Do:

- **Do** compose state colour through the named classes (`glyph`, `meter`, `meter-fill`,
  `segbar-segment`, `banner`, `flag`, `avatar`, `line-*`, `[data-glyph]`, `[data-hue]`). Use
  utilities only for layout and spacing.
- **Do** pair every glyph with its state word somewhere on screen.
- **Do** keep one primary button per screen, on the owner's next move.
- **Do** check every changed page at 1440px and 390px.
- **Do** keep every unblocking verb usable on a phone: 36–40px targets below 760px.

### Don't:

- **Don't** use ticket cards in lists, uppercase labels, gradients or a coloured sidebar.
- **Don't** add a token outside the frontmatter. Legacy aliases (`bg`, `sunken`, `soft`,
  `s-live`, `s-wait`, `s-stop`, `s-idle`, `s-check`) are on their way out.
- **Don't** colour `running` indigo or `checking` teal. Every working state is amber.
- **Don't** draw full table grids, tinted table heads or `[bracket]` buttons.
- **Don't** use mono as decoration, or a colour that is not a state, a project hue or your move.
- **Don't** add a dark theme, a component library, or a third-party font request.
