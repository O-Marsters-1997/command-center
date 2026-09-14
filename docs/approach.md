# Approach

_Last updated: 2026-09-14_

## Background research

### The use case

You are building a local control plane that walks a DAG of tickets through agents to reviewable
pull requests, one worktree per ticket. Roughly 10,000 lines of non-test Go across eight packages,
with Postgres behind sqlc, an htmx board served from Go templates, and agent processes spawned as
subprocesses and watched. The question this synthesis answers is what "idiomatic, reusable,
scalable, performant" means for a program of exactly that shape, drawn from fourteen primary
sources rather than from blog aggregators.

### The mental model

Seven ideas that the sources assume you already hold. Everything else follows from these.

1. **A package name is a pitch, not a category.** Cheney's framing is that a package name should
   describe the service the package offers. `net/http` has no `client/` and `server/`
   subdirectories, just `client.go` and `server.go`. This matters here because the control plane's
   real boundaries are tracker, plan, push and store, and those are already the package names.

2. **Interfaces belong to the consumer and stay small.** Both the Google style guide and the code
   review comments say to define an interface in the package that uses it, only once real usage
   exists. The proverb is blunter: the bigger the interface, the weaker the abstraction. Producing
   an interface for a type with one implementation is the most common over-engineering in Go.

3. **Errors are values, and wrapping is a promise.** Rob Pike's argument is that error handling
   repetition is a design smell you can program your way out of. The Go 1.13 post adds the part
   people miss: wrapping an error with `%w` makes that error part of your API, because callers can
   now match on it with `errors.Is`, and you have to keep returning it.

4. **Context is a parameter, never a field.** Storing a context in a struct merges two lifetimes
   and takes per-call deadlines away from the caller. The only sanctioned exception is retrofitting
   an existing API, which is why `http.Request` carries one.

5. **Goroutine lifetime is a design obligation.** Cheney states it as a rule: never start a
   goroutine without knowing when it will stop. The pipelines article gives the mechanics. Senders
   close channels, receivers drain until closed, and every stage either has buffer for everything
   it will send or a cancellation signal. Goroutines are not garbage collected.

6. **The zero value should work.** `sync.Mutex`, `bytes.Buffer` and `http.Server` are all usable
   the moment they are declared. Designing for this removes a whole class of "forgot to call
   Init" bugs and makes struct literals in tests short.

7. **There are three optimisations: do less, do it less often, do it faster.** Cheney's line, and
   the rest of the performance workshop is about how most engineers spend all their time on the
   third and get the smallest return. Measure before you touch anything.

### Where the sources agree

The settled ground is wider than the arguments, and most of it is enforceable by tooling.

Formatting is not a matter of taste. `gofmt`, `goimports`, `go vet` and a linter suite run before
human review, and no style document should restate a rule a linter already enforces.

Naming length scales with scope. A loop index is `i`, a package-level variable is spelled out. Do
not put the type in the name (`userCount`, not `usersInt`). Do not repeat the package name in its
own exports (`widget.New`, not `widget.NewWidget`). Initialisms keep consistent case, so `URL` or
`url` but never `Url`. Getters drop the `Get`.

Never name a package `util`, `helper` or `common`. Google's guide forbids it outright and Cheney
calls it a signal of a design problem. The fix is either to move the function next to its only
caller or to find the name that describes what it does.

Accept interfaces, return concrete types. Add the interface when a second implementation actually
exists, not in anticipation of one.

Errors are returned, not signalled in band. No returning -1 or "" to mean failure. Error strings
are lowercase without trailing punctuation, and the message reads "x: y", not "failed to x: failed
to y". Check the error first and return early so the happy path stays unindented. Never discard an
error with `_` without a comment saying why it is safe. Handle each error exactly once, so either
log it or return it, never both.

Context is the first parameter. Not in an options struct, not a custom type.

Prefer synchronous functions and leave concurrency to the caller. Google's reasoning is the
asymmetry: a caller can trivially add concurrency around a synchronous function, but cannot remove
concurrency a library imposed on it.

Tests are table-driven with named struct fields, helpers call `t.Helper()`, and there is no
assertion framework. Validation code returns errors and the test decides whether to fail, which
keeps shared test logic usable outside the test binary. Use `t.Error` inside table loops and
reserve `t.Fatal` for setup that makes the rest meaningless.

Profile before optimising, and use `benchstat` to decide whether a difference is real.

### Where the sources disagree

These are the calls you actually have to make.

**`%w` or `%v`.** Uber and most of the community lean on `%w` almost everywhere. Google is
narrower: use `%w` inside a subsystem where callers may legitimately match on the cause, and `%v`
at a system boundary where you want to hide what is underneath. The Go 1.13 post supports Google,
because once you wrap `sql.ErrNoRows` and a caller matches on it, migrating off that database
breaks them. There is no universally right answer, but "always `%w`" is a decision you have made by
accident rather than on purpose.

**Package granularity.** Cheney argues for fewer, larger packages and compares a Go package to a
Maven module, on the grounds that deeper hierarchies produce wider, shallower APIs. Google argues
for splitting by concept and keeping files small enough to navigate. Both are right about
different failure modes. The tiebreaker is whether clients need to import two packages to do
anything useful, in which case those two packages are one.

**Embedding.** Effective Go presents embedding as the composition mechanism and shows it promoting
methods for free. Uber bans it in public structs, because an embedded type's new methods appear on
your type without you choosing them, which is a breaking change you did not write. Uber's position
is about libraries with external consumers. Inside `internal/` the cost is much lower.

**Test shape.** The table-driven default is universal. Mat Ryer argues against it at the service
level and prefers calling `run()` to spin the whole program up against real transports, on the
grounds that layer-by-layer unit tests miss integration bugs and duplicate effort. Google lands in
the same place from a different angle: test against real transports connected to fakes, not
hand-written mocks of clients.

**Functional options or an options struct.** Google's tiebreaker is usage. If most callers pass
options, use a struct. If most pass none and the option list is long, use variadic functions. The
variadic form costs more boilerplate and buys backwards compatibility.

**Channel buffering.** Uber says channels should be unbuffered or size one, and anything larger
needs justification. The pipelines article buffers deliberately as one of two ways to guarantee
senders never block. Both agree that the buffer size must be a reasoned number.

### Patterns worth lifting for this program

**Structure `main` as `run(ctx, args, getenv, stdin, stdout, stderr) error`.** Ryer's pattern.
`main` becomes four lines, and a test can start the whole program with controlled environment and
cancel it through the context. This repo already does most of this through `App.New` and `App.Run`.

**Keep the HTTP API visible in one file.** A `routes.go` whose only job is to list every route
means you can read the service's whole surface without grepping. Handlers are functions returning
`http.Handler`, which gives each one a place to do setup once, outside the request path.

**Shut down on a context you do not cancel.** Derive the shutdown context with
`context.WithoutCancel` plus a timeout, so cancelling the parent does not immediately kill the
drain you just started. Set `BaseContext` on the server so long-lived streaming handlers end when
the program does rather than holding `Shutdown` open for the full grace period.

**Use `errgroup` for any fan-out that can fail.** `errgroup.WithContext` cancels the shared context
on the first error, `Wait` returns it, and `SetLimit` bounds parallelism without a hand-rolled
semaphore. Each goroutine still has to check `ctx.Done()` or the cancellation buys nothing.

**Use `testing/synctest` for anything timing-dependent.** Stable since Go 1.25. A bubble runs
goroutines against a fake clock that advances automatically when every goroutine is durably
blocked, so a test of a five-second poll finishes instantly and deterministically. This replaces
the sleep-and-hope pattern outright.

**Log with `log/slog`.** Stdlib since 1.21. `LogAttrs` with typed `Attr` values avoids the
allocation that the loose key-value form causes, which matters only on hot paths but costs nothing
to adopt from the start.

**Commit a `default.pgo`.** Profile-guided optimisation is 2 to 7 percent of CPU for the cost of
dropping a profile in the main package directory. Refresh it occasionally as the code moves.

### What to watch out for

`fmt.Errorf("x: %w", err)` with a nil `err` returns a non-nil error reading `%!w(<nil>)`. Any
shutdown path that normalises an expected error to nil and then formats it unconditionally will
report failure on every clean exit.

Wrapping is a one-way door. Once a caller matches on what you wrapped, you own that error.

Benchmarks lie when the compiler deletes work that has no observable effect. Assign the result to a
package-level variable, call `b.ResetTimer()` after setup, and `b.ReportAllocs()`.

Only one profile type at a time. CPU and memory profiling running together observe each other.

`errgroup.Group` is single-use. Do not pool or reuse one.

A nil slice and an empty slice are the same for every practical purpose. Test with `len(s) == 0`
and never make an API where the difference is meaningful.

### Gaps in this reading list

Nothing here covers connection pool sizing, transaction retry, or `pgx` specifics, all of which
matter once the store is under concurrent load. Nothing covers supervising subprocesses, which is
the riskiest part of this program and has no canonical Go source. There is no material on
`testscript`, which this repo relies on for end-to-end coverage, or on `sqlc`, which generates the
database layer. The performance sources predate the current garbage collector tuning knobs
(`GOMEMLIMIT` in particular), and none of them address observability beyond structured logging,
so tracing and metrics are unaddressed. Generics get one sentence from Google ("start without
them") and nothing from anyone else.

<!-- Alignment content (Problem, Goals, Features, etc.) to be added via chat-to-approach -->

## Conformance audit

Measured against the research above on 2026-09-14. Covers `internal/` and the `go-idiomatic`
skill at `~/Documents/personal/ai-development/my-claude-code/skills/code/go-idiomatic`.

### internal/ — what already holds

The import graph is the strongest thing here. Seven leaf packages (`agentlog`, `ccdb`, `gh`,
`plan`, `tp`, `tracker`, `verdict`) import nothing from each other, and `internal/cc` composes all
of them. No cycles, no tangle, and every leaf is testable without the control plane.

Three interfaces in 10,000 lines of non-test code, and each is defined where it is consumed.
`cc.Runner` exists because the loop's tests substitute a fake process runner. `tracker.Source`
exists because `For()` dispatches on host. `ccdb.DBTX` is sqlc output. That is the discipline the
sources ask for and most Go codebases fail.

No `panic`, no `log.Fatal`, no `os.Exit` anywhere under `internal/`. No context stored in a struct,
and no function taking a context anywhere but first. Package-level `var` blocks are `//go:embed`
handles, parsed templates and immutable lookup tables, not mutable shared state.

`App.Run` gets graceful shutdown right in a way most Go services do not: `BaseContext` so streaming
handlers die with the program, `context.WithoutCancel` plus a five-second budget for the drain, and
`errors.Join` across the server and the loop.

`gofmt` and `go vet` are clean. 87 test files, 73 using `t.Parallel`, 109 `t.Helper` calls, 59
table-driven loops, golden files behind `-update`, and `testscript` for end-to-end coverage.

### internal/ — findings, worst first

**1. `App.Run` returns a non-nil error on every clean shutdown.** Fixed 2026-09-14, with a
regression test at `internal/cc/app_test.go:TestRunReturnsNilOnACleanShutdown`.
`internal/cc/app.go:180` set
`err = nil` when the server closed normally and then formats it unconditionally:

```go
if errors.Is(err, http.ErrServerClosed) {
    err = nil
}
errs <- fmt.Errorf("serve %s: %w", srv.Addr, err)
```

`fmt.Errorf` with a nil `%w` operand returns a non-nil error reading
`serve 127.0.0.1:8080: %!w(<nil>)`. Verified by running it. That value reaches `errors.Join` and
then `cmd/cc/main.go:49`, so a normal Ctrl-C exits non-zero with a garbled message. The fix is to
send nil directly when there is nothing to report.

**2. The codebase has no error values of its own.** `errors.New` appears zero times under
`internal/`, and all eighteen `errors.Is` / `errors.As` calls target stdlib errors
(`os.ErrNotExist`, `sql.ErrNoRows`, `io.EOF`, `syscall.ESRCH`, `exec.ExitError`). Nothing in the
program can distinguish "no such ticket" from "database unreachable" without matching on strings,
which Google's guide forbids explicitly. The 134 `%w` wraps preserve a chain that has nothing
worth matching at the top of it.

**3. Wrapping is a habit rather than a decision.** 134 of 149 `fmt.Errorf` calls use `%w`. Google's
rule is `%w` inside a subsystem and `%v` at the boundary, precisely so that pgx and sqlc errors do
not become part of the store's permanent contract. Worth an explicit policy either way, because
right now the policy is implicit.

**4. Five `time.Sleep` calls in tests. Not a finding, on inspection.** An earlier draft of this
audit proposed replacing them with `testing/synctest`. That was wrong. All five poll real operating
system state (`kill(-pgid, 0)`, `ps`, a file appearing), and `synctest`'s fake clock only advances
when every goroutine inside its bubble is durably blocked. A real subprocess is outside the bubble,
so the clock would never move. The six-second sleep at `internal/cc/runner_unix_test.go:72` tests
pid-reuse tolerance, which inherently needs two processes started more than five seconds apart.
Polling with a deadline is the correct shape for all five.

**5. No benchmarks, no fuzz targets.** Defensible for a program that polls every five seconds, but
it means there is no baseline to regress against. `internal/agentlog/parse.go` parses JSONL written
by agent processes, which is untrusted input with a parser in front of it, and that is the shape
fuzzing exists for.

**6. `log.Printf` rather than `log/slog`.** Only three calls, so this is cheap to change, and the
first time you want to filter a run by ticket you will want structured output.

**7. `internal/cc/server.go` is 1,106 lines.** The package being large is a deliberate choice and
defensible on Cheney's argument. The file is the problem: routing, handlers, template wiring and
origin checks in one place. Splitting the route table into `routes.go` costs nothing structurally
and makes the HTTP surface readable at a glance.

**8. `.golangci.yml` has no `errorlint`.** Added 2026-09-14. It reports zero issues today, which
is the useful result: the 134 `%w` wraps are internally consistent and nothing compares errors with
`==`. It is there to keep finding 3 from drifting. Note that `.golangci.yml` is deny-listed for
agent pushes, so this change has to be committed by a human.

`nilerr` and `bodyclose` were also trialled and both were rejected. `nilerr` flags
`internal/cc/runner_unix.go:126`, where a failing `kill(-pgid, 0)` means the process group is
already gone and nil is the correct answer, as the comment above it explains. `bodyclose` flags all
three `postLaunchForm` call sites in `server_test.go`, which do close the body, via
`t.Cleanup` at line 1013. Adopting either would mean annotating correct code with `//nolint`.

### go-idiomatic skill — what already holds

Accept interfaces and return concrete types, define interfaces in the consumer, keep them small,
make the zero value useful, return early, drop the dead `else`, prefer `switch` to `if`/`else if`
chains. Extract on the third repetition rather than the second, which is the right reading of "a
little copying is better than a little dependency". `internal/` over `pkg/`, which matches the
official module layout guidance. Lint before review rather than restating what a linter enforces.
Delegating comment policy to `~/.claude/rules/comments.md` instead of restating it.

`references/testing.md` is stronger than any single source in the reading list, covering tables,
subtests, parallelism, golden files, mocks, benchmarks, fuzzing and HTTP handlers in one place.
`references/concurrency.md` covers errgroup, bounded pools, leak avoidance and graceful shutdown.

### go-idiomatic skill — findings, worst first

**1. The `utils` carve-out contradicts every source.** SKILL.md says "`utils` is a legitimate
package name" given cross-cutting use and no internal imports. Google's guide says never name a
package `util`, `helper` or `common`. Cheney says utility packages indicate a design problem. The
conditions the skill attaches actually describe `strings` and `slices`, which are named for what
they do. This is the one place the skill teaches something the community has explicitly rejected.

**2. `Option[T]` is a house preference presented as idiom.** A 172-line reference mandates a
generic `Option[T]` for optionality and bans `*T` for the same purpose. None of the fourteen
sources mention it. Go's answers are the comma-ok idiom, a zero value that means absent, or a
pointer where nil genuinely means unset. The command-centre repo uses `Option[T]` zero times, which
is the honest verdict on how load-bearing it is. Keep it if you like the pattern, but label it as
a preference rather than as idiomatic Go.

**3. Nothing on context.** No rule that `ctx` is the first parameter, never a struct field, never
inside an options struct, and no custom context types. That appears in Google's decisions document,
the code review comments, and a dedicated Go blog post. It is among the most frequently broken
rules in Go and the skill does not mention it once.

**4. Nothing on error message style or the API cost of wrapping.** Missing: lowercase error strings
with no trailing punctuation, "x: y" rather than "failed to x: failed to y", and the important one,
that `%w` makes the wrapped error part of your API forever. `references/patterns.md` covers
sentinels and `errors.Is` but never says when to reach for `%v` instead.

**5. `references/testing.md` predates `synctest`.** Stable since Go 1.25 and the single highest
-value addition available, especially given the five sleeps in this repo's own tests.

**6. Performance guidance starts at the wrong end.** `references/patterns.md` has preallocation,
`sync.Pool` and `strings.Builder`, which are all real but are the third of Cheney's three
optimisations. Missing: profile before touching anything, `benchstat` for statistical significance,
the fact that benchmarks lie when the compiler eliminates unobserved work, one profile type at a
time, and PGO for a few percent at almost no cost.

**7. No synchronous-by-default rule.** Google and Cheney both state it, and the reasoning is
asymmetry: a caller can add concurrency around a synchronous function but cannot remove concurrency
a library imposed. `references/concurrency.md` opens with when to reach for concurrency, which
implies the default without stating it.

**8. "Declare complex arguments before the call" is idiosyncratic.** Harmless, absent from every
source, and it treats the symptom of a problem Cheney names directly: a function taking several
parameters of the same type is the defect, and the signature is what should change.
