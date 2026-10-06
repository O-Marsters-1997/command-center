# 2. A repo is a remote, and a clone carries its own config

**Date:** 2026-08-27 · **Status:** accepted

## Context

The app inferred its workspace from the config path (`filepath.Dir` twice), required every `[[repo]]`
to exist on disk already, and named the state directory after the root's basename. Running `cc` from
inside the repo silently resolved a different root and database. The config and the
resolve-merge-conflict procedure also lived outside version control, so a fresh clone had neither.

A repo's identity already came from its remote (gh resolves it from `origin`); only the checkout's
location was configured, and no other machine can supply that.

## Decision

- A repo has a `name` and a `remote`, cloned to `<data_dir>/repos/<name>`. `path` (absolute, or
  relative to the config's directory) is the alternative. Exactly one is required; both resolve to one
  `checkoutPath`.
- `data_dir` is a config key, `CC_DATA_DIR`, or `os.UserConfigDir()/command-centre`, split into
  `state/` and `repos/`.
- The app never resets, pulls or checks out a checkout: it clones if absent, fetches if `origin`
  matches, and refuses to start otherwise.
- The config is tracked at `cc/config.toml`, the `--config` default. `CC_AGENT_COMMAND` replaces
  `agent_command` as a JSON array (a shell split could silently mis-split). The argv must name a
  permission mode, `{agents}` and `{system_prompt}` or startup refuses.
- The resolve-merge-conflict skill is tracked at `cc/skills/resolve-merge-conflict/SKILL.md`, not
  under `.claude/skills/`, whose symlinks `npx skills` manages. `.treepad.toml` stays gitignored,
  because the app depends on no key in it. The binary is `./bin/cc`.

## Consequences

One config runs on a laptop and a fresh VPS with `path` swapped for `remote`. `path` survives because
git records absolute paths in worktrees; a symlink at `<data_dir>/repos/<name>` keeps existing ones.

Unverified: with `.treepad.toml` untracked, a worktree cut in a fresh clone gets no
`.claude/settings.local.json`, and in `-p` mode an unpermitted tool is denied, so an agent in a
container may quietly lose capabilities.
