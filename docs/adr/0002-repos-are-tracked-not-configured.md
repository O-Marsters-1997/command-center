# 2. Repos are tracked, not configured, and each carries its own settings

**Date:** 2026-10-08 · **Status:** accepted

## Context

The app's config describes the daemon: where its data lives, how it starts an agent, how many run at
once. Which repos it drives, and the rules an agent works under in each, belong to the operator and to
each repo's owners. A per-repo setting held in the app's config would be a change to what agents may
touch, reviewed by no one who owns the repo and read once at startup.

## Decision

- **A repo is tracked**, a row in `repos` named `owner/name` with its remote, added from the Repos
  page. The app clones it to `<data_dir>/repos/<owner>/<name>`. It never resets, pulls or checks out a
  checkout: it clones if absent, fetches if `origin` matches, and refuses otherwise.
- **The config describes the daemon.** It is tracked at `cc/config.toml`, the `--config` default.
  `data_dir` is a key, `CC_DATA_DIR`, or `os.UserConfigDir()/command-centre`. `CC_AGENT_COMMAND` is a
  JSON array, since a shell split could silently mis-split, and the argv must name
  `--permission-mode`, `{agents}`, `{system_prompt}` and `{model}` or startup refuses.
- **A repo's settings live in `.command-centre.toml`** at its root, read from `origin/main`, never from
  a ticket's branch or worktree. The observer reads them after its fetch into `plan.Observation`, and
  `plan.RulesFor` builds `Rules` from them, so the loop and the board derive with the same settings and
  a change on `origin/main` takes effect next tick.
- A missing file answers defaults. A parse error, an unknown key or an invalid predicate is a read
  error: it becomes the tick's last error and the repo is skipped for every act step that tick.
- The settings file's own path is always denied to pushes, whatever the repo's `deny` says.
- The skills the app's agents run are tracked under `cc/skills/`, not `.claude/skills/`, whose
  symlinks `npx skills` manages.

## Consequences

One config runs on a laptop and a fresh VPS. Tightening a repo's guardrails is a pull request to that
repo; loosening one is too, and the file is denied to agents, so only a human can merge it. A repo
with no settings file runs on defaults: no verdict and only the built-in deny list.
