# Audit 05: store, app, config, cmd/cc, justfile

Verification method: every `ccdb.Queries` method and every `*Store` method was grepped across the repo
(prod and test, excluding ccdb). Result: NO sqlc query is unreferenced and NO store method lacks a
production caller, except `UpsertTickets` (test-only, see #5). This slice is lean in dead code. The mass is
in essential persistence plus comments. The wins are small and mostly structural.

## 1. Essential core
- `internal/store`: Postgres via pgx + goose + sqlc. Ticket import/withdraw, runs/dispositions/metrics,
  intents + launches, pushes, refresh/push/removal facts, events, meta (JSON kv), auth, utilization
  readings/intervals/spend fit. ~70 queries, all live. Each store file is a thin typed wrapper around
  ccdb plus row to plan.* mapping. That mapping is the domain boundary, not a delegating layer.
- `internal/store/ccdb`: generated, all of it reachable.
- `internal/app/app.go` New/Run/Close + `lock.go` flock (inv. 9, one instance per workspace).
- `internal/config/config.go` TOML load + defaults, `statedir.go` workspace layout.
- `cmd/cc`: main (config flag, signal ctx), `open`, `useradd`, `passwd` (auth is real), demo/e2e build-tag hooks.
- justfile: build/run/up/down/test/test-e2e/lint/fmt/assets/sqlc/migrate-create/ci/check-conflicts/demo.

## 2. Findings (biggest cut first)

| tag | what to cut | replacement | path:line | est. lines removed | confidence | risk |
|---|---|---|---|---|---|---|
| comments | ~190 of 322 comment lines in prod store/app/config/cmd are non-contract: unexported-func docs, `(inv. 9)`/`§ 11` design cross-refs, "Production no longer calls this", narrative on `metaX` consts, history ("Phase 4", "issue #89", "CC-313") | delete; keep exported <=3-line contracts and real outside constraints (Postgres/gh/sqlc) | internal/store/*.go (216 comment lines, ~140 removable), internal/config/*.go (59, ~25), internal/app/*.go (35, ~20), cmd/cc (12, ~6) | ~190 (hand-written) | high | none |
| yagni | `WithRunner`, `WithForge`, `WithWorktrees`, `WithTrackerSource` options plus `runner/forge/worktrees/trackerFor` fields and the `if nil` wiring. Zero callers anywhere (tests use only WithClock/WithObserver/WithRepoCheck/WithMetricsParser; e2e uses WithCheckout). Demo builds loop/server directly via SetTrackerSource, not app. | delete; re-add when a test needs it. Then `SetForge/SetWorktrees/SetRunner` call-through shrinks to constructor defaults | internal/app/app.go:34-44,66-104,159-206 | ~35 | high | low |
| shrink | `useradd` and `passwd` are 95% identical (flags parse, load cfg, open store, gen pw, hash, print) | one `credential(ctx, configPath, args, name, apply func(store, email, hash) error)` | cmd/cc/subcmd_prod.go:68-153 | ~28 | high | low |
| yagni | `LastPushedTips` (method + query + sql) is derivable from `LatestPushes` (`pushed[id].PushedTip`); 3 callers in loop | callers read `LatestPushes` and take `.PushedTip`; drop method, query, generated func | internal/store/pushes.go:49-62; queries/pushes.sql; ccdb/pushes.sql.go:57-~75; callers loop/push.go:27,211, loop/verbs.go:502 | ~15 hand + ~22 generated + 4 sql | medium (check call-site cost of a bigger map, trivial) | low |
| yagni | `UpsertTickets` + `UpsertTicket` query: its own doc says "Production no longer calls this" (only 125 test call sites seed with it). Prod binary carries it plus the generated query | move to a test-only helper (e.g. `store/storetest` or internal/cctest) that runs raw SQL, or drive tests via `ImportTickets` | internal/store/store.go:74-108; queries/store.sql:1-8; ccdb/store.sql.go:365-~400 | ~35 hand + ~35 generated | medium | medium (touches 40 test files unless the helper keeps its name) |
| shrink | `QueueVerbIntent` / `QueueVerbIntentWithPayload`: two queries, two store methods, two sqlc funcs for one INSERT with optional payload | one `QueueVerbIntent(..., payload string)`; payload `NULL` when empty (`NULLIF($4,'')`). Update the one payload caller (web/server.go:443) and N callers of QueueVerbIntent | internal/store/runs.go:335-362; queries/runs.sql:36-40; ccdb/runs.sql.go (QueueVerbIntent*, ~30) | ~14 hand + ~28 generated + 4 sql | medium | low (call-site churn) |
| stdlib | Flock.Close does explicit `LOCK_UN` before close; closing the fd releases a flock. Also stale doc says "SQLite" and "DB path" while the lock is on a state file | `return l.f.Close()` | internal/app/lock.go:15-40 | ~6 | high | none |
| stdlib | goose globals + `sync.Once` + `setUpGoose` to avoid races between concurrent opens. goose v3.27 has `goose.NewProvider(dialect, db, fsys)`: no globals, no Once | `provider, _ := goose.NewProvider(goose.DialectPostgres, db, sub-FS)`; `provider.Up(ctx)` | internal/store/store.go:46-70 | ~14 | medium (needs `fs.Sub(migrations,"migrations")`; check test that opens twice) | low |
| shrink | `ResolveClaudeProjectsDir` duplicates `ResolveDataDir`'s "configured else default, expandHome, Abs" shape; `resolveDatabaseURL` is a third copy of the precedence ladder | `firstNonEmpty(configured, env, default)` helper + one `absHome` | internal/config/statedir.go:30-90 | ~14 | medium | low |
| yagni | Duplicate justfile migrate recipes: app auto-migrates on `OpenStore`; keep `migrate-create`, drop `migrate-status/up/down/redo/reset` and `db_url`/`goose` vars (or keep `status` only) | delete recipes | justfile:14-35 | ~22 (not Go) | medium (owner may use `redo` when writing migrations) | low |
| yagni | `CC_AGENT_COMMAND` env override (`applyAgentCommandEnv`): a second way to set `agent_command`, which already lives in TOML | delete, or set `agent_command` in a local config. Documented in README:68,235 and sample toml, so update docs | internal/config/config.go:143-160 | ~19 | medium (owner's local workflow `caffeinate` wrapper per README:71) | medium |
| yagni | `max_turns` key: appended to argv; the user can already write `--max-turns` in `agent_command` | delete field + the append block + sample toml block | config.go:30-33,127-130; sample toml:49-52 | ~8 | low-medium (user-visible knob, in README) | medium |
| yagni | `board_poll_seconds` config key + `>=1` validation + `Server.SetBoardPollSeconds` + `view.SetBoardPollSeconds` chain exists only to let demo set 1 (real use is default 5; sample toml and README?). Keep setter for demo, drop key and validation | demo calls the setter; drop `BoardPollSeconds` Config field | config.go:36-38,75,96,103-105; app.go:202 | ~8 (+README row) | low-medium | low |
| yagni | `requireAgentCommandParts` validation of required argv tokens | keep: it guards a real footgun (missing deny settings/agents). Not recommended to cut | config.go:162-175 | 0 | n/a | n/a |
| shrink | `LatestReadings` wraps `LatestReadingsFull` into a `Gauge` struct that is just `{Utilization, ResetsAt}` of agentlog.Reading; consumer web/view/reader.go:118,183 could take `agentlog.Reading` directly | drop `Gauge` type + wrapper; callers use map[Window]Reading | internal/store/utilization.go:29-46 | ~16 (+ view code adjustments) | low-medium | low |
| shrink | Numbered `Kind_2..Kind_5` params for `IN ($1..$5)`; use `= ANY($1::text[])` | smaller params structs and call sites (LatestRefreshOutcomes, PushFacts) | queries/refresh.sql:9, queries/pushes.sql PushFacts; store/refresh.go:47-54; pushes.go:95 | ~10 hand + ~12 generated | medium | low |
| shrink | `TicketFeature` vs `TicketFeatureAny` differ only by `withdrawn_at IS NULL`; could be one query with a bool, or leave | leave. 2 lines of SQL each | queries/store.sql:14-18 | ~0 | n/a | n/a |
| yagni | Migrations 0002 (2 lines), 0003 (2 lines), 0004/0006 additive columns could be folded into 0001 for a fresh-install-only project. Breaks existing DBs' goose history. | squash only if no deployed DB matters | store/migrations/0001-0009 | ~20 | low | high |
| idiom | `cmd/cc` hook indirection (`demoSubcmd` global var set in `init()` via build tag, plus `runOptions` global) is a registry for exactly two tags. Acceptable; no change recommended | none | cmd/cc/main.go:38-55 | 0 | n/a | n/a |

## 3. Comment violations summary
- Counts of `//` lines in prod files: store 216, config 59, app 35, cmd 12 (total 322), plus 6 prose lines in query .sql (copied into generated code).
- Roughly 190 fail the test. Patterns: (a) design-doc breadcrumbs `(inv. 9)`, `§ 11 inv. 11`, `CC-313`, `issue #89`, `Phase 4's crash-safety hinges on`; (b) docs on unexported funcs (`repairBlockedBy`, `pendingLaunchIntents`, `closeUnderBlockedBy`, `ticketFeatureLookup`, `agentCommandEnv`, `setUpGoose`); (c) historical narration (`UpsertTickets` "no longer calls this itself"; `Config.Repo` "pre-Phase-5 behaviour"); (d) restating-signature doc on exported (`Flock.Close`, `Store.Close`, `Tickets returns every ticket`, `AppendEvent`, `LastObservation` have none; several others restate); (e) docs longer than 3 lines on exported (`RestackedSinceLastPush` 4 lines, `QueueVerbIntent`, `RunIDsForTicket`, `LatestRefreshOutcomes` 4 lines, `RunOnce` none but `Run`).
- Genuinely allowed to keep: `//go:build`, `//go:embed`, `_ "pgx"` driver note, the `json.Marshal of []string cannot error` is an own-code claim (delete), `BaseContext` note about Shutdown waiting on active connections (stdlib behaviour: keep, outside constraint), the `>=` stamping note in `RestackedSinceLastPush` is own-code logic (fix code or drop).
- Stale and misleading: lock.go:20 mentions SQLite and "DB path" though store is Postgres; statedir.go:41 `// databaseURLEnv names...` comment sits above a two-const block.
- Config struct fields carry "holds the resolved X after LoadConfig, not the raw config key" three times (config.go:24-27): this is the confession the rule describes; fix by separate resolved type or drop.

## 4. Subtotals (lines removable)
- High-confidence, hand-written: comments ~190 + options 35 + useradd/passwd 28 + Flock 6 = **~259**.
- High-confidence, generated: 0.
- All (hand-written, incl. medium/low): ~259 + LastPushedTips 15 + UpsertTickets 35 + QueueVerb merge 14 + goose provider 14 + config resolve helpers 14 + env override 19 + max_turns 8 + poll_seconds 8 + Gauge 16 + Kind_N 10 = **~412**, plus justfile ~22 non-Go.
- All, generated (ccdb): LastPushedTips ~22 + UpsertTicket ~35 + QueueVerb merge ~28 + Kind_N ~12 = **~97** (sql source ~16 more).
- Bottom line: this slice is ~85% essential. It is not where the "too much code" lives. The ceiling for the slice is about 500 lines out of ~4.4k hand-written + 2.3k generated; the rest of the weight is the domain (loop/web/plan), which other auditors cover.
