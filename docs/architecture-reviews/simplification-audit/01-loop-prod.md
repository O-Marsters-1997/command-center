# Audit 01: internal/loop production code (2736 lines, 19 files)

## 1. Essential core (do NOT cut)
- loop.go Run/RunOnce + absorb/act/derive: observe -> persist -> derive snapshot -> act. The tick is the system (inv. 9/10).
- observe.go NewObserver: fetch, PR snapshot, worktree map, branch tips, MidMerge. Plan input; no observe, no loop.
- loop.go launchEligible/cutAndSpawn/spawnRun: cut worktree, write prompt/log, spawn, RecordSpawn. Core of "ticket -> agent".
- loop.go reconcileRuns/disposeRun/commitsSinceBaseline: liveness + disposition (commits after baseline decide push vs failed). Crash recovery.
- push.go pushPushable/pushOne/pushBranch + applyRetryPushIntents: push, deny-list check, adopt-or-create PR, RecordPush.
- refresh.go refreshOne/advanceOnto/restackBoundary + retarget.go: stacked-branch rebase/merge when a parent squash-merges (repo is squash-only so this is required for the DAG).
- verbs.go applyRemoveWorktreeIntents/removeWorktreeOne (post-merge cleanup), applyAbortIntents, applyResolveIntents, applyCancel/Kill/ReRun intents. These are the operator verbs.
- draftgate.go: PRs open as drafts, un-drafted when ready (CLAUDE.md rule).
- loop.go applyImportIntents/importFeature/applyEditTicketIntents: loop is the tickets table's only writer (inv. 9).
- settings.go deny-list settings (git push / gh denied to agents): security-relevant.
- Clock interface (tests + demo sim drive it), Set{Forge,Worktrees,TrackerSource} used by demo/sim.go and app.go (real consumers, not test-only).

## 2. Findings (biggest cut first)

| tag | what to cut | replacement | path:line | est. lines | conf | risk |
|---|---|---|---|---|---|---|
| comments | ~200 of ~298 `//` lines violate comments.md (unexported doc comments, "why/note" prose, issue-number archaeology, `docs/prds/...` pointers) | delete; keep ~60 on exported identifiers + outside constraints (diff(1) flags, gh DetailsURL shape, Mergify) | all files, see §3 | 200 | high | none |
| shrink | 14 copies of the intent-consumer skeleton: `PendingVerbIntents` -> `len==0 return` -> `now :=` -> loop -> `snap.Entry`/`byTicket` ok-check -> `ConsumeVerbIntent` | one helper `func (l *Loop) eachIntent(ctx, verb string, fn func(store.VerbIntent) error) error` that fetches, calls fn, consumes (each applyX shrinks to 4-8 lines; the early-return exists only to avoid loading side tables, do that lazily in fn / closure) | verbs.go:44,91,155,280,310,382,459,493; push.go:76,101; loop.go:268,324,351; refresh.go:17 | 140 | high | low (golden-ish tests cover each verb) |
| yagni | Whole-feature candidates the owner may not need (see §2b) | delete per feature | various | 614 | med | product decision |
| shrink | `act`/`absorb` are 15+10 repeated `if err := ...; err != nil { return err }` (3 lines each) | `for _, step := range []func() error{ func() error { return l.applyReRunIntents(ctx,snap,obs) }, ... } { if err := step(); err != nil { return err } }`, or `errors.Join`-free early-exit loop; saves ~25. NB `rereadLocalTips` and the `l.spawned` loop sit between steps and stay as steps | loop.go:145-228 | 30 | high | low |
| shrink | repo lookup maps rebuilt per call: `repoPathsByName` called 11x/tick, plus `verifyCommandByRepo`, `generatedByRepo`, `buildCommandByRepo` (ticket.go, 37 lines) each building a map to read one field | `func (l *Loop) repo(name string) config.Repo` over a map built once in NewLoop (or `slices.IndexFunc`); callers read `.Checkout/.VerifyCommand/.Generated/.BuildCommand`. ticket.go and its package doc move to loop.go | ticket.go:1-37; verbs.go:105,169,324,391,468; push.go:164,201; merge_events.go:24; loop.go:205,491,508; draftgate.go:20; retarget.go:47; refresh.go:187; generated_conflict.go:30-31 | 30 | high | low |
| shrink | `settings.go`: three `Write*` funcs, each "write constant to path"; `agentDefinition` struct + `json.MarshalIndent` for a static document; callers (app.go:169-175, demo/sim.go:161-163) call all three | one `WriteAgentFiles(ws config.Workspace) error` writing three consts (digest def as a raw-string const; drop struct + json import). Or `//go:embed` three files | settings.go:24-100; app.go:169; demo/sim.go:161 | 35 | high | low |
| shrink | `parseRunMetrics` / `parseReadings` / BackfillMetrics all repeat "parse; if !errors.Is(err, fs.ErrNotExist) log; return nil" (4 copies) | one `ingestRunLog(ctx, store, run, ...)` used by both disposeRun and BackfillMetrics, one `logUnlessMissing(err, what, path)` helper | loop.go:446-475; metrics.go:35-53 | 18 | high | low |
| yagni | `AssertReposSquashOnly(ctx, ws, repos)`: `ws` param never read; 20-line file wrapping a 4-line loop over `git.CheckSquashOnly`; app has a `RepoCheckFunc` option type just to stub it | move the loop into git (`git.CheckAllSquashOnly`) or inline in app.go; drop `ws`; keep stub option only if tests need it | repocheck.go:13; app.go:59,139-143 | 15 | high | low |
| yagni | 12 const aliases of `plan.Verb*` (`reRunVerb = plan.VerbReRun`, `killVerb`, `importVerb = store.ImportVerb` in own file import.go, `retryPushVerb`, `commitResolutionVerb`...) | use `plan.VerbX` at the call site; delete import.go (5 lines) | verbs.go:16-25; loop.go:26; import.go:1-5; push.go:14-18 | 18 | high | none |
| shrink | `abortOne`, `commitResolutionOne`, `refreshOne` each re-implement the "no worktree for X / a run is alive in Y" check that `idleWorktreeFor` already does (verbs.go:140) | call `idleWorktreeFor` (it returns path+refusal); refreshOne's third case (MidMerge) stays | verbs.go:76-82; push.go:131-137; refresh.go:136-144 | 14 | high | low |
| shrink | `store.Event{At: now, TicketURL: ..., Kind: ..., Detail: ...}` + `l.store.AppendEvent(ctx, ...)` spelled out ~35 times; `now time.Time` threaded through ~15 function signatures only to stamp them | `func (l *Loop) event(ctx, url, kind, detail string) error` stamping `l.clock.Now()`; drop `now` params | verbs.go:72,126,203,214,292,417,474,539,591; push.go:127,208,220,228,240; refresh.go:132,147,163,171,181,210; retarget.go:49,63; draftgate.go:36; generated_conflict.go:89; loop.go:441,674,684 | 40 | med | low (clock.Now() per event vs per tick: ordering unchanged) |
| yagni | `launchSpec` struct exists only to pass 4 values from launchEligible/reRunOne to cutAndSpawn; `spawnRun` takes 9 positional params (`"", "", runKindAgent, "", ""`) and a `kind` switch that builds the prompt inside the spawner | callers compose the prompt (`plan.Compose` / `ComposeResolve` / `ComposeFollowUp`) and pass `spawnSpec{ticket, worktree, baseline, hash, kind, prompt}`; cutAndSpawn takes `(ticket, baseBranch, hash, repoPath)` | loop.go:549-556,597-612; verbs.go:134,220,377 | 15 | high | low |
| yagni | `branchKey` is a one-line wrapper over `plan.BranchKey`; `defaultBaseBranch` copy "mirrors plan's own unexported copy" | `plan.BranchKey` directly (sed, ~50 sites); export `plan.DefaultBaseBranch`; keep `mainTipKey` only if used outside observe.go (it is only used observe.go:66 + export_test) -> inline | branchkey.go:1-17; export_test.go:14-17 | 10 | high | none |
| yagni | duplicate `Clock` interfaces: loop.Clock and web.Clock | web imports loop.Clock, or both use a func; delete one | clock.go:7; web/server.go:121 | 5 | med | low |
| stdlib | `importFeature` repeats `errors.As` + `RecordImportRefusal` for two error types; | give `FeatureConflictError`/`FeatureClosureError` a shared interface (`Feature() string`) and one `errors.AsType[...]` (Go 1.26) | loop.go:309-318, 332-340 | 6 | med | low |
| stdlib | `maps.Copy`/`slices` already used; `ticketsByURL`, `branchesFor` hand-rolled | `branchesFor` -> `slices.Collect` over filter; `ticketsByURL` keep (no stdlib equiv). `tickCheckingWaits` builds url slice -> store could take tickets | observe.go:200; loop.go:529-539 | 8 | low | low |

### 2b. Non-essential FEATURES in this slice (delete as units; need owner call)
| tag | feature | where | est. lines (this slice) | conf | notes |
|---|---|---|---|---|---|
| delete | generated-file auto-resolve (issue #177): merge main, rerun build cmd, commit | generated_conflict.go (93) + generatedByRepo/buildCommandByRepo (14) | 105 | med | Also drops `ConflictsWithBase`/`ConflictedPaths` obs (observe.go:77-85, ~10) if nothing else reads them, plus config.Repo.Generated/BuildCommand and plan.AllGenerated outside slice. Agent resolve verb already covers conflicts. |
| delete | `fetchCIFailedLog` + `lastLines` + `ciLogUnavailableSection` + follow-up CI log section (issue #232) | verbs.go:223-278 (+ follow-up plumbing pushFacts/vd/obs params, verbs.go:164-177,198-220) | 70 | med | Follow-up verb itself keeps working with operator's text. |
| delete | one-shot `BackfillMetrics` startup migration (runs awaiting backfill predating ADR 10) + `MetricsParser` seam + `RunsAwaitingMetricsBackfill`/`BackfillRunMetrics` store fns outside slice | metrics.go:1-56; app.go:187-196 | 60 | med | After one run on a given DB it is dead code. Cut once all live DBs migrated. |
| delete | re-run prompt-diff preamble: shells out to diff(1), writes runs/<id>.diff, `re_run_no_diff` event, `oldPromptPath` plumbing | loop.go:624-633,680-724; verbs.go:333-336,343,361,377; verbs.go:609 | 60 | med | Nothing reads .diff except store prune (store/runs.go:421). Prompt-hash change already visible. |
| delete | analytics-only recorders: `recordFirstPushCI` (+SetFirstPushCI), `recordHandChurn` (+SetHandChurnLines, git.LinesChanged) | first_push_ci.go:1-51; merge_events.go:49-67; loop.go:174 | 73 | med | Insights page only; not control plane. Verdict-transition events (verdict_transitions.go) remain as the audit trail. |
| delete | `supersededConflict` + `conflictDetail` + `parseConflictDetail`: round-trips tips through a space-separated Detail string to un-gate auto-refresh (issue #188) | refresh.go:81-111 | 30 | low | Cut = after a conflicted refresh, only the `refresh` verb retries. Behaviour change; stringly-typed state is the smell. |
| delete | verb: re-check (`gh run rerun`) incl. `runIDFromDetailsURL` | verbs.go:380-454, event consts | 70 | low | Shares runIDFromDetailsURL with fetchCIFailedLog. Operator convenience for flaky compat check. |
| delete | verb: close-pr | verbs.go:456-487 | 32 | low | Operator can close PR on GitHub. |
| delete | peer-conflict cache (#180): `LastObservation` read, `cachedPeerConflict`, `prev` arg | observe.go:30-33,161-164,175-183 | 20 | low | Perf only; N^2 merge-tree calls per repo per 5s tick if cut. Keep unless ticket counts small. |
| yagni | `planPR`: field-by-field copy gh.PR -> plan.PR (+Checks map copy) because plan can't import gh | observe.go:210-223 | 14 | med | Make gh return plan types or alias `type PR = ...` in the leaf package. Cuts gh/plan duplicate struct too (outside slice). |
| yagni | Mergify-specific: `mergifyHash` + `MergifyHash` obs + branch in observer | observe.go:112-119,125-135 | 20 | low | Owner-specific gate (design §7). |
| yagni | `verifyOne` output truncation (`maxVerifyDetail`), `ponytail:` comment | refresh.go:190-214 | 8 | low | Trim only; verify cmd itself is a feature (cut 25 if dropped). |
| yagni | Setter-injection of metricsParser/forge/worktrees/tracker: 4 `SetX` methods that mutate after `NewLoop` set defaults; demo/sim.go:113 calls `SetMetricsParser(agentlog.ParseMetrics)` == the default (dead call) | functional options on `NewLoop` or exported fields; drop MetricsParser entirely if BackfillMetrics goes | loop.go:75-98 | 12 | med | |

## 3. Comment-rule violations per file (count of `//` lines / est. removable)
| file | `//` lines | removable | examples |
|---|---|---|---|
| loop.go | 83 | 65 | L34-36 "Event kinds ... prd Phase 6" (const group, unexported); L43-44; L60-62 NewLoop doc is 3 lines of restating args; L106-107/112-113 "Runs before observe, not after" (code-ordering justification; the ordering is the fix, the comment argues for it); L165-167; L240-241; L256-258; L379-381; L414-415; L477-480 (issue #189 archaeology); L525-528; L549-550 launchSpec; L558-560; L587-596 (10-line spawnRun essay, "see the PR description"); L642-645; L667 |
| verbs.go | 62 | 50 | L41-43, L67-69, L137-139, L151-154, L194-196, L223-224, L228-230, L270-271, L305-309, L356-358, L380-381, L410-411, L433-434, L456-458, L489-492, L521-533 (13-line doc on unexported removeWorktreeOne), L598-600 |
| refresh.go | 35 | 28 | L15-16 (orphan comment, names no declaration), L44-47, L81-83, L96-100, L113-115, L120-122, L159-161, L192-193 (`ponytail:` marker, comments.md says it gets no exemption), L216-221, L241-244 |
| observe.go | 23 | 16 | L21-23, L61-63, L71, L113, L125-127, L137-138, L141-146, L175-176, L185-187 (references "internal/loop's decide step", which does not exist) |
| push.go | 17 | 13 | L14-15, L22-25, L62, L73-75, L171-174, L189-192, L198 |
| retarget.go | 15 | 13 | L17-19, L38-43, L54-59 (issue #95 narrative) |
| settings.go | 14 | 8 | the three "Idempotent: the content never varies by call" doc comments (L24-25, L60-61, L88-90) are the same sentence x3; L9-11, L33-35 |
| verdict_transitions.go | 8 | 6 | L11-18 doc on exported-less internal method |
| metrics.go | 9 | 5 | L18-22, L28-29 |
| branchkey.go | 8 | 8 | whole file's comments restate the code or mirror another package |
| draftgate.go | 5 | 4 | L16-18, L33-34 |
| generated_conflict.go | 5 | 4 | L17-20 (cites `push.go:54` line number, already stale), L36, L64 |
| merge_events.go | 2 | 2 | L49-50 |
| checkout.go / repocheck.go / clock.go / ticket.go / first_push_ci.go | 3/3/3/2/1 | 2/2/0/2/1 | repocheck "mirrors OpenStore's schema_version check"; ticket.go package doc is fine (keep, move) ; clock.go doc on exported Clock is OK |
| **total** | **~298** | **~205-215** | |

Violation shapes: ~40 doc comments on unexported identifiers; ~25 issue-number ("issue #85/#89/#93/#95/#177/#188/#189/#196/#232") narratives; ~30 pointers into `docs/prds/prd-command-centre.md` or `docs/designs/...` section numbers (rot-prone; the prds dir was untracked per recent commit "Move plans under docs, untrack plans and prds" so these links may already dangle); 1 `ponytail:` marker.

## 4. Idiom notes (no line count, worth a refactor)
- idiom: `store.Event` kind strings are split: `store.EventPushed/EventRefreshed/...` live in store, ~30 more `event*` consts live in loop across 8 files. Pick one home (store).
- idiom: `Loop` mutable `spawned []string` field is per-tick scratch state reset in `absorb` (loop.go:158) and read in `act`/`launchEligible`; make it a return value or local on a per-tick struct.
- idiom: `NewLoop` takes 6 positional args then 4 post-construction setters; use functional options (as app.go already does).
- idiom: `RepoNameForDir` (checkout.go) is only used by cmd/cc/subcmd_prod.go:53; belongs in config/git, not loop.
- idiom: `rereadLocalTips` re-splits the key with `strings.Cut(key, "//")`, inverse of plan.BranchKey; add `plan.SplitBranchKey`.
- idiom: `commitsSinceBaseline`, `disposeRun` ignore `ctx` cancellation semantic differences; fine.
- idiom: errors: several `return err` without context in verbs.go/loop.go (store calls); ok in a single-caller chain, inconsistent with the wrapped ones.

## 5. Subtotals
- High-confidence removable: **~515 lines** (comments 200, intent helper 140, act/absorb 30, repo lookup 30, settings 35, parse dedupe 18, alias consts 18, spawnRun/launchSpec 15, repocheck 15, idleWorktreeFor 14, branchkey 10; minus ~10 overlap with comments).
- All findings (adds med/low: event helper 40, features 2b ~560 net of overlap, clock/errors/stdlib ~19): **~1,130 lines of 2,736 (~41%)**.

One-line severe flags: none seen in this slice (spawn gap race is documented; agent deny-list is the security control and must stay).
