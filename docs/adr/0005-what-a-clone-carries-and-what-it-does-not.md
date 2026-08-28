# 5. What a clone carries, and what it does not

**Date:** 2026-08-27 · **Status:** accepted

## Context

Everything a human authored to make this app work lived outside version control. The config sat
at `ai-development/.claude/command-centre.toml`, in a directory `.gitignore` excludes, two levels
above the repo. The resolve-merge-conflict procedure sat at
`ai-development/.claude/skills/resolve-merge-conflict/SKILL.md`, in a directory that is not a git
repository at all. Both existed on one laptop and nowhere else. A fresh clone got neither, and
nothing in the repo said they were missing.

Phases 3 and 4 made the app find its data and its checkouts from the config alone. That is only
worth anything if the config itself arrives with the clone.

## Decision

The config is `cc/config.toml`, tracked, and `--config` defaults to it. It names its repo by
`remote`, so the file that runs on this laptop is the file that runs on a machine with nothing
checked out.

Two environment variables carry everything machine-specific. `CC_DATA_DIR` says where `state/`
and `repos/` go. `CC_AGENT_COMMAND` replaces `agent_command` wholesale, as a JSON array, and is
where the laptop's `caffeinate` and `safehouse` wrapper goes. JSON rather than a shell-style
split because an argv element may one day contain a space, and a silent mis-split is a worse
failure than a parse error.

The resolve-merge-conflict skill is `cc/skills/resolve-merge-conflict/SKILL.md`, tracked. It sits
under `cc/` rather than `.claude/skills/`, because that directory is entirely gitignored and its
contents are symlinks into `.agents/skills` managed by `npx skills`, which would clobber a real
directory placed among them. No code reads it yet: `resolveSkillPath` arrives with
`cc-agent-resolves-refresh-conflict`, and this is the file move alone.

`.treepad.toml` stays gitignored. The app calls `tp new` and `tp remove` and passes neither
`--open` nor `--ticket`, so it depends on no key in that file.

The built binary moves from `./cc` to `./bin/cc`, because a directory named `cc` and a file named
`cc` cannot share the repo root.

## Consequences

A clean clone plus `CC_DATA_DIR` reaches a serving state with nothing written by hand. That is
the precondition for a container, which is a separate task.

The thirteen `[[task]]` blocks are still in the tracked config, so the repo now records which
issues were in flight on 2026-08-27. Phase 7 replaces them with an import from the tracker and
they leave the file.

**Unverified, and it belongs to the container task.** Because `.treepad.toml` stays untracked, a
worktree cut in a fresh clone gets no `[sync].include`, and therefore no
`.claude/settings.local.json`. In `-p` mode an unpermitted tool is denied rather than prompted, so
an agent in a container may quietly lose capabilities it has on this laptop, and fail in ways that
read as the agent being bad at the task. Nobody has run an agent in a container yet, so this is a
prediction, not an observation.

Olly's laptop keeps six live treepad worktrees beside the existing checkout, and git records an
absolute path in each worktree's `.git` file. A fresh clone under `<data_dir>/repos/command-center`
would orphan all six. Symlinking that path at the existing checkout keeps them: `EnsureCheckout`
stats through the link, finds a matching origin, and fetches.
