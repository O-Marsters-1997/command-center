# AGENTS.md

A local control plane that drives a DAG of tickets to reviewable pull requests using agents, one
worktree per ticket.

Glossary: `CONTEXT.md`. Mechanism: `docs/designs/command-centre-design.md`. Decisions:
`docs/adr/`.

## Approach

- **Load the `go-idiomatic` skill before editing any `.go` file**, including tests. Existing code
  that breaks it is not licence to match it.
- Comments follow `~/.claude/rules/comments.md`, which outranks any skill's comment guidance.
- **Write source files with Edit or Write, never Bash heredocs, `sed -i` or `perl -pi`.** The global
  `require-go-skills` and `check-written-comments` hooks only see Edit and Write, so a shell edit
  skips both checks.
- Read existing files before writing code. Prefer editing over rewriting.
- Test your code before declaring done.
- Before a structural decision (a new package, an import across a boundary, a naming ambiguity),
  check `CONTEXT.md` and `docs/adr/`. Routine work does not need them.
- Use **bun** for the JS in `web/`. Never npm, pnpm or yarn.

## Layout

One binary, `cmd/cc`. `internal/app` is its composition root: it builds the store, loop and web
server and wires them together.

- `internal/plan`: the decisions, pure functions over value types. **Only `plan` derives**
  ([ADR 15](docs/adr/0015-only-plan-derives.md)): `Rules.Derive(Input)` returns the `Snapshot`
  holding every ticket's state, reason and offered verbs. `plan` imports only `spend` and `verdict`,
  never anything impure.
- `internal/loop`: the imperative shell, the tick loop and its steps. **The loop is the only writer
  of reconciled state** ([ADR 11](docs/adr/0011-the-loop-owns-reconciled-state.md)): anything a tick
  observes or derives from, or a page renders. Verbs queue intents for it. `users` and `sessions` sit
  outside, which is why `cc useradd` writes directly.
- `internal/store`: Postgres. It returns `plan.Input` (`Store.PlanInput`) and holds the generated
  `internal/store/ccdb` unexported. Migrations live in `internal/store/migrations` (goose).
- `internal/web`: handlers and `html/template` pages. `internal/web/view` builds what a template
  renders. The web derives per render through `plan` and caches nothing between requests.
- `internal/gh` is the only package that knows the gh CLI's JSON shape. `internal/tracker` owns
  reading an issue tracker, so nothing above it knows GitHub exists. `internal/verdict` evaluates a
  repo's check predicate over a normalised snapshot, never gh's raw JSON.
- Leaves that import no other `internal` package: `command` (runs a CLI), `runner` (agent process
  groups), `agentlog` (reads a run's stream-json), `auth`, `verdict`. `spend` imports only
  `agentlog`.
- `internal/config` loads the TOML config and a repo's `.command-centre.toml`. `internal/cctest`
  hands a test its own Postgres database. `internal/demo` is the headless sim behind `just demo`.

## sqlc

- SQL is generated from the schema ([ADR 5](docs/adr/0005-sql-is-generated-from-the-schema.md)).
  sqlc reads the schema from `internal/store/migrations` and the queries from
  `internal/store/queries`, one `.sql` per store Go file, and generates `internal/store/ccdb`.
- `just sqlc` regenerates. Commit the generated code with the query or migration change; never
  hand-edit it. A query sqlc cannot parse stays hand-written on `s.db`.
- Add a migration with `just migrate-create <name>`. Never edit an applied one.

## The frontend stack

Read this before touching anything visual. The stack is unusual and the wrong assumption is
expensive.

- **Go `html/template` renders every page.** The templates are `internal/web/*.tmpl`. There is no
  React, no Next, no app framework.
- **htmx drives updates.** The board polls itself every five seconds and swaps its own `outerHTML`.
  Never remove or rename an `hx-` attribute, `id="board"`, or the `hx-preserve` detail row while
  doing styling work.
- **Two Solid islands exist**, `web/src/graph.tsx` and `web/src/launch-modal.tsx`, compiled with
  `solid-element`. Both opt out of shadow DOM
  ([ADR 1](docs/adr/0001-one-global-stylesheet.md)), so their classes are page-global.
  `solid-js` in `web/package.json` is those islands and nothing more. `web/src/layout.ts` holds
  the DAG layout math they share; neither island imports the other.
- **Tailwind v4, CSS-first.** `web/app.css` holds `@import "tailwindcss"`, an `@theme` block of
  oklch tokens, and a `[data-theme="dark"]` override. There is no `tailwind.config.*`, and there
  will not be one.
- **Go never depends on Node.** `test`, `e2e` and `lint` are Go-only.

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

A styling change is not finished until `just assets` has run and the rebuilt
`internal/web/assets/dist/app.css` is committed. CI diffs it.

## Tests

- `go-idiomatic`'s Testing section is the authority on how a test is written: black-box through the
  exported API, asserting on outputs and state, with the most faithful double (real, then fake, then
  stub, then mock). Use the `tdd` skill for the loop.
- Store and loop tests run against a real Postgres. `internal/cctest` starts one in Docker through
  testcontainers, or uses `CC_TEST_DATABASE_URL` when set. Docker must be running.
- Golden files in `internal/web/testdata/` regenerate with `go test ./internal/web -update`. Read
  the diff; it is the review.
- The end-to-end scripts in `e2e/` drive the real `cc` binary against a fake `gh` and `tp`
  (`e2e/README.md`). They run under the `e2e` build tag.

## Commands

`just --list` has them all. The ones you will use:

```
just test          # go test ./...
just test-e2e      # go test -tags=e2e ./e2e/...
just lint          # golangci-lint in Docker
just fmt           # go fmt ./...
just tidy          # go mod tidy
just sqlc          # regenerate internal/store/ccdb
just assets        # bun install && bun run build, in web/
just up / down     # the Postgres the app connects to
just migrate-up    # also migrate-status, -down, -redo, -create <name>, -reset
just ci            # conflicts check, build, lint, test, e2e
```

## Gotchas

- **The repo is squash-merge only.** A merged branch is not an ancestor of `main`, so GitHub's PR
  state is the only honest merge test.
- **Open pull requests as drafts.**
- Use the domain terms in `CONTEXT.md` (Ticket, Ref, Run, Verb, Intent, Tick); read the relevant `docs/adr/`
  before changing an area.

## Boundaries: never hand-edit

- `internal/store/ccdb/**` → `just sqlc`
- `internal/web/assets/dist/app.css` → `just assets`
- `internal/web/testdata/` goldens → `go test ./internal/web -update`
- `.github/**`, `go.mod`, `go.sum` → deny-listed for agent pushes (`.command-centre.toml`). Work
  that needs a CI change or a new Go dependency cannot be completed by an agent: stop and say so
  rather than working around it.
