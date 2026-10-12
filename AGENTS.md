# AGENTS.md

A local control plane that drives a DAG of tickets to reviewable pull requests using agents, one
worktree per ticket.

Glossary: `CONTEXT.md`. Mechanism: `docs/designs/command-centre-design.md`. Decisions:
`docs/adr/`. Check them before a structural decision; routine work does not need them.

## Approach

- **Load the `go-idiomatic` skill before editing any `.go` file**, tests included. Existing code
  that breaks it is not licence to match it.
- Comments follow `~/.claude/rules/comments.md`.
- **Write source files with Edit or Write, never Bash heredocs, `sed -i` or `perl -pi`.** The
  `require-go-skills` and `check-written-comments` hooks only see Edit and Write.
- Use **bun** in `web/`, never npm, pnpm or yarn.
- Open pull requests as drafts. The repo is squash-merge only, so GitHub's PR state is the only
  honest merge test.

## Layout

One binary, `cmd/cc`, wired together in `internal/app`.

- `internal/plan`: pure decisions. **Only `plan` derives**
  ([ADR 13](docs/adr/0013-only-plan-derives.md)) and it imports only `spend` and `verdict`.
- `internal/loop`: the tick loop. **The loop is the only writer of reconciled state**
  ([ADR 9](docs/adr/0009-the-loop-owns-reconciled-state.md)); verbs queue intents for it.
- `internal/store`: Postgres, and the only importer of the generated `internal/store/ccdb`
  ([ADR 4](docs/adr/0004-sql-is-generated-from-the-schema.md)). Queries live in
  `internal/store/queries`, migrations in `internal/store/migrations`. Never edit an applied
  migration.
- `internal/web`: handlers and `html/template` pages. It derives per render through `plan` and
  caches nothing.
- `gh`, `tracker` and `git` each decode the gh CLI's JSON; a change to gh output handling checks
  all three.

## Frontend

The stack is unusual and the wrong assumption is expensive.

- **The visual target is `DESIGN.md` (Signal)**, with `PRODUCT.md` as the product record
  `/impeccable` reads. Check a changed page against `DESIGN.md` at 1440px and 390px.

- **Go `html/template` renders every page** (`internal/web/*.tmpl`). No React, no app framework.
- **htmx drives updates.** Pages are path routes (`/s/{owner}/{name}/{n}`, `/f/{feature}`, `/tickets`
  and so on), each rendered in full by one handler. Rail links fetch a page with
  `hx-select="#main"` and `hx-push-url`, swapping `#main` and never the rail. The rail polls
  `GET /rail` inside a scroll container that does not swap
  ([ADR 15](docs/adr/0015-pages-are-routes-beside-a-polling-rail.md)). Do not remove or rename an
  `hx-` attribute, `#main`, or the rail's scroll container. `id="board"` stays as the swap target of the
  board's poll and verb forms, and the board sections by attention
  ([ADR 14](docs/adr/0014-glyph-is-the-only-colour-key.md)).
- **Two Solid islands**, `web/src/graph.tsx` and `web/src/launch-modal.tsx`, opt out of shadow DOM
  ([ADR 1](docs/adr/0001-one-global-stylesheet.md)), so their classes are page-global. They share
  `web/src/layout.ts`; neither imports the other. Load the `solid-islands` skill before editing
  `web/src/` or adding a `cc-*` element.
- **Tailwind v4, CSS-first.** `web/app.css` holds the `@theme` oklch tokens. The
  theme is light only: no dark block, no toggle
  ([ADR 1](docs/adr/0001-one-global-stylesheet.md)). There is no `tailwind.config.*` and will not be one.
- **Go never depends on Node.** `test`, `e2e` and `lint` are Go-only; the tests that read the built
  built assets skip when they are absent. Everything in `dist` is generated and gitignored
  ([ADR 16](docs/adr/0016-built-assets-are-generated-not-committed.md)).

For styling, load `tailwind-design-system`. **Do not load `tailwind-shadcn`**: it mistakes
`solid-js` for a shadcn-solid project. Add no component library, CLI scaffolding or new dependency.

- Utilities go in the template, for layout and spacing.
- The state grammar stays named classes, because Go composes them at render time and Tailwind's
  purge cannot see them: the `glyph` family (`glyph glyph-{word}`), `ribbon`, `meter`, `meter-fill`,
  `segbar-segment`, `banner`, `flag`, `flag-warning`, `avatar`, the `line-*` family, and the
  `[data-glyph]` and `[data-hue]` mappings ([ADR 14](docs/adr/0014-glyph-is-the-only-colour-key.md)).
- Go never returns a utility string. `plan.Glyph` returns one of eight words and the template
  composes the class.
- Colour is an oklch token in `@theme` with no dark counterpart, never a literal in a template.

## Tests

- `go-idiomatic`'s Testing section is the authority on how a test is written.
- Store and loop tests need Docker: `internal/cctest` starts Postgres through testcontainers, or
  uses `CC_TEST_DATABASE_URL`.
- `e2e/` drives the real `cc` binary against a fake `gh` and `tp`, under the `e2e` build tag.

## Boundaries: never hand-edit

- `internal/store/ccdb/**` → `just sqlc`, committed with the query or migration change
- `internal/web/assets/dist/**` → `just assets`, gitignored; a binary built without it is unstyled
  and has no keys or islands, so `just build` and `just run` depend on `assets`
  ([ADR 16](docs/adr/0016-built-assets-are-generated-not-committed.md))
- `internal/web/testdata/` goldens → `go test ./internal/web -update`, then read the diff
- `.github/**`, `go.mod`, `go.sum` are deny-listed for agent pushes. Work that needs a CI change or
  a new Go dependency stops and says so.
