# Command Centre

A local control plane that drives a DAG of tickets to reviewable pull requests using agents, one
worktree per ticket.

Glossary: `CONTEXT.md`. Mechanism: `docs/designs/command-centre-design.md`. Decisions:
`docs/adr/`.

## The frontend stack

Read this before touching anything visual. The stack is unusual and the wrong assumption is
expensive.

- **Go `html/template` renders every page.** The templates are `internal/cc/*.tmpl`. There is no
  React, no Next, no app framework.
- **htmx drives updates.** The board polls itself every five seconds and swaps its own `outerHTML`.
  Never remove or rename an `hx-` attribute, `id="board"`, or the `hx-preserve` detail row while
  doing styling work.
- **Two Solid islands exist**, `web/src/graph.tsx` and `web/src/launch-modal.tsx`, compiled with
  `solid-element`. Both opt out of shadow DOM
  ([ADR 2](docs/adr/0002-islands-opt-out-of-shadow-dom.md)), so their classes are page-global.
  `solid-js` in `web/package.json` is those islands and nothing more. `web/src/layout.ts` holds
  the DAG layout math they share; neither island imports the other.
- **Tailwind v4, CSS-first.** `web/app.css` holds `@import "tailwindcss"`, an `@theme` block of
  oklch tokens, and a `[data-theme="dark"]` override. There is no `tailwind.config.*`, and there
  will not be one.

## Styling

**For styling work, load the `tailwind-design-system` skill.** It matches this repo: CSS-first
`@theme` configuration, the token hierarchy, oklch colour, native dark mode.

**Do not load or follow `tailwind-shadcn`.** It detects `solid-js` in `web/package.json` and enters
a Solid mode built for shadcn-solid and Kobalte. This repo has neither and will not get them. Its
advice about `components/ui/`, `cva` variants, `ui.config.json`, `className`, and the
`shadcn`/`shadcn-solid` CLIs does not apply to Go templates.

Concretely, in this repo:

- **Add no component library, no CLI scaffolding, and no new dependency.** Not shadcn, not
  shadcn-solid, not Kobalte, not Base UI, not `cva`.
- **Utilities go in the template, for layout and spacing.**
- **The state grammar stays a small set of named classes**, because Go composes class names from
  derived state at render time and Tailwind's purge cannot see a string built at runtime. These are
  named: the `pill` family, `ribbon`, `meter`, `meter-fill`, `segbar-segment`, `banner`, `flag`,
  `flag-warning`, the `line-*` family, and the `[data-tone]` mapping.
- **Go never returns a utility string.** `plan.Tone` returns one of five words and the template
  composes the class from it.
- **The `data-depth` indent rules stay CSS attribute selectors.** A depth is an integer, not one of
  five words, so a class per depth would force Go to emit a utility name.

Colour belongs in `@theme` as an oklch token with a matching entry in the dark block, never as a
literal in a template.

## Build

```
just assets        # bun install && bun run build, in web/
just test          # or: go test ./...
```

`internal/cc/assets/dist/app.css` is committed and CI diffs it, so a styling change is not finished
until `just assets` has run and the built sheet is committed. Go never depends on Node: `test`,
`e2e` and `lint` are Go-only.

Golden files in `internal/cc/testdata/` regenerate with `go test ./internal/cc -update`. Read the
diff; it is the review.

## Constraints an agent will hit

- **`.github/**`, `go.mod`, `go.sum` and `.golangci.yml` are deny-listed** for agent pushes. Work
  that needs a CI change or a new Go dependency cannot be completed by an agent and should stop and
  say so rather than working around it.
- **The repo is squash-merge only.** A merged branch is not an ancestor of `main`, so GitHub's PR
  state is the only honest merge test.
- **Open pull requests as drafts.**
