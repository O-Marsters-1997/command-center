# 3. A repo is a remote, not a path

**Date:** 2026-08-27 · **Status:** accepted, not implemented

## Context

The app infers where everything is. `ResolveWorkspace` takes the config path and calls
`filepath.Dir` twice to get a workspace root, then every `[[repo]]` is a path relative to that root
that has to exist on disk already. The state directory is named after the root's basename. Nothing
in the config says how to produce any of those directories, so the config describes one laptop's
tree rather than a system that can be rebuilt.

The inference is already failing on the machine it was written for. The `--config` flag defaults to
`.claude/command-centre.toml`, relative to the working directory, so running `cc` from inside the
repo resolves a different root, names a different state directory, and opens a different database.
Both exist on Olly's laptop right now. One holds the real run history and one is an accident, and
the app never said a word about it.

Beneath that, a repo is not really a path. `internal/gh` and `repocheck.go` shell out with
`cmd.Dir` set to the checkout and let gh resolve `repos/{owner}/{repo}` from `origin`, so the app
has always taken a repo's identity from its remote. Only the checkout's *location* was configured,
and that is the part no operator on another machine can supply.

The structure is friendly to changing this. Two functions derive a path, `ResolveWorkspace` and
`repoPathsByName` (plus two sites that inline the same join). Every other path-taking call site is
handed a resolved `repoPath` and shells out. `Worktrees()` already asks git where treepad put a
worktree rather than predicting it, so nothing downstream of a checkout assumes a layout either.

`docs/designs/location-independence.md` has the full inventory, the cross-repo walkthrough, and the
config migration.

## Decision

A repo is identified by `name` and located by `remote`, a git URL the app clones to
`<data_dir>/repos/<name>`. `path` survives as the alternative to `remote`, absolute or relative to
the config file's own directory. Exactly one of the two is required, and setting both is refused at
config load. Both resolve to a single `checkoutPath` field, so nothing downstream branches on which
form was written.

`data_dir` is an explicit config key, or `CC_DATA_DIR`, or a default of
`os.UserConfigDir()/command-centre`. It replaces the grandparent rule entirely, and it splits into
`state/` for the database, lock, runs and settings, and `repos/` for checkouts and the worktrees tp
cuts beside them.

The app never resets, pulls or checks out a repo checkout. It clones when the directory is absent,
fetches when it is present and `origin` matches, and refuses to start when `origin` does not match
or the directory is not a git repo. The checkout is a ref cache. Work happens in worktrees.

Files that decide how an agent behaves are tracked in the repo they apply to. `.treepad.toml` comes
out of `.gitignore` and gets committed. The resolve-merge-conflict skill moves into the repo at
`skills/resolve-merge-conflict/SKILL.md`. There is no shared location and no per-repo override.

## Consequences

The same config runs on a laptop and on a fresh VPS with one key changed, `path` for `remote`. A
machine with no tree at all can be brought up from the config alone, which is the precondition for
dockerising later.

Olly's current setup migrates without moving a file. `data_dir` points at the state directory the
grandparent rule already produced, `path` goes absolute, and the six live treepad worktrees keep
working. Those worktrees are why `path` survived at all: git records absolute paths in each
worktree's `.git` file, so relocating the checkout would break every one of them.

Tracking the agent-behaviour files deletes a problem rather than fixing it. `[sync].include` exists
to carry untracked files into a new worktree, and a tracked file arrives with the clone. The cost
is that a second repo wanting the same conflict procedure copies the file instead of inheriting it.
That is 56 lines, and per-repo divergence in how conflicts get resolved reads to me as a feature.

Refusing to start on a remote mismatch is a new way for the app to fail at startup. It matches the
posture `AssertReposSquashOnly` already sets, and reconciling a config against the wrong repository
silently is the worse outcome.

Two things this does not buy. It does not make the app multi-repo: `NewObserver` still keys
branches globally rather than by `(repo, branch)`, so two repos sharing a branch name still
collide. And the seam retirement path still has no test in practice, because the live config
configures no seams.

Reverting means putting the grandparent rule back and dropping `remote`, which gets harder once a
config has shipped with `data_dir` in it, and harder again once anything runs off a clone the app
made rather than a tree Olly made.
