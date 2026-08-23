# The end-to-end harness

Fifteen
[testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)
scripts drive the real `cc` binary against a real git repository, a fake `gh`
and a fake `tp`. Each one asserts a behaviour the unit tests cannot reach,
because it needs the binary, the filesystem, the database and the process
runner all at once.

```
just test-e2e     # go test -tags=e2e ./e2e/...
```

The `e2e` build tag gates the testscript run only. `go test ./...` still
compiles and runs this directory's other tests, which cover the two fakes and
the fake agent scripts directly.

## How the substitution works

`TestMain` builds three binaries into one temp directory and puts it first on
`PATH`:

| Built from | Installed as |
|---|---|
| `./cmd/cc` with `-tags=e2e` | `cc` |
| `./e2e/fakegh` | `gh` |
| `./e2e/faketp` | `tp` |

That is the whole mechanism. Production code never learns it is under test, and
no test-only environment variable exists in `internal/cc`. Even the state dir
redirects on its own, because `os.UserConfigDir` honours `HOME` and every script
runs with `HOME` set to its own work directory.

## Layout

```
tests/*.txtar             one script per behaviour
testdata/config/*.toml    the config a script installs with cc-config
testdata/gh/*.json        the fixture the fake gh answers from
fakegh/                   the fake gh: fixture in, stdout and exit code out
faketp/                   the fake tp: delegates to real git
register/                 the e2e-only cc subcommands
../testdata/agents/*.sh   the fake agents a config's agent_command points at
```

## The e2e-only subcommands

`cc tick` and `cc request` exist only in a build tagged `e2e`. The release
binary's surface is the loop plus the page, and `cmd/cc/subcmd_prod.go` is what
keeps it that way.

`cc tick --count N` takes the flock and runs the loop exactly N times with no
sleeping, so a script asserts against a known number of ticks rather than
against the wall clock.

`cc request [-origin URL] [-form BODY] METHOD PATH` prints what a real HTTP
client gets back from the real handler, over an ephemeral httptest port so
scripts can run in parallel. It does not take the flock, so it can read while
`cc tick` or a daemon holds it. `-origin` sends a foreign `Origin` header, which
is how the CSRF refusal is tested. `-form` posts a url-encoded body, which is
how a route gets driven the way the page's own forms drive it.

## Script commands

Beyond testscript's builtins, with `RequireExplicitExec` on so every external
command is spelled `exec`:

| Command | What |
|---|---|
| `cc-init-repo` | Creates `$WORK/repo` with one commit and `$WORK/remote.git` as its origin. Observe starts with `git fetch origin --prune`, so without a remote every script fails before its assertion. |
| `cc-config <name>` | Installs `testdata/config/<name>.toml` at cc's default path, prepending `port = 0` and substituting `{{agents}}` with the repo's `testdata/agents` directory. No script ever passes `--config`. |
| `cc-fake-gh <name>` | Stages `testdata/gh/<name>.json` as the fake gh's fixture. Read at exec time, so a script can restage it between ticks. |
| `cc-fake-gh-head <name> <branch>` | The same, with `{{head}}` replaced by that branch's current tip. This is how a verdict fixture points `headRefOid` at a SHA the script cannot know in advance. |
| `cc-daemon` | Starts a real `cc` in the background and returns only once it logs `serving http://`, meaning it holds the flock. Polling rather than sleeping is what keeps the second-instance assertion off a race. |

## The fakes

`fakegh` answers from the JSON at `$CC_GH_FIXTURE`, keyed on the first two argv
words (`pr list`, `pr create`, `api repos/{owner}/{repo}`) or on a longer argv
prefix when one shape of a call needs distinguishing from another. The longest
matching key wins. Each entry is `{"stdout": ..., "stderr": ..., "exit": ...}`,
all optional. Every call is appended to `$CC_GH_LOG` before the fixture is
consulted, so an unanswerable call still shows in the log, and `grep -count=`
against that log is how most scripts assert on side effects.

`faketp` implements `new` and `remove` by delegating to real git, so worktree
layout and branch-deletion refusals are genuine rather than simulated. It places
worktrees where treepad does, as siblings of the repo named
`<repo-slug>-<branch-slug>`. It removes the worktree before deleting the branch,
which is the order real tp uses and the reason a non-forced remove of a
squash-merged branch leaves the branch behind. Calls are logged to `$CC_TP_LOG`.
Setting `CC_TP_FAIL` makes every call fail after logging.

## The fake agents

`testdata/agents/` sits at the repo root, not under `e2e/`, because the unit
tests use it too. The argv contract is `$1` worktree, `$2` settings path, `$3`
prompt file.

| Script | What it does |
|---|---|
| `commits.sh` | Commits one file and exits 0. The ordinary happy path. |
| `commits_ci.sh` | Commits under `.github/workflows`, so the push policy refuses it. |
| `empty.sh` | Does nothing, leaving HEAD where it was. |
| `sleeps.sh` | Backgrounds a long sleep and waits for a signal, so kill and pgid reaping are testable. |

`commits.sh` and `empty.sh` append their own invocation to `$CC_AGENT_LOG`. That
is a script's proof of how many times an agent actually ran, which is a
different fact from tp.log's cuts or the `events` table's `run_launched` rows.

## Environment

Every script, and everything it starts, runs with:

| Variable | Why |
|---|---|
| `HOME=$WORK` | Redirects the state dir, since `os.UserConfigDir` honours it. |
| `GIT_CONFIG_NOSYSTEM`, `GIT_AUTHOR_*`, `GIT_COMMITTER_*` | So `cc-init-repo` can commit on a CI runner with no gitconfig at all. |
| `CC_GH_FIXTURE`, `CC_GH_LOG` | The fake gh's input and log. |
| `CC_TP_LOG` | The fake tp's log. |
| `CC_AGENT_LOG` | The fake agents' log. |

## Adding a script

1. Write the intent as the comment at the top of the `.txtar`. Every existing
   script opens with the behaviour it pins and, where it exists, the invariant
   it comes from. That comment is the test's actual specification.
2. Reuse a config and a gh fixture if one fits. Add a new one only when no
   existing shape does.
3. Open with `cc-init-repo`, `cc-config <name>`, `cc-fake-gh <name>`.
4. Drive the app with `exec cc tick --count 1` and read it with
   `exec cc request GET /`. One tick per assertion beats one tick doing several
   things, because a failure then names the step.
5. Assert on the page's HTML, on `GET /events`, and on the fake logs. The three
   together are how a script distinguishes "the app decided this" from "the app
   actually did this".

A few scripts contain `exec sleep 0.5`. It is always waiting for a real spawned
agent process to exit between ticks, never for the app's own timing. Nothing in
the app is polled for.
