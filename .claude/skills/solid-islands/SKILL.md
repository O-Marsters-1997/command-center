---
name: solid-islands
description: >
  How a Solid island is written in command-center: the cc-* custom element bootstrap, data from a
  JSON endpoint with loading and error states, cleanup, hx-* markup rendered by Solid, wire types
  that match Go json tags, pure logic under bun test, dialogs and shortcuts. TRIGGER — load BEFORE
  editing web/src/*.tsx or *.ts, adding a cc-* element or a vite entry, touching solid-element, or
  adding a JSON endpoint an island reads. Plain requests count: "make the graph show X", "the launch
  modal should…", "add an interactive widget to the feature page". Load alongside
  frontend-best-practices. SKIP for .tmpl templates, Go handlers that return HTML, and CSS-only work.
---

# Solid islands

An island is a `cc-*` custom element built with `solid-element`, placed by a Go template with
fallback text and a `<script type="module" src="/assets/dist/<name>.js">`. The browser's
connect and disconnect callbacks fire when htmx swaps the element in or out, so Solid's
`onCleanup` is the unmount hook. There is no island registry and no `htmx.onLoad`.

An island earns its place by holding client state HTML cannot: a laid-out graph, a selection with
rules. If the server can render it, or `<details>`, `<dialog>` or `popover` can do it, it is not an
island.

## Files

| File | Holds |
|---|---|
| `web/src/<name>.tsx` | The element: signals, JSX, wiring. Thin. |
| `web/src/<name>.ts` + `<name>.test.ts` | Pure logic on plain data, tested with `bun test` |
| `web/src/island.ts` | The shared bootstrap, fetch and `hx` helpers |
| `web/src/layout.ts` | Shared DAG layout. Islands share through modules like this and never import each other |
| `web/vite.config.ts` | One `lib.entry` per island |
| `internal/web/<name>.tmpl` | The element, its fallback text and its script tag |

## Rules

1. **One bootstrap.** An island is `defineIsland(tag, defaults, Component)` from `island.ts`, which
   calls `customElement`, then `noShadowDOM()` (ADR 1: one global stylesheet, and htmx must see the
   markup), then clears the fallback text, because `solid-element` appends rather than replaces. If
   `island.ts` does not exist yet, extract it from `graph.tsx` and `launch-modal.tsx` the first
   time you touch an island, and move both onto it.
2. **Props are attributes; data is a JSON endpoint.** Scalars such as a feature name or a ticket
   query are attributes. Lists, and anything that refreshes, come from a GET endpoint in
   `internal/web` that encodes `internal/web/view` structs. Poll from the island only when a server
   re-render would destroy client state (focus, hover, scroll). Otherwise let the template
   re-render the element.
3. **Every fetch shows loading, error and empty.** Errors render the `banner` grammar. Abort the
   request on cleanup and skip poll ticks while `document.hidden`. Never write `catch {}`: a
   swallowed error leaves stale data looking current.
4. **Everything an island starts, `onCleanup` stops**: intervals, `matchMedia` and document
   listeners, `AbortController`s. Per-instance state such as caches and ref maps lives inside the
   component, so it dies with the element. A cache keyed by id is pruned to the latest payload.
5. **Keep DOM identity stable across refreshes.** `<For>` keys by reference, so reuse each node
   object by id. Focus and hover then survive a poll.
6. **Solid-rendered `hx-*` is inert until htmx processes it.** Pass the element through the one `hx`
   ref helper, which calls `window.htmx?.process(el)` after insertion. `Window.htmx` is declared once
   in `web/src/htmx.d.ts`, never per file. Copy the attribute set the Go templates use for the same
   link (`hx-select="#main"`, `hx-target="#main"`, `hx-push-url`), per AGENTS.md Frontend.
7. **Wire types match Go json tags exactly.** A `[]string` field is `string[] | null` in TS, because
   Go encodes a nil slice as `null`. Change the view struct and its TS interface in the same commit,
   and grep `web/src` for the old field name.
8. **Logic lives in `.ts`, not `.tsx`.** Layout, selection rules, labels and refusal messages take
   plain data and are tested with `bun test`. When a function needs the DOM, it takes a narrow
   interface (`KeyTarget` and `Timers` in `keys.ts`) so a test can pass a fake.
9. **Overlays are a native `<dialog>` opened with `showModal()`.** It traps focus, makes the page
   inert and handles Escape. React to Escape through the dialog's `cancel` event, not a document
   `keydown` listener.
10. **Global shortcuts go through the one keymap.** Render `data-key="x"` on the control, and
    `keys.ts` resolves and activates it, already skipping keys typed into a field. Arrow keys that
    move within the widget stay on the island's own elements.
11. **Styling follows AGENTS.md Frontend.** Utilities for layout, the named state grammar
    (`glyph glyph-{word}`, `banner`, `ribbon`…) for state, so Go and Solid render state
    byte-identically. Colour comes only from `@theme` tokens.

## Checking

- `cd web && bun test && bun run typecheck && bun run lint`. `tsc` excludes the tests, so
  `bun test` is the only thing that runs them.
- `just assets`, then check the page against `DESIGN.md` at 1440px and 390px.
- Without `dist`, the element's fallback text still renders.
