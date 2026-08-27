# What the agents actually get wrong

_Pass 2, 2026-08-24. Companion to `docs/enforcement-inventory.md`. Method from
`docs/lit-review.md`._

Pass 1 asked what standards this repo claims. This asks what 37 merged
agent-authored PRs actually broke. The two answers are not the same list, and
where they disagree the frequency ordering here should win, because a rule
written from first principles is a hypothesis until agent output tests it.

## Method, and what it can and cannot see

I ran the candidate linters from pass 1 across HEAD with the `e2e` build tag on,
then `git blame`d every violation to the commit that introduced it and mapped
those to PRs. That gives exact counts and exact attribution for anything a linter
can express. For the rest I read the production Go diffs of PRs 61, 62, 64, 66
and 67 line by line and coded the failures in free text before mapping them onto
pass 1's rule ids.

Three limits worth stating.

The blame pass only sees violations that survived to HEAD. Anything an agent
introduced and a later PR fixed is invisible, which flatters the early PRs more
than the recent ones. Read the counts as "what leaked", not "what was written".

The whole repo is three days old, 22 to 24 August. There is no long-run trend to
extract, only a rate.

I sampled 5 PRs for the free-text pass, not the 20 to 30 the method asks for. I
stopped because PRs 61, 62, 64, 66 and 67 produced the same three categories and
no new ones, which is the saturation test, but 5 is a thin sample and a sixth PR
finding something new would not surprise me.

## The taxonomy

Frequency-ordered. "Written" names the pass 1 rule id, or says the category is
new.

### 1. Doc comments on unexported identifiers, 164 occurrences

Written: `cmt-doc-exported-only`. Rung 3.

Every unexported function this repo has ever gained carries a doc comment. In PR
66 alone: `applyDraftGate`, `readyOne`, `verdictDepsFor`, `draftReasonFor`,
`unresolvedGateReason`, `closedGateReason`. In PR 67: `spawnRun`,
`reRunDiffPreamble`, `unifiedDiff`, `pruneRunLogs`. In PR 64:
`applyReCheckIntents`, `reCheckOne`, `runIDFromDetailsURL`.

The blame spread is the important part. All 164 do not come from one bad early
phase. They come from every PR, including the newest: 11 from "base moved and the
automatic refresh", 8 from "the draft gate", 5 from "refresh conflicted and
abort". This is not debt, it is the current behaviour, repeated on every turn.

Worth saying plainly: many of these comments are good. `unifiedDiff`'s names a
real outside constraint, that GNU and BSD `diff` both accept `-u` and `--label`,
which is exactly what the standard permits. The rule the agent is breaking is the
narrow one about which declarations earn a comment at all, not the one about
comment quality.

### 2. Functions over the argument limit, 24 occurrences

Written: `go-max-args`, which says three to four arguments and then a struct or
functional options. Rung 2, via `revive` `argument-limit`.

Worst offenders: `derive` in `internal/cc/server.go` takes **10 parameters**,
`reRunOne` in `internal/cc/verbs.go` takes 8, `spawnRun` in `internal/cc/loop.go`
takes 7.

The mechanism matters more than the count, and this is the most interesting thing
in the whole pass. I watched it happen inside one PR. PR 67 needed to pass an old
prompt path down two call chains, so it took `spawnRun` from six parameters to
seven, `reRunOne` from seven to eight, and passed a bare `""` at the one callsite
that had nothing to send. PR 66 introduced `verdictDepsFor` with 5 parameters and `draftReasonFor`
with 5, both new. Every one of those diffs, read on its own, adds a single
parameter to a function that already had too many. No reviewer looking at one PR
sees a violation. The rule is only breakable cumulatively, which is precisely why
advice cannot hold it and a linter can.

### 3. Unwrapped errors from outside the package, 25 occurrences

Written: `pat-wrap-errors`. Rung 2, via `wrapcheck`.

`return tx.Commit()` and friends. The written rule says each layer adds the
context it knows about with `%w`. In practice the agent wraps diligently when it
writes a new `fmt.Errorf` and forgets entirely when it returns another package's
error straight through.

### 4. Subprocess spawn with no cancellation, 29 occurrences

**New.** Nothing in the 107 rules covers it. Rung 2, via `noctx`, scoped to
non-test code.

`exec.Command` where `exec.CommandContext` was available. This one deserves more
weight than its position suggests, because this program is a daemon whose entire
job is spawning agents, `git` and `gh`, and it already has a `ctx` threaded to
nearly every one of those callsites. A subprocess started without the context
cannot be cancelled when the tick is cancelled. PR 67's `unifiedDiff` uses
`exec.CommandContext` correctly, so the newer code knows better, which makes this
a leak rather than a blind spot.

The closest written rule is the concurrency reference's "derive at the edges,
propagate everywhere else". It talks about goroutines and never mentions
subprocesses. That gap is why 29 of these exist.

### 5. `time.Sleep` where a condition belongs, 8 occurrences

Written: `test-no-sleep`, which the testing reference puts in its Don't list in as
many words. Rung 2, via `forbidigo`.

Six in tests (`e2e/agents_test.go`, `internal/cc/cancel_test.go`,
`internal/cc/runner_test.go`, `internal/cc/runner_unix_test.go`) and one in
production (`internal/cc/runner_unix.go:128`). These are the tests the pass 1
handoff's own history calls out for racing their own cleanup, so the cost has
already been paid once.

### 6. Package-level mutable state, 9 occurrences

Written: `go-no-package-state`. Rung 2, via `gochecknoglobals`. No `init()`
functions anywhere, so half of that rule is already clean.

### 7. Test helpers and parallelism, 12 occurrences

Written: `test-t-helper` (7 via `thelper`) and `test-t-parallel` (5 via
`tparallel`). Rung 2.

### 8. Table-driven tests are not the default, structural

Written: `go-table-driven-default`. Rung 6, and it should stay there.

217 test functions, 36 `t.Run` calls. Roughly one test function in six uses a
subtest. The written rule says table-driven is "the default for any new
function"; the code says it is the exception. No linter can fix this and I would
not try. It is worth recording because it is the largest gap between what a skill
claims and what the codebase does, and the honest options are to enforce a ratio
in CI or to soften the rule to match reality. I would soften the rule. A
table-driven test with one row is worse than a plain one.

### 9. Small performance and idiom misses, 4 occurrences

`prealloc` (3) and `intrange` (1). Written as `pat-preallocate` and
`go-range-not-index`. Real, cheap, and near the bottom for a reason.

### 10. Derivation duplicated across the render and reconcile paths

**New.** Rung 6, or rung 4 at a stretch. One clear instance, so treat this as a
hypothesis rather than a finding.

PR 66 extracted `verdictDepsFor` because the page's render and the loop's
verdict-transition step were computing the same verdict separately. In the same
PR it added `draftReasonFor`, which calls `plan.DraftGate` a second time to
re-derive a decision `applyDraftGate` has already made on the same tick. The
agent removed one duplication and created another in the same diff.

`go-extract-on-third` is the nearest written rule and it is about packages, not
about this. What is actually going on is that the repo has two paths over the
same facts, one that renders and one that acts, and the agent keeps deriving
independently in each. If a second instance shows up, this becomes the most
valuable rule in the backlog, because nothing mechanical will ever catch it.

### 11. Doc comments citing something a reader cannot open

**New,** and minor. One instance: `spawnRun`'s comment ends "(see the PR
description)". Every other reference in the repo points at a design doc section,
which is the convention and works. `cmt-link-constraint` asks for a link when one
exists; a merged PR's description is not one.

## The rate

Violations surviving to HEAD, per 100 added Go lines, by the commit that
introduced them:

| Commit | Violations | +Go lines | Per 100 |
| --- | --- | --- | --- |
| Phase 1b e2e harness | 12 | 328 | 3.7 |
| e2e fakes and fake agent scripts | 12 | 700 | 1.7 |
| Phase 3 cut, spawn, liveness | 33 | 2993 | 1.1 |
| Phase 1a spine | 19 | 2189 | 0.9 |
| seams into prompt (#62) | 5 | 557 | 0.9 |
| the draft gate (#66) | 3 | 828 | 0.4 |
| re-check verb (#64) | 1 | 329 | 0.3 |
| Phase 5 verdict engine | 3 | 1149 | 0.3 |

The rate is flat, between 0.3 and 1.1 per 100 lines for everything except the two
smallest early PRs. It does not decay as the codebase fills in and the agent has
more local convention to copy. Given that early violations have had more chances
to be fixed by later PRs, the true rate for the early work was higher than this
table shows, so if anything the flat line is generous to the recent PRs. Two days
of advice-only enforcement produced a constant leak.

## What this changes about the backlog

Pass 1 ranked the nine zero-violation linters first, on the grounds that they
ship with no cleanup. That is still the right first move, but pass 2 says none of
those nine is where the damage is. All nine are clean precisely because the agent
was never inclined to break them.

The evidence-ordered backlog:

1. **`revive argument-limit`, 24 violations.** Highest value in the list. The
   only failure here that compounds, the only one no per-PR review can see, and
   the fix is one config block. Ship it advisory, fix the 24, then block.
2. **`cmt-doc-exported-only`, 164 violations.** Biggest by volume and the reason
   to settle ast-grep versus Semgrep now. Needs a codemod, not a cleanup sprint.
3. **`noctx` scoped to non-test code, 29 violations.** Correctness, not style. A
   daemon that cannot cancel its own subprocesses.
4. **`wrapcheck`, 25 violations.** One cleanup pass, then permanent.
5. **`forbidigo` on `time.Sleep`, 8 violations.** Cheap, and this repo has
   already been bitten by test races.
6. The rest: `gochecknoglobals` (9), `thelper` (7), `tparallel` (5), `prealloc`
   (3), `intrange` (1).
7. The nine zero-violation linters, as a group. Free to enable, and they stop
   the next class of mistake rather than an existing one.

## Two findings about the enforcers themselves

**`noctx` as I configured it has a 45% effective false positive rate.** Of 56
hits, 19 are tests calling their own `httptest` server and 6 are
`httptest.NewRequest`, none of which any written rule covers and none of which
anyone would want to fix. 29 `exec.Command` and 2 `database/sql` calls are real.
Google's number for humans is under 10% before engineers stop trusting a check,
and the lit review argues agent tolerance is lower than human tolerance, not
higher, because a compliant agent acts on a false positive instead of ignoring
it. So `noctx` must be scoped to non-test paths before it goes anywhere near a
hook. This is the first real datapoint in this repo for the tolerance question the
literature leaves open.

Related: pass 1's http-client rows said there is no `net/http` client code in the
tree. That still holds. Every `http.Get` and `http.NewRequest` hit is a test
against its own test server, so the seven `http-client.md` rules remain untested
by any real code.

**One of the eight enabled linters is switched off by its own setting.** The repo
sets `exhaustive.default-signifies-exhaustive: true`, and there are 30 `default:`
labels against 37 `switch` statements in non-test `internal/`. So `exhaustive`
exempts most of what it looks at. `plan.DraftGate` in PR 66 is a clean example:
its switch over PR state has a `default: // Open or Absent`, which makes the
check pass without ever verifying the enum is covered. Nobody wrote that setting
down as a standard either, so it appears in neither pass. Flipping it to `false`
is a one-line change and I have not measured what it would cost, which is the
next thing worth measuring.

## What is still not answered

The categories a linter cannot express are the ones I have the least evidence
for, and they are the ones that matter most: the duplicated derivation in
category 10 rests on a single instance. Getting real evidence there needs the
free-text pass extended to 20 PRs rather than 5, and it needs reading the diffs
against the design doc rather than against the linters. That is the pass where
`code-review`'s spec axis would actually earn its place, and it is a different
session from this one.
