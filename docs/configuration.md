# Configuration

Command Centre reads two kinds of config: the daemon's own TOML file, passed with `--config`, and a
`.command-centre.toml` in each tracked repo. Repos themselves are not configured: track one from
the page at `/repos`.

## The daemon config

`--config` defaults to `cc/config.toml`, the config this project runs itself on. It names no repo
and nothing machine-specific, so the same file works on a laptop and on an always-on host.
`docs/command-centre.sample.toml` is a read-only variant that spawns nothing.

| Key | Default | What |
|---|---|---|
| `data_dir` | `CC_DATA_DIR`, else `$UserConfigDir/command-centre` | Where state and repo checkouts live. A leading `~` expands. |
| `database_url` | `CC_DATABASE_URL`, else the `compose.yaml` server | The Postgres the store runs on. |
| `claude_projects_dir` | `~/.claude/projects` | Where the Claude CLI writes session transcripts, read to weigh actual spend. |
| `max_agents` | 1 | How many agents may run at once. |
| `port` | 7777 | The page's port. It always binds `127.0.0.1`. |
| `agent_command` | the `claude` argv below | The argv each run spawns. `[]` disables spawning. |
| `max_turns` | no cap | Appended to `agent_command` as `--max-turns`. |
| `spend_limit_5h` | 0 (off) | A five-hour usage percentage, 0 to 100, above which no new agent spawns. |
| `board_poll_seconds` | 5 | How often the board polls. |

### Environment variables

| Variable | What |
|---|---|
| `CC_DATA_DIR` | Fallback for `data_dir`. |
| `CC_DATABASE_URL` | Fallback for `database_url`. |
| `CC_AGENT_COMMAND` | A JSON array that replaces `agent_command` wholesale, for a machine-local wrapper. |

```sh
CC_AGENT_COMMAND='["caffeinate","-i","claude","-p","{prompt}","--permission-mode","auto","--append-system-prompt-file","{system_prompt}","--model","{model}"]' just run
```

### `agent_command`

The default:

```toml
agent_command = [
  "claude", "-p", "{prompt}",
  "--output-format", "stream-json", "--verbose",
  "--settings", "{settings}",
  "--append-system-prompt-file", "{system_prompt}",
  "--permission-mode", "auto",
  "--model", "{model}",
  "--effort", "{effort}",
  "{session}",
]
```

A non-empty argv must contain `--permission-mode`, `{system_prompt}` and `{model}`, or startup
refuses it. The runner substitutes these placeholders into every element:

| Placeholder | Becomes |
|---|---|
| `{prompt}` | The composed prompt, as text. |
| `{prompt_file}` | A path to the same prompt. |
| `{worktree}` | The ticket's worktree path. |
| `{settings}` | The app-owned deny settings file. |
| `{system_prompt}` | The system prompt file. Dropped, with the flag before it, when there is none. |
| `{model}` | The ticket's model: `claude-sonnet-5-5` unless the ticket picks `opus`, `sonnet` or `haiku`. |
| `{effort}` | The ticket's effort. Dropped, with the flag before it, when the ticket sets none. |
| `{session}` | A whole element: `--session-id <id>`, `--resume <id>`, or nothing. |

Give Claude Code `{prompt}`, not `{prompt_file}`. It expands a slash command only when the prompt
arrives as argv text, so a path is read as inert text and `/implement` never fires.

### Where state lives

```text
state/command-centre.lock         flock, one instance per data dir
state/runs/<id>.jsonl             one per run: agent stdout and stderr
state/runs/<id>.prompt            the prompt that run was given
state/settings/agent.json         the deny settings passed to every spawn
state/settings/system-prompt.md   the system prompt passed to every spawn
repos/<owner>/<name>/             a repo's checkout, with the worktrees tp cuts beside it
```

Startup clones a tracked repo's checkout when it is absent, and refuses to start when the directory
holds a different repository. The app never resets, pulls or checks out a checkout: it fetches,
reads remote-tracking refs, and leaves the work to the worktrees.

## Per-repo `.command-centre.toml`

Read from the repo's `origin/main` on every tick, never from a branch. A missing file means
defaults. A malformed file, or one with an unknown key, skips that repo for the tick and shows the
error with its line. The file's own path is always denied to agent pushes.

| Key | Default | What |
|---|---|---|
| `tracker` | `"github"` | Where features and tickets come from. GitHub issues are the only tracker. |
| `stacking` | `false` | When a ticket's one same-repo blocker has an open PR, branch from that PR's branch instead of `main`. |
| `deny` | none | Path patterns added to the push policy's default refusals. |
| `checks` | none | The predicate over CI checks that makes a PR green. |
| `mergify_sha` | none | The `sha256` of `.mergify.yml` that `checks` was written against. On a mismatch, rows hold at `checking` rather than claim green. |
| `compat_check` | none | The check reporting whether a consumer still builds against its producer. Holds a cross-repo consumer's PR in draft until it passes. |
| `verify_command` | none | An argv run after a clean merge-forward or restack, before the row counts as sound. No shell: chain steps with `["sh", "-c", "..."]`. |

A repo with neither `checks` nor `mergify_sha` stops at `checking` and never derives a verdict.

`checks` composes `success`, `skipped`, `absent_ok` and `author` leaves with `all_of`, `any_of`
and `not`. This repo's own file:

```toml
stacking = true
deny     = [".github/**", "go.mod", "go.sum"]

[[checks.all_of]]
success = "test"
[[checks.all_of]]
success = "lint"
[[checks.all_of]]
success = "build (ubuntu-latest)"
```
