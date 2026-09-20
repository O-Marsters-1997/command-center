# Plan: the side nav

> Source: the grilling session of 2026-09-20. No ADR yet. Phase 2 owes `CONTEXT.md` three new
> entries, **sidebar**, **rail** and **breadcrumb**, since all three name surface concepts the
> glossary does not currently cover.

## The problem

The masthead is one wrapping flex row carrying ten things: the title, the workspace name, the live
pill, the observe pill, the board/graph nav, one pill per configured repo, one pill per feature in
the fleet, the reimport button, a link to `/features`, and the theme toggle. Two of those rows grow
without bound. `featureLinksFor` emits a pill per distinct feature, so a fleet of twenty wraps the
header to four lines before a single ticket is visible.

Three of the four pages you can land on have no shell at all. `/features`, `/preview` and `/confirm`
each carry their own doctype, stylesheet link, title and hand-rolled back link. A side nav that
disappears on `/features` is worse than no side nav, so the layout extraction is the ticket, and the
sidebar is what it buys.

## The model

Vercel's split, because the app already has its shape. Scope is `repo` × `feature`. Section is
`board` | `graph`. Those are orthogonal, so scope lives in the top bar and sections live in the
sidebar.

```
┌─────────────────────────────────────────────────────────────┐
│ workspace   command-center ⌄ / cc-236 ↻      ● 3 live  ⟳ 4s  ☾│  static, never swaps
├──────────┬──────────────────────────────────────────────────┤
│ ▤ board  │  band                                            │
│ ◆ graph  │  ───────────────────────────────────────────────  │
│ ▦ feat.. │  board                                           │
│          │                                                  │
│      ⟨   │                                                  │
└──────────┴──────────────────────────────────────────────────┘
```

Switching feature is a destination, not a control: sidebar → **features** → the search box that
`/features` already has → click a feature → `GET /features/{feature}` 303s to `/?feature=X` → that
feature's board. That path is already wired. What is missing is a nav that points at it.

## Technical design decisions

**One layout, four pages.** A new `layout.tmpl` owns the doctype, head, inline theme and clock
script, top bar, sidebar and a content slot. `/`, `/features`, `/preview` and `/confirm` all render
through it. Each of the three bare pages loses its own doctype, stylesheet link and `<title>`
scaffolding. `/confirm`'s "back to the board" link goes, made redundant by the nav. `/preview`'s
`[ cancel ]` stays, because cancelling a launch is not the same act as navigating away from it.

**The masthead splits in two.** The half that changes on a tick keeps `id="masthead"` and
`hx-swap-oob="true"`, and shrinks to the live pill, the observe pill and the last-error banner. The
half you interact with, the workspace name, the breadcrumb and the theme toggle, becomes a sibling
outside every swap target. The rule that falls out: anything that changes on a tick swaps, anything
you point at does not.

The masthead loses, permanently: the board/graph nav (now the sidebar), the repo pills (now the
breadcrumb switcher), the feature pills (now the features destination), and the `/features` link
(now the sidebar). `pageView.FeatureLinks` and `featureLinksFor` are deleted outright.
`pageView.RepoLinks` and `repoLinksFor` survive and feed the breadcrumb switcher unchanged.

**The sidebar never swaps.** It renders once per page load and sits outside every `hx-swap` target,
the same rule as the breadcrumb. Every scope change is already a full page load, since scope links
use `pagePath()` rather than `boardPath()`, so any navigation refreshes it. The one staleness
window is a feature that finishes importing while you sit still: its tickets appear on the board
within five seconds, but `/features` will not list it as imported until your next navigation. That
is the accepted trade. In exchange: no focus loss while tabbing the nav, no `hx-preserve`, and the
out-of-band masthead payload shrinks to status.

**Collapse is CSS, not markup.** One set of markup, two states, switched by
`html[data-nav="rail"]`. The state is written to `localStorage` and applied to `documentElement`
before first paint by the inline script that already does exactly this for `data-theme`. No Go
change, no new field on `pageView`, no new query parameter, no cookie. Collapsed is a 3rem icon
rail; expanded shows icon plus label.

Rejected: a query parameter through `viewParams`, which would poison `boardPath`, `verbPath` and
every row's `SelectPath` and `TogglePath`, change every golden file, and put a rendering preference
into shareable URLs beside real view state.

**Three hand-authored SVG icons.** Board, graph, features. 16px, 1.5px stroke, `currentColor` so
tone and theme come free, one `{{define "icon-board"}}` each, inline so there is no sprite fetch.
Deterministic, unlike unicode box-drawing, which falls back differently across the three fonts in
`--font-mono` and can land as tofu on a machine that is not this one. The app is acquiring an icon
language it has never had; three marks is the whole of it, and every future sidebar entry owes one.

Scope needs no icon, which is the point of putting it in the breadcrumb. Repo names and feature
labels are arbitrary strings that will never have a glyph, and a rail that cannot represent them
would be a rail you expand once and leave expanded.

**The breadcrumb.** Reads `workspace / <repo ⌄> / <feature>`, each segment present only when that
axis is scoped. The repo segment carries a switcher built on the native `popover` attribute: a
button with `popovertarget`, a div with `popover`, holding the same anchor list `repoLinksFor`
already produces. Top layer, light dismiss, Escape to close, all from the browser, no JS and no
third island. Positioned with an ordinary relative wrapper, not CSS anchor positioning, which is
still Chromium-only.

The feature segment has no switcher. Repos are configured, bounded and currently one. Features are
tracker-owned and unbounded. The asymmetry is justified by the data, not by taste.

**Reimport rides the feature segment.** When a feature is scoped, its breadcrumb segment carries a
small reimport control, preserving what cc-267 was for: unsticking a feature without leaving the
board. It still `hx-post`s to `featureImportPath()` and targets `#board`, exactly as today. Because
the breadcrumb is outside the swap, the control is no longer rebuilt under the cursor twelve times
a minute.

**One breakpoint.** Below 900px the nav forces the rail whatever `localStorage` says. No off-canvas
drawer, no overlay, no focus trap, no scroll lock. `app.go:168` binds `127.0.0.1`, so the only
narrow viewport that can reach this server is a half-width window on the same machine, and both the
board table and the graph island are wide surfaces that degrade there regardless.

**Accessibility floor.** `<nav aria-label>` on the sidebar. The toggle carries `aria-expanded` and
`aria-controls`. Icons are `aria-hidden` with a visually-hidden label beside them, so the accessible
name survives collapse. `aria-current="page"` stays the convention it already is, and applies to the
active view even though board and graph are both `/` under different `?view=`. The skip link
retargets from `#board` to the main content region, since the nav now precedes it in source order.

No keyboard shortcut. The toggle is focusable and early in the tab order, and that is the whole
interaction. The app has no keybinding vocabulary today, not even for theme, and one key means
deciding whether there is a scheme and what owns it.

## Phases

Two tickets, stacked. Stacking is already on for this repo.

**Phase 1: the layout.** Extract `layout.tmpl`. Put `/features`, `/preview` and `/confirm` on it.
Split the masthead into its swapping and static halves. Move the existing repo pills, view nav and
reimport into the static half as they are. Nothing new is drawn and nothing is deleted yet. The diff
reads as things moved.

**Phase 2: the nav.** Add the sidebar, the rail, the three icons, the collapse script and its CSS
states, the breadcrumb and its popover switcher. Delete `FeatureLinks` and `featureLinksFor`. Move
reimport onto the feature segment. Add the three `CONTEXT.md` entries. The diff reads as new chrome.

Kept apart because the repo's own rule is that the diff is the review, and a four-file layout
extraction mixed with a visual redesign makes both unreadable.

## Cost

Five golden files regenerate: `shell`, `board`, `board_grouped`, `board_selected`, and `preview`
once it wears the layout. Three e2e txtar files touch the header: `board_route`, `features_page`,
`launch_open_confirm_spawn`. `just assets` must rerun, because Tailwind purges from `internal/cc`
and the built sheet is committed and diffed by CI.

The repo config already lists `internal/cc/testdata/*.golden.html` under `generated` with `just
assets` as the `build_command`, so a golden conflict between the two stacked branches resolves in
the app rather than waiting on the resolve verb.

## Not in this plan

- A command palette, or any keybinding scheme.
- A repos destination page. One configured repo does not earn one.
- An off-canvas mobile drawer. There is no phone that can reach `127.0.0.1`.
- A filter inside the nav. `/features` already has the search box, and that is where feature
  switching now happens.
- Any change to the board, the band, the detail panel or either Solid island.
