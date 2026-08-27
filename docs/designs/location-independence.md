# Location independence: what a repo is, and where its files live

**Date:** 2026-08-27 · **Status:** design, not implemented

The app currently learns where everything is by inference. The workspace root is the config file's
grandparent directory, and each `[[repo]]` names a path under it that has to exist already. Every
directory the app touches was created by hand on one laptop, and nothing in the config says how to
recreate any of them. This document inventories that, then proposes a repo model that does not
depend on a pre-existing tree. ADR 0003 records the decision; this is the working behind it.

Dockerising is the reason to want this. It is not part of this task. No Dockerfile, no
credentials-from-env, no change to `agent_command`, no decision about where agents run.

## 1. What the app assumes today

Two functions derive a path. Everything else is handed one.

| Site | Assumption |
|---|---|
| `statedir.go:35` | Workspace root is `filepath.Dir(filepath.Dir(configPath))`. The config has to sit exactly two levels above the repos. |
| `statedir.go:41` | State dir is `os.UserConfigDir()/command-centre/<basename of root>`. The workspace's identity is a directory name. |
| `config.go:146` | `repoPathsByName` joins root and `repo.Path`. Called nine times: `loop.go:262`, `refresh.go:104`, `draftgate.go:26`, `push.go:42`, `verbs.go:151,220,301,350`, `server.go:88`. |
| `observe.go:63` | Joins root and `repo.Path` again, separately from `repoPathsByName`. |
| `repocheck.go:58` | Joins `ws.Root` and `r.Path` a third time, at startup. |
| `seams.go:62` | Seam text is `<root>/.claude/seams/<name>`. |
| `resolve.go` (on `cc-agent-resolves-refresh-conflict`, not yet on the default branch) | Conflict procedure is `<root>/.claude/skills/resolve-merge-conflict/SKILL.md`. |

Every other path-taking site consumes a resolved `repoPath` and shells out with `git -C`
(`git.go:295,311`) or `cmd.Dir` (`gh.go:176`, `repocheck.go:42`, `runner.go:86`). None of them
derives anything. That is why this change is small: fix the two derivations and the other forty
call sites do not move.

Three further facts worth having.

**Worktree discovery is already location independent.** `Worktrees()` (`git.go:26`) parses
`git worktree list --porcelain`, and `cutAndSpawn` (`loop.go:361`) asks git where treepad put the
worktree rather than predicting it. Invariant 5 says so explicitly. Whatever layout tp chooses, the
app follows. Preserve this.

**The GitHub identity of a repo already comes from the remote.** `checkRepoSquashOnly` runs
`gh api repos/{owner}/{repo}` with `cmd.Dir` set to the checkout, and gh resolves the placeholder
off `origin`. Same for every `gh pr list` and `gh pr view` in `internal/gh`. The app has never
needed a configured owner and repo, because the checkout carries one. That makes `remote` a
promotion of something the app already relies on, not a new concept.

**The grandparent rule has already produced two state directories.** On this machine:

```
~/Library/Application Support/command-centre/ai-development/   532K db, 3.9M wal   the live one
~/Library/Application Support/command-centre/command-center/   156K db             an accident
```

The flag defaults to `.claude/command-centre.toml`, relative to the working directory. Run `cc`
from inside the repo with no `--config` and the grandparent rule names a different workspace, opens
a different database, and starts reconciling against it. No error, no warning. That is the
inference failing on the machine it was designed for.

## 2. What a repo actually is

Strip it back. The app needs four things from a repo:

1. A checkout it can run `git fetch`, `git worktree list`, `git rev-parse` and `git show` in.
2. A directory `tp new` can run inside, so treepad cuts a worktree beside it.
3. A GitHub owner and repo, which gh reads off the checkout's `origin`.
4. `.mergify.yml`, read from the checkout's working tree (`observe.go:114`).

All four come from a checkout. So the app needs a checkout, and `path` is a bad way to name one. It
points at a directory the operator made, and says nothing about how to make another. A git URL says
both. Given `remote`, the app can produce the checkout itself.

Item 4 is the one wart. `mergifyHash` reads `.mergify.yml` off the working tree, so a checkout
sitting on a stale or dirty default branch hashes the wrong file. Every other read in the app goes
through a remote-tracking ref. It should read the file out of `origin`'s default branch and stop
caring what the working tree says. Small fix, not required by this design, worth doing in the same
implementation.

## 3. The proposed shape

### A repo is a remote the app clones

```toml
[[repo]]
name     = "command-center"
remote   = "git@github.com:O-Marsters-1997/command-center.git"
stacking = true
```

The checkout lives at `<data_dir>/repos/<name>`. The app names it, from `name`, which is already
the config's unique identifier for a repo. The operator names `data_dir` and nothing below it.

`path` survives as an alternative to `remote`, absolute or relative to the config file's own
directory. Exactly one of the two is required, and a repo setting both is refused at config load
rather than resolved by precedence.

I went back and forth on keeping `path`. The case for dropping it is one code path instead of two.
The case for keeping it is what decided me, and it is not convenience: the existing checkout has
six live treepad worktrees as siblings, git records absolute paths in each worktree's `.git` file,
so moving the checkout under a new `data_dir` breaks all six. `path` is what lets Olly point the
new config at the tree he already has and lose nothing. Local development against a dirty working
tree needs it too.

It is also cheaper than it looks. Both keys resolve to one `checkoutPath` field at config load, and
everything downstream reads that field. The only extra code is clone-if-absent, which runs once at
startup. There are not two paths through the loop, there is one resolution function and one ensure
function.

### Found an existing checkout

Clone-if-absent needs a defined answer for the case where the directory is already there. The app
never resets, pulls, or checks out anything in a repo checkout. It fetches, reads remote-tracking
refs, and cuts worktrees off `origin/<base>`. That is what it does today (`Fetch`, then `RevParse
origin/…`, then `tp new --base origin/…`) and it is right. The checkout is a ref cache, not a place
work happens. Work happens in worktrees.

So:

- Directory absent: clone.
- Directory present, a git repo, `origin` matches the configured `remote`: use it, fetch, say
  nothing.
- Directory present and `origin` does not match, or it is not a git repo at all: refuse to start.

Refusing is deliberate, and it mirrors `AssertReposSquashOnly`, which already refuses to start on a
repo whose merge settings would invalidate the design. Silently reconciling a config against the
wrong repository is worse than not starting. Local commits and a dirty tree are not the app's
business and are not checked, because nothing the app does reads them.

### An explicit data directory

`data_dir` replaces the grandparent rule. It comes from a config key, or `CC_DATA_DIR`, or a
default of `os.UserConfigDir()/command-centre`. The config file then has no bearing on where
anything lives, so it can sit inside the repo, which is where a config that is checked in belongs.

```
$CC_DATA_DIR/
  state/
    command-centre.db
    command-centre.lock
    runs/
    settings/
  repos/
    command-center/                     the checkout
    command-center-cc-98-tailwind/      a tp worktree, sibling of the checkout
```

The state and checkout split stays, and `state/` is a separate subtree from `repos/` rather than
its parent. That keeps the database two `../` from any worktree instead of one. A container
boundary retires the isolation argument in `statedir.go:10`, but the split costs one directory and
I would rather not hand an agent a shorter path to the database than it needs.

`Workspace.Name` dies with the grandparent rule. It exists to name the state directory, and nothing
outside tests reads it.

Seam text moves to `<dir of config file>/seams/<name>`, resolved from the config path directly
rather than from its grandparent. Seams are shared prompt text across repos, so they belong beside
the config, not inside any one repo.

## 4. Agent-behaviour config lives in the repo it applies to

Two files decide how agents behave and neither is in version control.

`.treepad.toml` is in `.gitignore`. A fresh clone gets no treepad config and `tp new` falls back to
defaults. Its `[sync].include` also omits `.claude/skills/**`, so a worktree gets none of the 37
skills that `.claude/skills` symlinks. It does get `.agents/skills/`, which is in the include list
and holds the same 37 skills under a different name, so the effect is narrower than "agents have no
skills": a worktree is missing the directory Claude Code documents and has the one `npx skills`
writes. Whether the agent loads from `.agents/skills` I have not verified, and it does not change
the answer below.

The resolve-merge-conflict SKILL.md is worse. It sits at
`ai-development/.claude/skills/resolve-merge-conflict/SKILL.md`, in a directory that is not a git
repo at all. It exists on one laptop and nowhere else.

**Decision: a repo carries its own copies. No shared location, no per-repo override.**

Three reasons.

It is the only form that survives `git clone` on a fresh machine, which is the whole point of the
task. A shared directory has to be provisioned out of band, and out-of-band provisioning is exactly
the failure this remodelling exists to remove.

`.treepad.toml` is already a per-repo file in the wrong place. Its `[sync].include` names this
repo's files (`.env.docker-compose`, `.vscode/*`), and its `[from_spec].ticket_url` names this
repo's issue tracker. Sharing it across repos was never coherent.

A shared-with-override scheme means two lookup paths and a precedence rule, for a problem with one
repo in it today. If a second repo wants the same conflict procedure, copy the file. It is 56
lines, and a procedure diverging per repo is the point rather than a defect.

Concretely:

- Un-ignore `.treepad.toml` and commit it.
- Move the resolve-merge-conflict skill into the repo, tracked.

Tracking it removes the sync problem instead of fixing it. `[sync].include` exists to carry
untracked files into a new worktree. A tracked file arrives with the clone and with every worktree,
and needs no sync entry at all.

Where it goes needs care. `.claude/` is entirely gitignored, and `.claude/skills/*` are symlinks
into `.agents/skills` managed by `npx skills`, so committing a real directory into that symlink
farm invites the skills tool to clobber it. Put the app's own procedure skills somewhere the app
owns and the skills tool does not touch, `skills/resolve-merge-conflict/SKILL.md` at the repo root,
and have `resolveSkillPath` read it relative to the repo checkout rather than the workspace root.

The 37 general-purpose skills are a separate question. They are Olly's, not the app's, and on a VPS
they come from the image or from `npx skills add` at build time. Out of scope here.

## 5. The cross-repo features, checked

**Seams.** `retirementsByName` (`seams.go:17`) needs `repoPaths[s.Repo]` to run `git show` against
the producer repo's default branch. Under the new model every configured repo has a checkout by
construction, because startup either found one or cloned one. That is better than today, where a
repo configured with a path that does not exist fails at first use rather than at startup. Seam
files move with the config. Nothing else changes. The live config has no seams, so this path stays
untested in practice, and this design does not change that.

**The draft gate.** `draftgate.go:26` resolves `repoPaths[t.Repo]` and `readyOne` runs gh in the
consumer repo's checkout. It consumes a resolved path and derives nothing. Unaffected.

**Cross-repo `blocked_by`.** Pure, in `internal/plan`, which imports nothing outside the standard
library and is held to that by `internal/plan/api_test.go`. It never sees a path. Unaffected.

The same holds for `push.go`, `retarget.go`, `verbs.go`, `repocheck.go` and `observe.go`: all take
a repo path and shell out. Only `observe.go:63` and `repocheck.go:58` derive one, and both switch
to the resolved field.

One real blocker for actually running a second repo, unrelated to this design and worth naming
because it looks related. `NewObserver`'s own comment says it: branches are keyed globally, not by
`(repo, branch)`, so two repos with a same-named branch collide in `obs.PRs`, `obs.Worktrees`,
`obs.BranchTips` and `obs.MidMerge`. `mainTipKey` works around it for the default branch alone.
Making repos location independent does not make the app multi-repo. That is its own task.

## 6. The live config, rewritten

Current, at `ai-development/.claude/command-centre.toml`, with the workspace root inferred as
`ai-development`:

```toml
max_agents = 3
port       = 7777

[[repo]]
name     = "command-center"
path     = "command-center"       # relative to an inferred root
stacking = true
deny     = [".github/**", ".golangci.yml", "go.mod", "go.sum"]
```

New, keeping the existing checkout so the six live worktrees survive. This is the migration Olly
runs on the laptop, and it moves no files:

```toml
data_dir   = "~/Library/Application Support/command-centre/ai-development"
max_agents = 3
port       = 7777

[[repo]]
name     = "command-center"
path     = "/Users/ollymarsters/Documents/personal/ai-development/command-center"
stacking = true
deny     = [".github/**", ".golangci.yml", "go.mod", "go.sum"]
```

`data_dir` points at the state directory the grandparent rule already produced, so the database,
run logs and settings are found where they already are and no run history moves. `path` is
absolute, so the config file itself is free to move into the repo. The `[[task]]` and
`[[repo.checks]]` blocks are unchanged, because nothing in them refers to a location.

The same config on a VPS, where no tree exists yet:

```toml
data_dir   = "/var/lib/command-centre"
max_agents = 3
port       = 7777

[[repo]]
name     = "command-center"
remote   = "git@github.com:O-Marsters-1997/command-center.git"
stacking = true
deny     = [".github/**", ".golangci.yml", "go.mod", "go.sum"]
```

Startup clones to `/var/lib/command-centre/repos/command-center`. Nothing else in the file differs.
That is the test of whether this worked: one key changes between a laptop and a fresh machine.

`docs/command-centre.sample.toml` needs the same treatment. It relies on the grandparent rule twice
over, sitting in `docs/` so the root resolves to the repo, then setting `path = "."`. Under the new
model it sets `path = ".."`, relative to `docs/`, and says so.

## 7. What this deliberately does not do

No Dockerfile, and no opinion on whether agents run in the daemon's container. No credentials from
the environment. No change to `agent_command`. No change to how gh authenticates. No fix for the
global branch keying in `observe.go`. No implementation: this branch is design only.
