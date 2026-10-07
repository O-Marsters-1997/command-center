# Simplification audit — 2026-10-07

Whole-repo audit for over-engineering, comment-rule violations (`~/.claude/rules/comments.md`) and
go-idiomatic drift. Eight parallel auditors, one per slice; per-slice tables with `file:line`,
confidence and risk are in [`simplification-audit/`](simplification-audit/). Estimates, not
measurements: auditors sampled large test files, and slices overlap by roughly 10%.

## Verdict

The contention holds, but not evenly. **The production logic is mostly proportionate; the weight
is in tests, comments and a few bolt-on features.**

- Tests are 1.7× production (27k vs 16.6k). `internal/loop` is 4:1 — 190 test funcs, one `t.Run`,
  zero tables, ~100 copies of the same fixture setup.
- ~1,800 of ~2,800 Go comment lines fail the comment rule (issue-number breadcrumbs, `inv. N`
  citations, doc comments on unexported funcs, 10–14-line doc blocks).
- Two subsystems are not the product: demo mode (~3,400 lines) and half-built auth (~520 lines,
  nothing checks a session).
- The core — tick, derivation, state model, store, git/gh/tp integration — is load-bearing and
  should stay. The 21-state model is fully reachable; leave it alone.

## Lines removable, by slice

| Slice | Size now | High-confidence cut | All findings (incl. owner calls) |
|---|---:|---:|---:|
| `internal/loop` prod | 2,736 | 515 | 1,130 |
| `internal/loop` tests (+ `cctest`) | 11,190 | 2,000 | 4,600 |
| web prod (Go, templates, TS, CSS) | ~5,500 | 750 | 1,230 |
| web, view and e2e tests | ~8,600 | 1,100 | 2,700 |
| store, app, config, cmd (hand-written) | ~3,100 | 260 | 430 (+100 generated) |
| plan, verdict, tracker, spend | ~6,200 | 440 | 1,040 |
| git, gh, runner, agentlog, auth | ~5,100 | 760 | 1,850 |
| demo mode (incl. scenarios, plan doc) | ~3,400 | — (owner call) | 3,400 delete / 1,100 simplify |
| **Total** | **~47.8k** | **~5,800 (12%)** | **~15,000 after overlap (31%)** |

High-confidence splits ~2,100 production / ~3,700 test. Zero dependencies removable: every
devDep and Go module is used.

## What is essential (do not cut)

- **The tick** — `loop.RunOnce`: observe → persist → derive → act; run disposition by
  baseline commits; push with deny-list check; draft gate; restack/retarget on squash-merge;
  the operator verbs; loop as sole writer of tickets (ADR 0011, 0014).
- **Derivation** — `plan.Rules.Derive`, `Unlocked`, `Status`, `Verbs`/`Tone` tables, verdict
  predicate grammar, spend fit (ADR 0006, 0012, 0015).
- **Persistence** — Postgres + goose + sqlc; all ~70 queries live. Store wrappers are the
  ccdb → `plan.*` boundary, not a pass-through layer.
- **Integrations** — git worktree map / `MergesCleanly` / mid-merge handling, `tp` new/remove,
  `gh` list + verbs, runner spawn/liveness/cancel by pgid, agentlog parsers with consumers.
- **Web** — board + htmx poll, intent-queuing POST routes, two islands, SSE log, `@theme` tokens
  and the named state grammar.
- **Tests that pin behaviour** — goldens, HTTP/security contract, candidate/launch semantics,
  the 36 txtar e2e scripts with `faketp`/`fakegh`, verdict grammar tests.

## Owner decisions (biggest levers)

Each is a feature cut, not a refactor. Line counts include the tests riding on them.

| # | Decision | Lines | Notes |
|---|---|---:|---|
| 1 | **Demo mode**: delete / shrink to a seeded board / keep simplified | 3,400 / 2,800 / 1,100 | No product hooks outside `cmd/cc/register_demo.go`. Demo is the second impl of `gh.Forge` and `git.Worktrees`; deleting it lets the loop's `SetForge`/`SetWorktrees` seams shrink to test-only fakes. |
| 2 | **Auth**: finish login or delete | ~520 | `VerifyPassword`, `NewSessionToken`, `HashToken`, `UserForLogin` have no prod caller. `useradd`/`passwd` write a password nothing checks. `docs/deployment.md` already says "missing password". |
| 3 | **Loop bolt-ons**: generated-conflict auto-resolve, CI log in follow-up, re-check verb, close-pr verb, re-run prompt diff, analytics recorders (`recordFirstPushCI`, `recordHandChurn`), one-shot `BackfillMetrics` | ~490 prod + ~1,200 test | `generated_conflict_test.go` alone is 514. Each is independently deletable. |
| 4 | `POST /ticket` — no UI or doc caller | ~60 + ~300 | Only `server_ticket_test.go` hits it. |
| 5 | `insights.tsx` → Go-rendered SVG like `contextcurve.go` | ~150 | Also removes a duplicate `niceTicks`. CLAUDE.md says two islands; there are three. |
| 6 | `/confirm` page → `hx-confirm` | ~95 | |
| 7 | `agentlog.Accumulator` (third request-id dedupe), `ToolCalls`/`ToolFailures` metrics nobody displays | ~310 | Metrics cut needs a migration. |

## Mechanical refactor plan (no behaviour change)

Ordered so each wave is one reviewable PR per package and goldens stay green.

**Wave 0 — dead code (~450).** `rm -rf internal/cc/` (untracked stale dist).
`web/src/charts.tsx` Sparkline/BarChart/Histogram/sparkPoints/histogramBars + `.spark-*`/
`.chart-bar*` CSS (~220). `app.With{Runner,Forge,Worktrees,TrackerSource}` (35).
`tracker.IssueBody`, test-only `tracker.BranchNumber` duplicated at `plan/derive.go:298`,
`verdict.Verdict.String`, unread `Facts.Ticket`/`Facts.Now` (70). `types.ts` mirrors 45 Row
fields where 7 are read (60). `UpsertTickets` → test helper (70).

**Wave 1 — comments (~1,800).** `/clean-comments` per package. Densest: `loop` (205 prod +
380 test), `plan` (190), `web` (290 prod + 250 test), `store`/`config` (190). Fix the three
stale ones rather than delete blindly: `plan/push.go:92`, `plan/plan.go:265` (claims plan cannot
import verdict; `derive.go:11` does), `app/lock.go` (still says SQLite).

**Wave 2 — production shrinks (~650).**
- `loop`: one `eachIntent(ctx, verb, fn)` replaces 14 copies of the pending→consume skeleton (140);
  `act`/`absorb` error ladder (30); `l.event()` for ~35 `store.Event{}` literals and the `now`
  threaded through 15 signatures (40); per-tick repo maps built once (30); `settings.go` three
  writers → one `WriteAgentFiles` (35); drop 12 `plan.Verb*` aliases and `import.go` (18);
  unused `ws` param on `AssertReposSquashOnly`; `spawnRun`'s 9 positional params.
- `web`: 12 embeds → `//go:embed *.tmpl` + `ParseFS` (85); one handler adapter for 39
  `http.Error` repeats (70).
- `plan`: collapse five `RunFact.Verdict*` bools + `VerdictLabel` round-trip into one
  `verdict.Verdict` field (40); share the hand-rolled `/**` glob matcher between `push.go` and
  `generated.go`.
- integrations: one `run(cmd)` exec+stderr helper across git/tp/gh (55).
- `cmd/cc`: `useradd`/`passwd` share one helper (28) — moot if auth goes.

**Wave 3 — test consolidation (~3,700).**
- `loop`: one `newLoopFixture` helper replacing ~100 repeated date/upsert/NewLoop blocks and six
  bespoke fixture structs (1,100); table-drive the families — remove-worktree refusals,
  spend-limit trio, "never touches a live-run worktree" ×5, import refusals (1,000); move the
  ~1,800 lines testing store/auth/agentlog/spend into those packages and trim (600); six
  gh/tp shell-script installers duplicating `e2e/fakegh`/`faketp` (260); web-rendering tests
  living in loop (390).
- `web`: `testNow`/`get`/server-builder helpers for 125 `NewServer` + 73 identical `time.Date`
  (650); drop HTML-substring tests the goldens already pin (500); drop derivation re-asserted
  through HTTP — keep one smoke test per route (550); delete tests of the test doubles
  (`e2e/faketp/main_test.go`, `fakegh/main_test.go`, `agents_test.go`, 585).
- `runner`: nine near-identical Spawn tests → one table (300).
- `plan`/`verdict`: gate tests duplicating `Derive` snapshot tests per ADR 0015 (280); verdict
  fixtures duplicating `TestEvaluateGrammar` (150); tautological `String` tests (52).

## Idiom notes

No systemic go-idiomatic failures: errors wrap with `%w`, contexts reach `exec.CommandContext`,
and the three interfaces (`gh.Forge`, `git.Worktrees`, `runner.Runner`) each have two impls.
The recurring idiom debt is in tests — no table-driven tests in `loop`, setup copied instead of
helpers — and in setter injection (`loop.Set*` mutating after `NewLoop`, one demo call setting
the default `MetricsParser`).

## Out of scope

No severe correctness or security issues surfaced. Auth is the closest: the board is unauthenticated
while a users table and password tooling exist, which is a decision (#2), not a bug.
