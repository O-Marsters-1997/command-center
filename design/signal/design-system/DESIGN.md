# Signal — Command Centre design system

> Light, quiet, dense. Linear's structure and type discipline, T3 Code's
> agent surfaces (session rail, transcript, composer, "Settled"). Files:
> `tokens.css` (the contract), `components.css` (shared recipes),
> `index.html` (specimen). Product truth lives in `/PRODUCT.md`.

## Principles

1. **Structure is felt, not seen** (Linear). The sidebar sits a step dimmer than
   the content. Use few dividers, and soft ones. Separation comes from spacing
   and surface steps, not boxes.
2. **Running is quiet; needing you is loud.** Working agents get an amber pie and
   a shimmer line. Colour that pulls the eye is reserved for states that need a
   human: review, failure, unlock.
3. **Accent means "your move".** Indigo marks the next action: primary button,
   send, focus, review-ready. A screen with nothing to do shows almost no indigo.
4. **Lists, not cards.** Rows of 36–40px. Rich cards are allowed only for the
   handful of sessions that need you (T3 Sidebar V2).
5. **Fold the agent's work.** "Worked for 1m 44s ›" collapses to one line and
   expands on demand.
6. **Settle what's finished.** Merged work drops into a collapsed "Settled"
   group. New activity brings it back.

## Colour

Neutrals are OKLCH with chroma near zero and a faint warm hue (Linear's 2025
move away from blue-grey). The accent is Linear-adjacent indigo.

| Token | Role |
|---|---|
| `--canvas` | App background, sidebar (the dimmed layer) |
| `--surface` | Content pane, panels, inputs |
| `--surface-2` / `--surface-3` | Hover / selected and pressed |
| `--fg` → `--fg-2` → `--muted` → `--meta` | Four-step text ramp |
| `--border`, `--border-soft`, `--border-strong` | Edges, row separators, emphasis |
| `--accent` `oklch(0.56 0.15 276)` | Your move |

### Status glyphs

Linear-style 14px rings that fill as work progresses. Each glyph is always
paired with a word somewhere on screen.

| Glyph | `data-s` | Maps from |
|---|---|---|
| Dashed grey ring | `blocked` | `blocked` |
| Solid dark ring | `ready` | launchable now |
| Amber half pie | `running` | `running` |
| Amber ⅝ pie | `pending` | `push_pending` |
| Amber ¾ pie | `checking` | `checking` |
| Indigo ¾ pie | `attention` | `review_me` |
| Green disc with check | `done` | `merged` |
| Red disc with bar | `failed` | `conflicts_with_main`, failing checks, failed runs |

## Typography

- **Inter** (variable, optical size 14–32), with `cv01` and `ss03` on. JetBrains
  Mono for refs, branches, commands, money and durations only.
- **UI default is 13px.** Metadata 12px, reading 14px, section titles 15px,
  page and detail titles 18–22px.
- **Weights:** 400 body, **510** medium (controls, row titles, nav), 590 headings.
- Tracking −0.006em on UI text, −0.016em on titles. Sentence case, no eyebrows.

## Space, radius, elevation

- 4px grid. Nav items 28px, list rows 36–40px, top bar 44px, sidebar 244px.
- Radius: 4 for kbd and avatars, 6 for controls and rows, 10 for panels, 16 for
  the composer and sheets, pill for tabs and chips.
- Flat by default. `--elev-float` for the composer, action bars and menus.
  `--elev-overlay` for peek panels, sheets and toasts.

## Components

- **Button:** 28px, 6px radius, 13px/510, faint 1px drop. One primary per screen.
- **Tabs:** pill tabs. The selected tab is a raised surface with a hairline.
- **Nav item:** 28px with a muted icon and a count on the right; selected is `--surface-3`.
- **Avatar:** a 16px lettered square per project, with three soft hues.
  Hue tells projects apart; it is not a status.
- **Session card** (rail): appears only for needs-you items. Title, project ·
  feature, last line, and one inline verb.
- **Peek:** a right-hand panel opened with Space or a click; Esc closes it.
  On a phone it is a bottom sheet.
- **Composer:** 16px radius with a float shadow, model/effort/access pickers,
  and a round accent send button.
- **Toast:** dark pill with an Undo button, used for every triage verb.

## App structure

One shell, hash-routed (`/index.html`, code in `/app`). The session rail is always present on desktop; on a phone it is the home screen and every page opens over it with a back button.

| Route | Page | Replaces |
|---|---|---|
| `#/` · `#/s/<repo>#<n>` | Session: transcript or raw log, composer, feature stack panel | board row expansion, ticket detail |
| `#/f/<feature>/graph` · `/board` | Feature: dependency waves, or the board grouped by attention | graph, feature-scoped board |
| `#/f/<feature>/launch` · `#/launch` | Launch dialog: ready now vs on unlock, extra context | launch modal |
| `#/tickets` | All tickets across projects, filterable | board |
| `#/insights` | Limits, spend per merged ticket, merged table | insights |
| `#/repos` · `#/repos/<owner>/<name>` | Tracked repos, four checks, track search; repo banner, checks and tickets | repos, repo-scoped board, repo banner states |

Page anatomy: a 44px top bar with breadcrumbs and at most one secondary action, then a 22px title, a muted one-line purpose, and modules titled at 15px. Lists use the grouped board (Needs you → In progress → Ready → Blocked → Done) everywhere.

## Keyboard

`⌘K` search · `⌘0–9` project scope · `J/K` next/previous session · `C` launch ·
`G` then `H/T/I/R` go to home, tickets, insights, repos · `F` feature graph · `V` graph/board ·
`E` settle · `R` re-run · `O` open PR · `?` shortcut sheet · `Esc` close. Every verb has an Undo toast.

## Motion

120ms for hover, 180ms for state changes, `cubic-bezier(0.2, 0, 0, 1)`. Only live
things loop: the shimmer and a slow breathe on the running glyph in the rail.
All motion respects `prefers-reduced-motion`.

## Don't

- Don't use ticket cards in lists, uppercase labels, gradients or coloured
  sidebars.
- Don't use colour that isn't a state, a project hue or your next move.
- Don't use mono as decoration.
- Don't use dark mode yet (light only).
