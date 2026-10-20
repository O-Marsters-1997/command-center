# Command Centre

*An orchestration harness: scope a feature, break it into tickets, launch it, and watch it land as
atomic pull requests in the order their blockers allow.*

Command Centre takes a feature's tickets as a dependency graph and runs a coding agent on each one,
in its own git worktree. From one page it cuts the worktree, spawns the agent, pushes what the agent
produced, opens the pull request, reads CI, and restacks children when a parent merges. Every PR
stops at reviewable and a human merges it. It runs on a laptop or an always-on host.

## Prerequisites

- Go 1.26.6, [`just`](https://github.com/casey/just) and [`bun`](https://bun.sh)
- Docker, for the Postgres the store runs on
- [`gh`](https://cli.github.com), authenticated, for every PR read and write
- `tp` (treepad), which cuts and removes the per-ticket worktrees
- The [`claude`](https://docs.claude.com/en/docs/claude-code) CLI, which every agent runs as

Every tracked repo must be squash-merge only. Startup refuses a repo that allows merge commits or
rebase merges.

## Installation

```sh
git clone https://github.com/O-Marsters-1997/command-center.git
cd command-center
just build    # builds the web assets, then bin/cc
```

`go install` won't give you a working binary, because `just assets` generates the CSS and JS and
they are not committed.

## Usage

Start Postgres, then run the read-only sample. It renders the page but spawns nothing:

```sh
just up
just run --config docs/command-centre.sample.toml
```

Open http://127.0.0.1:7777 and track a repo at `/repos`. Its features and tickets appear on the
board.

To run agents, drop `--config`. The default, `cc/config.toml`, spawns `claude`:

```sh
just run
```

Pick a feature, select its tickets and launch. Each ticket moves to running, then to a PR waiting
for review.

## Configuration

Anything machine-specific is an environment variable, so one tracked config runs on any machine:

| Variable | What |
|---|---|
| `CC_DATA_DIR` | Where state and repo checkouts go. |
| `CC_DATABASE_URL` | The Postgres to connect to, instead of the `compose.yaml` one. |
| `CC_AGENT_COMMAND` | A JSON array replacing `agent_command`, for a local wrapper. |

[docs/configuration.md](docs/configuration.md) covers every config key, the `agent_command`
placeholders and the per-repo `.command-centre.toml`.

### Running on a server

The page binds `127.0.0.1` and has no login. On an always-on host, put the checked-in `Caddyfile`
in front of it for TLS. See [docs/deployment.md](docs/deployment.md).

## How it works

`cc` is one binary: a reconcile loop and an HTTP server sharing a Postgres.

- A feature is a GitHub label, and its tickets are the open issues carrying it. GitHub's "blocked
  by" links make them a DAG, and a ticket unlocks once its blockers have open or merged PRs.
- Each ticket gets its own worktree, cut by `tp`, and its agent works there.
- Every 15 seconds the loop observes git and GitHub. It pushes finished work, opens PRs, derives a
  CI verdict, and restacks children onto `main` after a parent squash-merges.
- The page only queues intents. A button writes one row and the next tick acts on it, so the loop
  is the only writer of reconciled state.
- The store holds facts. Each row's state label is derived from them on every render.

The [design doc](docs/designs/command-centre-design.md) has the full mechanism, states and
invariants.

## Limitations

- Agents are Claude Code, PRs are GitHub and worktrees are `tp`. None of them is pluggable yet.
- Tracked repos must be squash-merge only.
- It never merges, by design.

## Development

```sh
just test       # Go unit tests and the web tests
just test-e2e   # the real binary against a fake gh and tp
just lint       # golangci-lint in Docker
just ci         # everything CI runs
```

[AGENTS.md](AGENTS.md) holds the conventions, layout and boundaries for contributors and agents.

## Docs

| File | What |
|---|---|
| [docs/prds/prd-command-centre.md](docs/prds/prd-command-centre.md) | What is being built and why. |
| [docs/designs/command-centre-design.md](docs/designs/command-centre-design.md) | The design. Normative for mechanism, states and invariants. |
| [CONTEXT.md](CONTEXT.md) | The glossary. |
| [docs/adr/](docs/adr/) | Architecture decisions. |
| [docs/configuration.md](docs/configuration.md) | The full configuration reference. |
