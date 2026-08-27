# Standards enforcement inventory

_Pass 1, 2026-08-24. Method and vocabulary from `docs/lit-review.md`._

Every coding standard this repo claims, against what actually catches a violation.
The question is narrow: for each written rule, does anything fail when an agent
breaks it, or does compliance depend on the model choosing to comply?

I read all five rule sources in full rather than sampling them, then probed the
configured linters with a file of deliberate violations to check what they really
catch. Everything in the "enforced today" column below is a measured result, not
a reading of the linter's documentation. Every violation count reproduces with
`golangci-lint run --config <throwaway.yml> ./...` enabling the named linter and
nothing else.

## The ladder

Six rungs, from `docs/lit-review.md`. For each rule, take the highest rung that
can express it.

1. The violation is unrepresentable in the type or schema. No rule to maintain.
2. An existing formatter, linter or type checker already knows the rule, surfaced
   through a `PostToolUse` hook so the agent sees it on the same turn.
3. A structural rule in ast-grep or Semgrep, for conventions no off-the-shelf
   linter knows.
4. A CI check for properties that only exist across files: dependency direction,
   coverage floors, suppression counts.
5. `PreToolUse` deny, for the things that must never happen.
6. Model judgement.

## What enforces anything today

Eight linters and five CI jobs. What each one actually catches, verified:

| Enforcer | Catches | Verified behaviour |
| --- | --- | --- |
| `errcheck` | Error returns from a bare call | Does **not** flag `_ = f()`. `check-blank` defaults to false |
| `govet` | vet's suite, including `lostcancel` | Flagged a discarded `cancel`, not an assigned-but-uncalled one |
| `ineffassign` | Assignments never read | |
| `staticcheck` | SA, S, QF checks | Caught `QF1003` (if/else-if chain to tagged switch) and `S1000`. The ST doc-comment and naming checks are off in golangci-lint's default check set |
| `unused` | Dead functions, unused struct fields | The only linter that fired on most of my probe file |
| `misspell` | Dictionary typos | Caught 1 of 4 planted misspellings. Narrower than it looks |
| `exhaustive` | Missing enum cases in a switch | `default` counts as exhaustive |
| `lll` | Lines over 120 characters | |
| `goimports` | Formatting and import grouping | `local-prefixes` set to the module path |
| CI `test` | `go test -race`, ubuntu and macos | |
| CI `e2e` | `go test -tags=e2e ./e2e/...` | |
| CI `lint` | golangci-lint, pinned v2.11.3 | |
| CI `build` | `go build ./cmd/cc` | |
| CI `tidy` | `go.mod` and `go.sum` drift | |
| `just check-conflicts` | Merge conflicts against `origin/main` | |

There are no hooks in this repo. `.claude/settings.local.json` holds a
`permissions` key and nothing else. No `PreToolUse`, no `PostToolUse`, no `Stop`.
The user's global `~/.claude/settings.json` does have hooks, and one of them,
`check-staged-comments.sh`, enforces part of the comments standard on every
`git commit` in every repo. `docs/enforcement-proposal.md` measures how well it
holds, which is the reason to read that document before acting on this one.
There is no `CLAUDE.md` and no `AGENTS.md` at the root either, so the only
repo-level configuration an agent reads is the skills in `.agents/skills/`.

## The table

Ladder column is the highest rung that could express the rule, not where it sits
now. Unless the enforced column says otherwise, the rule sits at rung 6 today.
Violation counts in the gap column are measured against the current tree.

### `~/config/dotfiles/claude/rules/comments.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `cmt-external-thing` | Write a comment only if it names a specific thing a reader would get wrong, and that thing lives outside this repo's control | all comments | nothing | 6 | Stays at 6. "Outside this repo's control" is not decidable by a matcher |
| `cmt-permitted-list` | Only licence headers, load-bearing directives, external constraints and doc comments on exported identifiers are permitted | all comments | nothing | 3 | Structural rule: comment block above a declaration with no `//go:` prefix, no URL, no exported name. Noisy until `cmt-doc-exported-only` lands |
| `cmt-link-constraint` | Link the issue or RFC when one exists | constraint comments | nothing | 6 | Needs to know whether a link exists |
| `cmt-no-confessions` | No `IMPORTANT`, `do not remove`, `careful`, `this is subtle` | all comments | nothing | 2 | A grep in a `PostToolUse` hook. **0 violations today**, ships blocking with no cleanup |
| `cmt-suppression-reason` | A `//nolint` survives only if the comment says the silenced rule is faulty, pedantic or stylistic | suppressions | nothing | 2 | `nolintlint` with `require-explanation`. **0 violations** |
| `cmt-suppression-defect` | A suppression hiding a real defect is deleted and the defect reported | suppressions | nothing | 4 | CI check counting `//nolint`, `_ =` and config exclusions against a threshold that can only go down. **0 nolint, 8 `_ =` today** |
| `cmt-doc-exported-only` | No doc comments on unexported identifiers. `internal/` and `_test.go` are not public API | all declarations | nothing | 3 | Structural rule on comment-block-above-unexported-declaration. **164 violations**, so advisory only |
| `cmt-doc-three-lines` | Doc comments capped at three lines | doc comments | `lll` covers per-line width, not block length | 3 | Same rule, count lines in the block. **49 violations** |
| `cmt-doc-line-one` | Line one says what the declaration is; further lines carry an outside constraint or go | doc comments | nothing | 6 | |
| `cmt-no-signature-restate` | A doc comment that only restates the signature is noise | doc comments | nothing | 3 | Partial. A matcher gets `// Close closes the …` and little else |
| `cmt-no-density-match` | Do not match the comment density of the file being edited | editing behaviour | nothing | 4 | Comments-per-line delta on changed files against the repo median |

### `.agents/skills/go-idiomatic/SKILL.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `go-lint-first` | Run gofmt, goimports, go vet and golangci-lint before touching code | workflow | CI `lint` job only, minutes after the turn | 2 | `PostToolUse` hook running `golangci-lint run` on the edited package |
| `go-no-linter-duplication` | Do not write a standard for anything the linter already enforces | skill authoring | nothing | out of scope | This document is the audit of it |
| `go-cmd-entrypoint-only` | `cmd/<app>/main.go` wires dependencies and holds no business logic | `cmd/` | nothing | 4 | Import-direction check on `cmd/` |
| `go-internal-not-pkg` | Application code lives in `internal/`; no `pkg/` layer | layout | nothing | 4 | One line in CI: `pkg/` must not exist. Vacuously true today |
| `go-testdata-dir` | Fixtures and golden files live in `testdata/` | tests | nothing | 4 | |
| `go-extract-on-third` | Extract a shared package on the third repetition, not the second | refactors | nothing | 6 | |
| `go-local-helpers` | A helper coupled to one domain lives next to its primary caller | layout | nothing | 6 | |
| `go-package-named-by-doing` | Name packages by what they do, not what they are | layout | nothing | 6 | |
| `go-utils-no-internal-deps` | A `utils` package may not import anything else from `internal/` | layout | nothing | 4 | `depguard`, or an import-graph assertion. No `utils` package exists today |
| `go-no-helpers-common` | No package named `helpers` or `common` | layout | nothing | 3 | `depguard` deny-list. Clean today |
| `go-no-package-state` | No package-level state initialised in `init()`; pass dependencies through constructors | all Go | nothing | 2 | `gochecknoinits` (**0 violations**) plus `gochecknoglobals` (**7**) |
| `go-max-args` | Three to four arguments maximum; group into a struct or use functional options | functions | nothing | 2 | `revive` `argument-limit: 4`. **9 violations** |
| `go-functional-options` | Constructors that grow over time take `Option` funcs, not positional arguments | constructors | nothing | 6 | |
| `go-named-locals` | Extract multi-argument constructor calls into named locals before the call | callsites | nothing | 3 | Matcher on a call with three or more non-identifier arguments |
| `go-return-early` | Validate and guard at the top; keep the happy path unindented | functions | nothing | 2 | `revive` `early-return`. **0 violations** |
| `go-no-else-after-return` | Remove the `else` when the `if` branch returns | control flow | nothing | 2 | `revive` `indent-error-flow`. **0 violations** |
| `go-switch-over-elseif` | Prefer `switch` to an `if`/`else if` chain | control flow | **`staticcheck` QF1003** | 2 | Already enforced. Only fires when the chain compares one variable to constants |
| `go-range-not-index` | Use `range` unless the index is integral to the logic | loops | nothing | 2 | `intrange` covers the integer-count case only. **1 violation** |
| `go-pointer-for-mutation` | Pointers for mutation and measurably large structs; values otherwise | receivers, parameters | nothing | 6 | |
| `go-no-pointer-optionality` | Never use `*T` to signal "this might not be set" | struct fields | nothing | 3 | Matcher on `*string`, `*int`, `*bool` struct fields |
| `go-name-by-return` | Name functions by what they return or do | naming | nothing | 6 | |
| `go-bool-predicate-names` | Name booleans as predicates | naming | nothing | 3 | Matcher: `bool` field or variable with no is/has/can/should prefix |
| `go-no-odd-abbrev` | No abbreviations outside Go convention | naming | nothing | 6 | |
| `go-never-explain-what` | Never comment what the code already says | comments | nothing | 6 | See the comments rows above |
| `go-table-driven-default` | The default for any new function is a table-driven test | tests | nothing | 6 | |

### `.agents/skills/go-idiomatic/references/patterns.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `pat-clear-over-clever` | Clear beats clever | all Go | nothing | 6 | |
| `pat-useful-zero-value` | The zero value is usable without initialisation | types | nothing | 3 | Matcher on map-typed struct fields written without a constructor |
| `pat-accept-interfaces` | Accept interfaces, return concrete structs | signatures | nothing | 3 | `ireturn` covers the return half |
| `pat-wrap-errors` | Wrap errors with `%w` and the context this layer knows | errors | nothing | 2 | `wrapcheck`. **24 violations**, needs a real cleanup pass |
| `pat-sentinel-errors` | Sentinel `Err…` values and typed errors for structured cases | errors | nothing | 6 | |
| `pat-errors-is-as` | Compare with `errors.Is` and `errors.As`, never `==` | errors | nothing | 2 | `errorlint`. **0 violations** |
| `pat-never-ignore-errors` | Never discard an error. `_ =` only for documented best-effort cleanup | errors | **`errcheck`, partial** | 2 | `errcheck.check-blank: true`. **8 violations**, all deliberate. Today `_ =` silences the rule with no comment required |
| `pat-small-interfaces` | Keep interfaces small and compose them | interfaces | nothing | 6 | |
| `pat-interface-in-consumer` | Define the interface in the package that uses it | interfaces | nothing | 4 | |
| `pat-preallocate` | Preallocate slices when the size is known | performance | nothing | 2 | `prealloc`. **3 violations** |
| `pat-sync-pool` | Use `sync.Pool` for frequent allocations | performance | nothing | 6 | |
| `pat-no-concat-in-loop` | No `+=` string building in a loop | performance | nothing | 3 | |
| `pat-no-naked-return` | No naked returns in long functions | functions | nothing | 2 | `nakedret`. **0 violations** |
| `pat-no-panic-control-flow` | `panic` only for unrecoverable programmer error | all Go | nothing | 2 | `forbidigo` on `^panic$`. **3 violations** across panic and `time.Sleep` |
| `pat-no-ctx-in-struct` | Never store a `context.Context` on a struct | types | nothing | 2 | `containedctx`. **0 violations** |
| `pat-ctx-first-param` | `ctx context.Context` is the first parameter | signatures | nothing | 2 | `revive` `context-as-argument` |
| `pat-receiver-consistency` | One receiver style per type | methods | nothing | 2 | `recvcheck`. **0 violations** |

### `references/value-vs-pointer.md` and `references/optional.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `vp-large-is-measured` | "Large" means measured in a benchmark, not intuitively big | receivers | nothing | 6 | |
| `vp-value-from-constructor` | Return a value, not `*T`, from a constructor for a small immutable type | constructors | nothing | 6 | |
| `opt-for-optional-fields` | Use `Option[T]` for genuinely optional struct fields | types | nothing | 6 | |
| `opt-when-zero-meaningful` | Use `Option[T]` when `T`'s zero value is meaningful data | returns | nothing | 6 | |
| `opt-not-for-mutation` | Do not wrap mutation pointers, large structs or interfaces in `Option` | types | nothing | 3 | |
| `opt-not-reflexive` | Do not reach for `Option` when a natural zero value already means absent | types | nothing | 6 | |
| `opt-lives-at-internal-option` | The implementation lives at `internal/option/option.go` | layout | nothing | 4 | No such package exists. This rule has never been exercised |

### `references/http-client.md`

No `net/http` client code in the tree yet, so every row here is untested in practice.

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `http-no-client-for-one-endpoint` | Only build a full client when calls are shared or middleware is needed | http | nothing | 6 | |
| `http-functional-options` | Client constructors take `Option` funcs | http | nothing | 6 | |
| `http-auth-at-transport` | Attach auth in a `RoundTripper`, never per request | http | nothing | 3 | |
| `http-clone-before-mutate` | Clone the request before mutating it in `RoundTrip` | http | nothing | 3 | |
| `http-new-request-with-context` | Use `NewRequestWithContext`, not `NewRequest` plus `WithContext` | http | nothing | 2 | `noctx` |
| `http-no-default-client` | Never use `http.DefaultClient` | http | nothing | 3 | `forbidigo` on the identifier. Clean today |
| `http-pass-ctx` | Pass `ctx` to every outbound request | http | nothing | 2 | `noctx` |

### `references/testing.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `test-t-helper` | Mark helpers with `t.Helper()` | tests | nothing | 2 | `thelper`. **3 violations** |
| `test-t-parallel` | Use `t.Parallel()` for independent tests | tests | nothing | 2 | `tparallel`. **5 violations** |
| `test-t-cleanup` | Release resources with `t.Cleanup()` | tests | nothing | 3 | |
| `test-t-tempdir` | Use `t.TempDir()` for temporary files | tests | nothing | 2 | `usetesting` |
| `test-golden-in-testdata` | Golden files in `testdata/` behind an `-update` flag | tests | nothing | 4 | |
| `test-hand-written-mocks` | Prefer hand-written mocks for small interfaces | tests | nothing | 6 | |
| `test-descriptive-names` | Test names describe the scenario and the expected outcome | tests | nothing | 6 | |
| `test-behaviour-not-impl` | Test behaviour, not implementation | tests | nothing | 6 | |
| `test-no-private-direct` | Do not test private functions directly | tests | nothing | 3 | Matcher, but the false positive rate in same-package Go tests would be high |
| `test-no-sleep` | No `time.Sleep` in tests | tests | nothing | 2 | `forbidigo` on `^time\.Sleep$`, scoped to `_test.go` |
| `test-no-flaky` | Fix or delete flaky tests | tests | nothing | 4 | `go test -count=10` in a nightly job |
| `test-dont-mock-everything` | Prefer integration tests where setup cost is manageable | tests | nothing | 6 | |
| `test-error-paths` | Cover error paths | tests | nothing | 4 | Branch coverage floor |
| `test-coverage-floors` | 100% critical logic, 90% public API, 80% general, generated code excluded | tests | **nothing. Coverage is never measured, anywhere** | 4 | `go test -coverprofile` plus a threshold step. This is the one defence the literature names against agents deleting tests |
| `test-race-in-ci` | `-race` is non-negotiable | tests | **CI `test` job** | 4 | Already enforced |

### `references/concurrency.md`

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `conc-sequential-default` | Default to sequential code | all Go | nothing | 6 | |
| `conc-known-exit` | Every goroutine has a known exit | goroutines | nothing | 6 | |
| `conc-sender-closes` | The sender owns the channel and is its only closer | channels | nothing | 3 | |
| `conc-directional-params` | Declare channel direction at function boundaries | signatures | nothing | 3 | |
| `conc-unbuffered-default` | Default to unbuffered channels | channels | nothing | 3 | Matcher on `make(chan T, n)` with a literal `n` |
| `conc-select-ctx-done` | Every `select` in a goroutine includes `<-ctx.Done()` | goroutines | nothing (probed, not caught) | 3 | |
| `conc-no-mutex-across-io` | Never hold a mutex across I/O | locks | nothing | 6 | |
| `conc-no-rlock-upgrade` | Never upgrade an `RLock` to a `Lock` | locks | nothing | 3 | |
| `conc-typed-atomics` | Use typed atomics, not the `atomic.AddInt64` function API | atomics | nothing | 3 | `forbidigo` on the function-style names |
| `conc-syncmap-read-heavy` | `sync.Map` only for read-heavy, stable key sets | maps | nothing | 6 | |
| `conc-pool-reset` | `Reset()` a pooled object before `Put()` | `sync.Pool` | nothing | 3 | |
| `conc-wg-add-before-go` | Call `wg.Add` before the `go` statement | waitgroups | nothing | 3 | |
| `conc-errgroup-setlimit` | Bound concurrency with `errgroup.SetLimit(n)` | goroutines | nothing | 6 | |
| `conc-no-handrolled-pool` | Do not hand-roll a worker pool without a streaming reason | goroutines | nothing | 6 | |
| `conc-notifycontext` | Use `signal.NotifyContext` for graceful shutdown | main | nothing | 3 | |
| `conc-no-fresh-background` | No fresh `context.Background()` root mid-call-stack | context | nothing | 3 | |
| `conc-defer-cancel` | Always `defer cancel()` immediately after `WithTimeout` or `WithCancel` | context | **`govet` `lostcancel`, partial** | 2 | Probe confirmed it catches a discarded `cancel`, not an assigned one that is never called |
| `conc-no-timeafter-loop` | No `time.After` inside a loop | loops | nothing (probed, not caught) | 3 | |
| `conc-no-unbounded-spawn` | No unbounded goroutine spawning | goroutines | nothing | 6 | |
| `conc-goleak` | `goleak.VerifyNone` on any test that touches goroutines | tests | nothing | 4 | `goleak` is not a dependency today |
| `conc-no-pointer-over-channel` | Do not send a pointer over a channel | channels | nothing | 3 | |

### `~/.claude/CLAUDE.md` and `RTK.md`

Mostly out of a Go linter's reach, kept here rather than dropped.

| id | rule | scope | enforced today | rung | cheapest gap-closer |
| --- | --- | --- | --- | --- | --- |
| `glob-add-alias` | Add zsh aliases with `add-alias`, never by hand-editing `aliases/*.sh` | shell config | nothing | 5 | `PreToolUse` deny on writes to `~/.dotfiles/zsh/aliases/*.sh` |
| `glob-unslop` | Load `unslop` before writing prose for a human | prose | nothing | 6 | Rung 5 gets part of it: block a PR body write until the skill has loaded |
| `glob-rtk-proxy-push` | Push via `rtk proxy`, never `rtk git push` | git | a global RTK rewrite hook, outside this repo | 5 | `PreToolUse` deny on a `git push` without the `rtk proxy` prefix |
| `glob-rtk-meta-direct` | Call `rtk gain`, `discover` and `proxy` directly | tooling | same global hook | out of scope | |

## The count

- **107 rules** across the five sources.
- **2** are out of scope for mechanical enforcement (skill authoring, RTK meta
  commands).
- **35** genuinely need model judgement. Rung 6 is their ceiling and no rule
  language would help.
- **70** have a ceiling below rung 6: 25 at rung 2, 29 at rung 3, 14 at rung 4,
  2 at rung 5.
- Of those 70, two are fully enforced (`go-switch-over-elseif`,
  `test-race-in-ci`), three are partly enforced (`go-lint-first`,
  `pat-never-ignore-errors`, `conc-defer-cancel`), and one is enforced by a hook
  outside this repo (`glob-rtk-proxy-push`).
- **64 rules have no enforcer at all despite being expressible at rung 1 to 5.**
  Sixty-seven if you count the three partials as gaps, which you probably should.

That is the deliverable. Sixty-four of this repo's own written standards hold only
because the model chose to comply, and not one of them needs judgement to check.

## What ships clean today

Nine of the 64 map onto a linter that already ships inside the golangci-lint the
repo already runs, and shows zero violations against the current tree. These can
go blocking now, with no cleanup and no decision about ast-grep versus Semgrep:

| Rule | Linter | Violations |
| --- | --- | --- |
| `pat-receiver-consistency` | `recvcheck` | 0 |
| `pat-no-naked-return` | `nakedret` | 0 |
| `pat-no-ctx-in-struct` | `containedctx` | 0 |
| `pat-errors-is-as` | `errorlint` | 0 |
| `cmt-suppression-reason` | `nolintlint` | 0 |
| `go-no-package-state` (init half) | `gochecknoinits` | 0 |
| `go-no-else-after-return` | `revive` `indent-error-flow` | 0 |
| `go-return-early` | `revive` `early-return` | 0 |
| `cmt-no-confessions` | a grep, no linter needed | 0 |

Eight more are within one small cleanup pass, 39 fixes in total: `intrange` (1),
`prealloc` (3), `thelper` (3), `forbidigo` (3), `gochecknoglobals` (7),
`errcheck.check-blank` (8), `tparallel` (5), `revive argument-limit` (9).

Three need a real cleanup before they could ever block: `wrapcheck` (24),
`cmt-doc-three-lines` (49), `cmt-doc-exported-only` (164).

## Findings

**No hooks in this repo, so nothing repo-specific fires where the work happens.**
Every enforcer here runs in CI, minutes after the agent has moved on and lost the
context that would let it fix the problem cheaply. One global hook does fire on
every commit, `check-staged-comments.sh`, and pass 2 measured 49 violations of the
standard it guards landing after it went live, so its existence does not soften
this finding. The lit review's strongest consensus
point is that the check has to land on the same turn as the edit. Even the eight
linters that do exist are wired to the wrong moment for an agent audience.
Adding a `PostToolUse` hook that runs golangci-lint on the edited package changes
nothing about what is caught and everything about when.

**The most checkable rule in the set is the furthest from compliance.** The
comments standard is the one source written tightly enough to become a matcher on
day one. It is also 164 violations deep on its own exported-identifiers-only
clause and 49 deep on the three-line cap. Google's rule applies: nothing gets
enabled before the codebase is clean, or every agent turn burns tokens on
pre-existing noise. So rule one of the enforcement plan is advisory-only until
someone does the cleanup, and the cleanup is itself a large-scale change worth
doing with a codemod rather than by hand. My count is a heuristic (any comment
block sitting above a declaration), so treat 164 as the order of magnitude, not
the exact number.

**`errcheck` has a one-character bypass and the repo already uses it eight
times.** `check-blank` defaults to false, so `_ = f()` silences the
never-ignore-errors rule with no comment and no linter complaint. All eight
current uses look deliberate. That is not the point. The lit review flags
suppression as the trapdoor for deterministic enforcement against a compliant
optimiser, and this is the cheapest suppression available in the repo today. The
suppression-counting check should be written before the second real check exists,
while the count is still zero `//nolint` and eight `_ =`.

**Nothing measures coverage.** Not in CI, not in the Justfile, nowhere. The
testing reference states four coverage floors and the repo checks none of them.
CodeScene names test deletion as an agent failure mode that no structural check
catches, and a coverage floor is the only defence anyone in the literature names.
This is the largest single gap in the table.

**Three golangci-lint versions are in play.** `just lint` pulls
`golangci/golangci-lint:latest` from Docker, CI pins v2.11.3, and `just ci` calls
bare `golangci-lint run` against whatever is on PATH (2.13.1 on this machine). A
check can pass locally and fail in CI on a version difference alone. Flagging,
not fixing, per the handoff.

## The inverse gap

Seven things the repo enforces that nobody wrote down as a standard. An agent
learns each of these only by failing CI:

- 120-character line limit (`lll`)
- Spelling in comments and strings (`misspell`)
- Exhaustive enum switches, with `default` counting as exhaustive
- Import grouping with the module as local prefix (`goimports`)
- The `e2e` build tag must stay lint-clean
- `go.mod` and `go.sum` must not drift from `go mod tidy`
- A branch must not conflict with `origin/main` (`just check-conflicts`)

## Not this session

Pass 2 is the other half of the question. This document says what the repo
claims; it says nothing about what agents actually get wrong. Running
`code-review` over 20 to 30 recent agent-authored PRs and open-coding the failures
gives the frequency ordering that should decide which of the 64 gets closed first.
The nine clean-today rules do not need to wait for it. Everything else should.

The ast-grep versus Semgrep decision stays open. It should be settled on one
representative rule prototyped in both, and `cmt-doc-exported-only` is the
obvious candidate: it needs comment-to-declaration adjacency and an
exported-name test, which is enough structure to tell the two engines apart.
