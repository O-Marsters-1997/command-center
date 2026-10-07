# Audit 06: plan / verdict / tracker / spend (read-only)

Paths are relative to /Users/olly/Documents/coding/ai-development/command-center. Sizes: plan 1.6k prod / 2.5k test, verdict 331 / 624, tracker 288 / 340, spend 208 / 295.

## 1. Essential core

These packages are mostly proportionate. The code is pure, table-testable and has few callers per function. The bloat is comments and layered tests, not logic.

- plan/plan.go: `Unlocked` (blocker/unlock/base rules), `State` + `Status` + `statusFromPush`. This is the ordered priority ladder that ADR 0015 centralises. Keep it.
- plan/derive.go: `Rules.Derive` and `Snapshot`, the single derivation point (ADR 0015). `runFor`, `ApplyVerdict` and `conflictingPeerHold` are real behaviour (ADR 0004).
- plan/verbs.go: `Verbs(State)`, `Tone`, `Unattended`. These are real tables keyed by state.
- plan/launch.go, draftgate.go, push.go, generated.go, compose.go, observation.go, preview.go: each has a live prod caller (checked by grep).
- verdict: `Evaluate`, `resolve`, `allOf`, `anyOf`, `leaf`, `absentOK`, and the compat-check re-resolve. This is the real predicate grammar; the grammar tests are essential.
- spend: `Intervals`, `Fit` (least squares with an outlier pass), `Share`, `Paused`. Every one has a prod caller (store/intervals.go, web/view/chrome.go, plan/derive.go).
- tracker: `githubSource` with the `decode*` functions, `BranchSlug`, `Source`/`Resolver` (the fake seam used by loop and web tests).

Is the state model bigger than needed? It is 21 states (plan/plan.go:143-188). Every one is reachable and has a distinct `Verbs` row, except three pairs that share verbs and tone:
- `Failed` and `CutFailed` (verbs.go:67).
- `PushFailed` and `VerificationFailed` (verbs.go:77,91).
- `PRClosedUnmerged` and `BaseGone` (verbs.go:83).

Each pair differs only in its Reason string. Merging them would cut about 15 prod lines. It would also touch CSS, templates, the demo and the golden files, so risk is high and payoff small. I recommend leaving the model alone.

Two structural wastes remain:
- A verdict round-trip. `verdict.Verdict` is exploded into five `RunFact.Verdict*` bools plus `RedLeaves` (plan.go:270-276, derive.go:404-419). `statusFromPush` then re-switches on the bools (plan.go:411-420), and `VerdictLabel` (verbs.go:35-55) turns them back into strings. The comment at plan.go:265 says "this package cannot import that one", but derive.go:11 already imports verdict.
- `Entry` duplicates `Run.LogPath` (derive.go:105) and carries `Pgid`/`Elapsed` that could live on `RunFact`. This is minor.

## 2. Findings (biggest cut first)

| tag | what to cut | replacement | path:line | est. lines removed | confidence | risk |
|---|---|---|---|---|---|---|
| comments | ~190 comment lines in plan/ prod: doc comments on unexported funcs (`unlockedOnBlocker`, `statusFromRun`, `statusFromPush`, `conflictedBaseReason`, `conflictingPeerReason`, `waitingOnBlockers`, `ticketsByURL`, `prsByBranch`, `ConflictedBase`'s neighbours, `compareByRef`, `draftReason`, `runFor`, `denyMatch`, `generatedMatch`, `defaultDeny`, `unresolvedGateReason`, `closedGateReason`), the 6-line `PRMerged` naming apology, per-field "why" blocks on `RunFact`, and `docs/prds/...` pointers | Delete. Keep licence-free, contract-only 1-3 line docs on exported `Unlocked`, `Status`, `Derive`, `Preview`, `PushRefused`, `Verbs`, `Tone`. Package doc stays. | plan/plan.go:92-137,155-188,244-303,321-455; derive.go:15-18,163,237-323,325,377,431; push.go:9-19,35,53,79,92; draftgate.go:3-6,22,28,50-60; preview.go; generated.go:29; launch.go; observation.go | 190 | high | none (comments only) |
| yagni/shrink | Tests layered under `Derive`. ADR 0015 says "gates are tested through Derive as table tests, not layered under it". Yet launch_test.go (115) re-tests `LaunchPlan` beside snapshot_launch_test.go (143), preview_test.go (93) re-tests `Preview` beside snapshot_preview_test.go (131), and prospective_base_test.go (64) tests an internal helper. draftgate_test.go has 7 tests (182 lines); 4 small tests (66-127) fold into the `TestDraftGate` table. Unexport `LaunchPlan`/`LaunchCandidate`/`Preview` (no prod callers outside plan) | Drop launch_test.go and preview_test.go. Keep one table for each through `Derive`. Fold the draftgate singles into the table. | plan/launch_test.go:1-115; plan/preview_test.go:10-93; plan/prospective_base_test.go; plan/draftgate_test.go:66-127 | 280 (test) | medium | coverage regression if snapshot tests lack a case. Diff the case lists first. |
| shrink | Real-predicate fixtures: `TestEvaluateSupportApp`, `TestEvaluateServices` (verdict_test.go:96-269, about 175 lines) transcribe two production predicates as scenario tests, duplicating `TestEvaluateGrammar` (270-315) | Keep the grammar table plus one realistic any_of/all_of nest. Fold the rest into table rows. | verdict/verdict_test.go:49-269 | 150 (test) | medium | low |
| yagni | Five `RunFact.Verdict*` bools exist only because plan was once forbidden to import verdict. Replace with a `Verdict verdict.Verdict` field (CIFailed is `NeedsYou && len(RedLeaves)>0`). `ApplyVerdict` becomes `fact.Verdict, fact.RedLeaves, fact.VerdictReason = result...` (its 14-line switch goes). `VerdictLabel` becomes `fact.Verdict.String()` (18 lines go). `statusFromPush` switches on one enum. | single enum field | plan/plan.go:265-276,411-420; derive.go:404-420; verbs.go:32-55; callers loop/verdict_transitions.go:56, web/view/board.go:192, candidates.go:92 | 40 prod, +20 test churn | medium | medium: touches 3 callers and the status tests |
| idiom/shrink | Tautological String tests: `TestStateString` (status_test.go:63-92), `TestPreviewLabelString` (preview_test.go:87-93), `TestVerdictString` (verdict_test.go:409-423). They restate the switch table. Wire strings are already pinned by golden files and `TestStateDecisions` | delete | listed | 52 (test) | high | none |
| delete | `verdict.Verdict.String` has no prod caller. Only its own test uses it, and `VerdictLabel` hard-codes the same strings. | delete (or keep and use in `VerdictLabel`, see above) | verdict/verdict.go:41-52 | 12 prod | high | none |
| delete | `tracker.IssueBody` has zero callers anywhere. | delete | tracker/github.go:69-77 | 9 prod | high | none |
| delete | `tracker.BranchNumber` is used only by its own test, while plan/derive.go:298 hand-rolls an identical `branchNumber`. | delete the tracker copy and `TestBranchNumber` (slug_test.go:41-68). Do not import it into plan: plan must stay import-pure. | tracker/slug.go:14-26; slug_test.go:41-68 | 13 prod + 28 test | high | none |
| comments | tests: ~100 comment lines across plan/verdict/spend/tracker tests; most are scenario narration before `Test*` funcs (for example fit_test.go:12-15, verdict_test.go helper docs, draftgate_test.go:149) | delete | various | 60 (test) | high | none |
| comments | verdict prod: 67 comment lines, about 45 removable (all unexported docs: `evaluate`, `waited`, `resolve`, `allOf`, `anyOf`, `leaf`, `leafResult`, `requireConclusion`, `absentOK`, `author`, `triState`). Also `Verdict` doc, "Phase 1" and `StackedBase` field notes. | delete | verdict/verdict.go:63-75,81-118,126-330 | 45 | high | none |
| comments | tracker (about 15) and spend (about 15) prod comments on unexported or restating items: `githubSource`, `rawLabel`, `rawIssue`, `ticketStatus`, `rawDependency`, `decodeBlockedBy`, `Weigher`, the `Intervals` inline note, and the `Interval` doc (restates fields) | delete | tracker/github.go; tracker/tracker.go:12-33; spend/interval.go; spend/weigh.go; spend/share.go | 30 | high | none |
| yagni | `spend.Weigher` is a func type with an error return. Its only caller wraps an infallible `SumWeight` in a closure (store/intervals.go:63). | `Intervals(readings, previous, requests)` calling `SumWeight` directly. Drops the type, the `error` plumbing and `weigh.go`'s separate hop. | spend/interval.go:21-30,48-57; store/intervals.go:63-64 | 15 prod + 20 test | medium | low |
| yagni | tracker layering: `Kind` has one value; `New` is a switch over it; `Resolver` and `ForRemote` add a third level (tracker.go:12-84). Only github exists. | Keep `Source` and `Resolver` as the fake seam. Drop `Kind` and the switch: `New(remote)`. Drop `TestNewRejectsUnknownKind` (new_test.go:24-31). | tracker/tracker.go:12-19,39-52; new_test.go | 20 prod + 12 test | medium | medium: `config.Tracker` field and 3 callers |
| shrink | Two hand-rolled `/**` glob matchers: `denyMatch` (push.go:57-69) and `generatedMatch` (generated.go:33-46). Both implement the dir-prefix rule. | one shared `globMatch` | plan/push.go:57; plan/generated.go:33 | 12 prod | medium | low (different leaf semantics: Base vs full-path `filepath.Match`) |
| shrink | Duplicated base-resolution. The `base := unlock.BaseBranch; if "" ProspectiveBase(...)` block appears at derive.go:189-192 and inside `ConflictedBase` at derive.go:241-244. `ProspectiveBase` re-filters same-repo blockers that `Unlocked` already filtered (preview.go:84-97 vs plan.go:71-80). | Compute `base` once in `Derive` and pass it to `ConflictedBase`. | plan/derive.go:189,241; preview.go:84 | 10 prod | medium | low |
| yagni | `LaunchPlan` and `LaunchCandidate` are exported, but only `Snapshot.Launch*` calls them. `snap.launch` is a materialised slice rebuilt on every Derive. | unexport, or inline into `LaunchAfter` over `Entries` | plan/launch.go; derive.go:124,206 | 10 prod | low | low |
| delete | `Facts.Ticket` and `Facts.Now` are never read by `Status` (grep: no `f.Ticket`/`f.Now`). Setters at derive.go:198-199. Also a redundant `case Blocked:` before `default` in `State.String`, and `case OutcomeFailed` duplicated in `Outcome.String`'s default. | delete | plan/plan.go:305-319,237-240; disposition.go:21-24; derive.go:198-199 | 8 prod | high | none |
| idiom | Stale/incorrect comment: `PRBody` "Unreachable in Phase 1 ... wired now" (push.go:92-95) is false, since loop/push.go:226 calls it. The RunFact comment at plan.go:265 ("this package cannot import that one") is false (derive.go:11). | delete (counted under comments) | push.go:92; plan.go:265 | 0 | high | none |
| yagni (low) | Merge the state pairs listed in section 1 (CutFailed into Failed, VerificationFailed into PushFailed, BaseGone into PRClosedUnmerged). | n/a | plan/plan.go:149,179,160 | 15 prod + test/template churn | low | high |

## 3. Comment violations summary

| pkg | prod comment lines | removable | test comment lines | removable |
|---|---|---|---|---|
| plan | 291 | about 190 | 45 | about 30 |
| verdict | 67 | about 45 | 40 | about 20 |
| tracker | 32 | about 15 | 6 | about 3 |
| spend | 28 | about 15 | 13 | about 7 |
| total | 418 | about 265 | 104 | about 60 |

The main violation classes:
- Doc comments on unexported identifiers: about 40 across the packages.
- "Why"/PRD-citation blocks (`docs/prds/...`, "issue #110", "inv. 14") that justify the code's own logic.
- Confession-style naming apologies (plan.go:155-157, disposition.go:3-6).
- Two factually stale comments (see the last rows of the findings table).
- `docs/prds/prd-command-centre.md` is cited in comments about 15 times. Per the comments rule these belong in the design doc, not the source.

## 4. Subtotals (est. lines removable)

| | prod | test |
|---|---|---|
| High-confidence (comments, unused code, tautological tests) | about 290 (comments 265 incl. stale ones, IssueBody 9, BranchNumber 13, Verdict.String 12, Facts/dead cases 8; some overlap, net about 290) | about 150 (comments 60, String tests 52, BranchNumber test 28, misc) |
| All findings | about 390 | about 650 (adds layered plan tests 280, verdict fixtures 150, spend/tracker 32, enum churn) |

Together that is about 8% of the ~6.2k lines in scope; the all-findings total is about 1,040 lines, about 17%. The logic is largely essential. The savings are comments and test layering.
