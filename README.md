# Command Centre

Local app that drives a DAG of tickets across repos to reviewable pull requests
using agents, one worktree per ticket. It launches agents, watches them, pushes
what they produced, reads CI, and shows one page. It never merges.

```
go run ./cmd/cc --config ~/Documents/plain/.claude/command-centre.toml
```

## Status

Phase 1 is part-built. Config, store, tick, page, unlock, launch preview, consent,
cut, spawn, liveness, disposition and crash recovery are in (issues #1, #2, #4, #11,
#12). Still open: push policy and PR create (#5), the CI verdict (#6), the
two-ticket slice end to end (#7), the sandbox fixture repo (#8). `tp remove --force`
is still the one upstream change needed, for teardown.

## Docs

| File | What |
|---|---|
| [docs/prd-command-centre.md](docs/prd-command-centre.md) | What we're building and why. Start here. |
| [docs/command-centre-design.md](docs/command-centre-design.md) | The design, revision 4. Normative for mechanism, states and invariants. |
| [docs/prd-operator-surface.md](docs/prd-operator-surface.md) | The page and the operator surface. Part A lands inside Phase 1. |
| [plans/command-centre-phase-1.md](plans/command-centre-phase-1.md) | The Phase 1 build plan: module boundaries, schema, testing strategy. |

### Reviews

| File | What |
|---|---|
| [phase-3-code-structure-review.md](docs/architecture-reviews/phase-3-code-structure-review.md) | Review of the built code at end of Phase 3. Module boundaries and file structure. |
| [revision-3-foundations-review.md](docs/architecture-reviews/revision-3-foundations-review.md) | Staff review of revision 3. Drove revision 4. Nine must-fixes. |
| [revision-2-buildability-review.md](docs/architecture-reviews/revision-2-buildability-review.md) | Staff review of revision 2. Four of its six must-fixes are dissolved by revision 3, see §15. |
| [revision-2-assessment.md](docs/architecture-reviews/revision-2-assessment.md) | Verdict summary, genre comparison, adaptability. Written against revision 2. |

## Commands

```
just test        # go test ./...
just test-e2e    # go test -tags=e2e ./e2e/...
just lint        # golangci-lint in docker
just ci          # conflicts, build, lint, test, e2e
```
