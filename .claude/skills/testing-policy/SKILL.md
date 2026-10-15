---
name: testing-policy
description: >
  command-center's testing policy: which code gets which test, which doubles it uses, and what it
  asserts. TRIGGER — load BEFORE writing, editing or reviewing any *_test.go, web/src/*.test.ts,
  e2e/tests/*.txtar, testdata/ fixture or golden, fake, contract suite or fuzz target; or when the
  prompt says "write a test", "add tests", "cover this", "fix this test", "is this tested", or
  implements a feature or fix with TDD. Load it alongside go-testing, which owns how a test is
  written. SKIP for code changes that write and judge no test.
---

# Testing policy

This file says what command-center tests and with which of its own helpers. How a test is
written is `go-testing`, and where the two disagree `go-testing` wins. Where `go-testing` says
`pgtest` and `TEST_DATABASE_URL`, this repo uses `internal/cctest` and `CC_TEST_DATABASE_URL`;
don't install `pgtest`.

## Core rule

- Only runtime code is tested: code that ships in `cc` and runs. No tests of fixtures, config
  files, docs, generated code, source text or other tests.
- A test file's name says what it drives: either the source file's stem
  (`draftgate.go` → `draftgate_test.go`) or one exported surface a caller drives (a page or route
  in `web`: `session_page_test.go`; an intent or verb in `loop`: `steer_test.go`; an exported
  derivation in `plan`: `status_test.go`). A file that names neither breaks the policy.
- Fixtures and golden files exist only as inputs to such a test. One that no test reads is deleted.
- Test utilities (`cctest`, `gittest`, `htmltest`, `webtest`, `storetest`, request helpers) have
  no tests of their own; the tests that use them prove them. A port fake is the exception: it runs
  its port's contract suite (see Doubles).

## Before writing a test

1. Name the row of the map below that the test fills. If no row fits, stop and ask.
2. Check the never-tested list. If the code is on it, test it through its caller.
3. Check the case isn't already owned by a smaller row. A decision case belongs to `plan`; a page's
   content belongs to `web`. Larger rows prove the effect happens, not every case of it.

## The map

| Code | File | Size | Doubles | Asserts | Guards |
|---|---|---|---|---|---|
| `plan` derivations | `<derivation>_test.go` | small | none | returned `Snapshot`, `Status`, verbs, glyph, refusal and reason | every state, precedence and refusal case of the state machine ([ADR 13](../../../docs/adr/0013-only-plan-derives.md)) |
| `verdict`, `spend` | `<file>_test.go` | small | none | returned `Result`, red leaves, shares, fitted factors | verdict grammar; spend maths across resets and contamination |
| `agentlog` parsing | `<file>_test.go` + `testdata/*.jsonl` | small | recorded transcripts | parsed commands, edits, usage, cost; the line parser is fuzzed | reading Claude's transcript format, which another tool owns |
| `gh`, `tracker` adapters | `<file>_test.go` + `testdata/*.json` | small | injected executor returning recorded CLI output | decoded domain values; full-page and malformed-output errors; the decode is fuzzed | GitHub's output shape and our refusal to guess past a truncated page |
| `git` | `<file>_test.go` | medium | real repos from `gittest` | repo state after the call; worktree, merge and rebase state read back | our reading of the user's real repos |
| `tp`, `command`, `runner` | `<file>_test.go` | medium | fake `tp` on `PATH`; real subprocesses and process groups | argv sent to `tp`; output and errors; liveness, kill, reap, secret env stripped | the process edge: nothing orphaned, nothing leaked |
| `store` | `<file>_test.go` | medium | real Postgres via `cctest` | only what the database proves (below) | constraints, idempotence, rollback, migrations |
| `loop` tick | `<intent or file>_test.go` | medium | real Postgres and git; port fakes for forge, worktrees, runner, tracker; manual clock | store state after ticks; commands sent across an unmanaged edge | the loop acts on each decision once, retries what failed, and survives I/O failure ([ADR 9](../../../docs/adr/0009-the-loop-owns-reconciled-state.md), [ADR 12](../../../docs/adr/0012-the-two-phase-tick.md)) |
| `web` handlers | `<page>_page_test.go`, `<file>_test.go` | medium | real Postgres; port fakes | status, `HX-*` headers, what the user sees, the htmx contract, intents queued in the store | each page and verb answers both htmx and plain requests ([ADR 15](../../../docs/adr/0015-pages-are-routes-beside-a-polling-rail.md)) |
| `web/view` builders | `<file>_test.go` | small | none | the returned view model | rail grouping, scopes, params round-trip, charts, log detail |
| Page invariants | `<invariant>_test.go` in `web` | medium | real Postgres | a promise held by every rendered page or by the live schema | no inline script, every `hx-post` form disables itself, light only, no third-party request, each SQL `CHECK` matches its Go set |
| `config` | `<file>_test.go` | small | files in `t.TempDir`; a `config.Env` value | the returned `Config` or its error | precedence of key over env, defaults, bad-field messages |
| `app` | `app_test.go` | medium | real Postgres and git; port fakes | one tick runs and the page serves; a second instance is refused; clean shutdown | the wiring |
| Critical journeys | `e2e/tests/<journey>.txtar` | large | real `cc` binary; fake `gh` and `tp`; real git and Postgres | stdout, HTTP routes, DB state via `cc-wait` | the journey end to end, and wiring nothing smaller sees |
| `web/src/*.ts` | `<file>.test.ts` | small | injected timers and clocks | return values | key chords, layout, launch selection, poll merging |

**Never tested directly:** generated `internal/store/ccdb/**`; migrations (through `store`);
templates (through `web` handlers); `cmd/cc` and its flag wiring (through e2e);
`internal/web/assets`; `option`; `web/hx`; `internal/demo`; the Solid islands `graph.tsx` and
`launch-modal.tsx` (their logic lives in pure `.ts`); plain data types; test utilities; anything
whose only behaviour is calling something else.

## Doubles

- **Postgres → real**, via `cctest`, in every medium test. No store fake is written.
- **git → real**, via `gittest`. Local repos are fast and deterministic.
- **Unmanaged ports → one fake each**, in a `<owner>test` package beside the real one
  (`ghtest`, `tptest`, `runnertest`, `trackertest`), shared by `loop`, `web`, `app`, `demo` and
  e2e. Each owner exports `RunContract(t, newX)`; the real implementation runs it over recorded
  CLI output where the binary can't run.
- **gh and tp at the binary edge → fake binaries** on `PATH`, for `tp` adapter tests and e2e.
- **Time → injected**: `loop.Clock`, the `web` clocks, and timers in `.ts`.

**The unmanaged edges** whose commands a test may record: forge create, ready, edit, comment and
close; runner spawn and cancel, with the prompt, worktree and resume they carried; `tp` cut and
remove argv. Count calls only when the count is the behaviour: a PR opened once, ready called
exactly once across ticks, GitHub fetched once per cache window.

## plan, loop and e2e

`plan` owns every decision case. A `loop` test proves the tick acts on a decision: one happy path
per intent or effect, plus what only I/O shows (idempotence across ticks, retry after a failed
forge call, a git conflict, a rolled-back transaction). A loop case that restates a plan table
row is deleted.

```go
// plan: the case table
func TestDraftGate(t *testing.T) {
	t.Parallel()
	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	tests := []struct {
		name         string
		prs          map[string]plan.PRState
		verdictGreen bool
		wantDraft    bool
	}{
		{name: "blocker unmerged, verdict red", prs: map[string]plan.PRState{"pla-40": plan.Open}, wantDraft: true},
		…
	}
```

```go
// loop: the effect, once, across ticks
t.Run("draft gate readies the PR exactly once", func(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
	for range 10 {
		f.tick(t)
	}
	if got := len(f.Forge.Calls("Ready")); got != 1 {
		t.Errorf("Ready calls across ten ticks = %d, want 1", got)
	}
})
```

e2e holds one txtar per critical journey: track a repo, launch to draft PR, draft gate to ready,
steer and follow-up, merge and teardown, crash recovery. Plus the wiring only the real binary
shows: the fake `gh` and `tp` against `cc`, flags, the daemon's lifecycle. A txtar that asserts a
decision case or a page's content moves down to `plan` or `web`. When a large test catches a bug,
add the smaller test that should have caught it.

## Store tests

Cover only what the database proves: constraints and uniqueness, `ON CONFLICT` idempotence,
transaction rollback, concurrent writers (N goroutines, one wins), migrations over a legacy
database, and query semantics a caller depends on but can't easily set up (event paging,
retention, dedup, latest-per-window). Plain read-after-write is covered by the caller. Sabotage
once: remove the constraint or rollback and watch the test fail.

## Web tests

Read the page with `htmltest`. Assert:

- status and `HX-*` headers;
- text a user reads (a key phrase, not a paragraph), links and form targets;
- the htmx contract `AGENTS.md` freezes: `hx-*` attributes, `#main`, `#board`, the rail's scroll
  container;
- the state grammar Go composes: `glyph-{word}`, `data-glyph`, `data-hue`
  ([ADR 14](../../../docs/adr/0014-glyph-is-the-only-colour-key.md));
- intents queued in the store.

Never assert layout or utility classes, element nesting, an exact markup string or exact prose.
Escaping is asserted as "no live `<script>` element", not a pinned string. One test per page
proves it renders its view model; the model's cases are `web/view` tests.

## Golden files

A golden pins bytes an unmanaged consumer reads exactly: the prompt and settings an agent
receives, or a parser's output over a recorded real-world input. No goldens of HTML pages or
internal structs.

## Boundary gaps

Code without a testable surface yet. Don't write a test that reaches around the gap; add the seam
first.

None open.
