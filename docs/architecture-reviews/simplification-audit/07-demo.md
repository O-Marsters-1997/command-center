# 07 Demo mode audit (read-only)

## 1. Footprint inventory
| path | lines | note |
|---|---|---|
| internal/demo/sim.go | 376 | Sim: loop+server wiring, expect checking, landMain/landPushes |
| internal/demo/player.go | 321 | wall-clock playback, pause/jump/speed/restart, dev-strip HTTP |
| internal/demo/forge.go | 276 | in-memory gh.Forge (PRs, CI, merge) |
| internal/demo/agent.go | 219 | fake runner.Runner writing stream-json |
| internal/demo/scenario.go | 204 | TOML schema + loader |
| internal/demo/sandbox.go | 195 | bare origins, squash merge, throwaway PG |
| internal/demo/worktrees.go | 109 | git.Worktrees over real git |
| internal/demo/clock.go, git.go, tracker.go, world.go, forge_control.go, press.go | 58+51+47+46+45+29 = 276 | |
| internal/demo/devstrip.tmpl | 40 | |
| internal/demo/*_test.go | 354 | player_test 202, scenarios_test 152 |
| demo/scenarios/*.toml | 610 | showcase 242, conflicts 133, happy 95, ci 72, failures 68 |
| cmd/cc/register_demo.go | 64 | `-tags=demo` subcommand |
| cmd/cc/main.go:27-28,55 | ~4 | demoSubcmd hook |
| justfile:96-97 | 2 | `just demo` |
| docs/plans/demo-mode.md | 283 | plan doc |
| gh/gh.go, internal/git/worktrees.go, loop.go SetWorktrees, app.go WithWorktrees | ~60-70 (commit 3124701 touched gh/plan/tp ~56 changed lines) | seams; SetForge, SetTrackerSource, SetBoardPollSeconds, Clock are also used by real tests/app so they stay |

Totals: demo/ prod ~1,976 + tests 354 + tmpl 40 = ~2,370. Scenarios 610. Outside-demo code ~70 + 4 + 2 + 64 = ~140 (register file included). Plan doc 283.
Grand total ~3,400 lines (~2,500 Go+tmpl, ~610 TOML, ~283 doc). No CSS/JS/web/src/internal/web template/route hooks: the strip is injected by Player.withStrip middleware (player.go:295), so the product's web package carries nothing demo-specific except doc comments.

## 2. Options
A. Delete entirely: -3,400 (Go 2,510 incl. register/hooks, scenarios 610, plan 283). Keep justfile-free. Possibly keep Worktrees seam (see risk).
B. Shrink to minimal reusing existing fakes: realistic target ~400-600 lines kept, -1,900 to -2,100 Go plus scenarios cut from 610 to ~150 (one showcase). Caveat: the existing fakes are NOT importable. loop fakeForge (internal/loop/forge_test.go, 140) and fakeTrackerSource are _test.go types; e2e/fakegh (96) and e2e/faketp (224) are PATH-shim binaries that fake `gh`/`tp` CLIs, and internal/cctest (107) is only the throwaway-PG helper (demo already uses it). The only true reuse path is: run the real binary with the e2e shims on PATH (fakegh + faketp + e2e/register agent), driven by an e2e-style testdata config. That deletes demo's Forge (276), Agent (219), Worktrees (109), tracker (47), world, press, forge_control, clock (keep a ~15 line speed clock) and Sim/Player control layers. Cost: scenario scripting power drops (no CI timelines, no dev strip), so showcase of every state is lost; a static seeded-DB board (insert tickets in each state via store, serve real web.Server, ~150 lines) is the cheapest honest "showcase".
C. Keep but simplify: -500 to -700. Cut Player pause/jump/restart/speed/MergeNow + devstrip (player.go 321 -> ~100, devstrip 40 -> 0, player_test 202 -> ~60), drop scenario `[[expect]]` machinery or the scenario-as-test duality (scenarios_test 152, sim.go checking ~120), merge clock.go/press.go/forge_control.go/git.go/world.go into their owners, trim scenarios to showcase + happy.

## 3. Findings
| tag | what to cut | replacement | path:line | est. lines | confidence | risk |
|---|---|---|---|---|---|---|
| delete: | whole package + register + scenarios + plan | nothing; board polish uses real runs or seeded DB | internal/demo/, cmd/cc/register_demo.go, demo/scenarios/, docs/plans/demo-mode.md | 3,400 | high | loses UX iteration surface; only user is dev |
| yagni: | Player playback controls (pause/resume/jumpTo/restart/setSpeed/MergeNow) and dev strip | fixed `--speed` flag, run to completion | internal/demo/player.go:92-135,163-262; devstrip.tmpl | ~330 incl. test share | high | none to product |
| shrink: | fake Forge + Agent + Worktrees + tracker (651 lines) | e2e fakegh/faketp shims + e2e agent via PATH | internal/demo/forge.go, agent.go, worktrees.go, tracker.go | ~650 | medium | scripted CI/merge timing lost; shims are binaries not Go |
| yagni: | scenarios-as-tests: `[[expect]]` checkpoints, Transition recording, mismatches | existing e2e testscripts already assert states | internal/demo/sim.go (checked/landed/pushed/pressed/transitions fields :52-59), scenarios_test.go:1-152 | ~300 | medium | demo regressions caught only by eye |
| yagni: | 4 of 5 scenarios (ci, conflicts, failures, happy duplicate showcase states) | keep showcase.toml only | demo/scenarios/ci.toml, conflicts.toml, failures.toml, happy.toml | 368 | high | none |
| delete: | sandbox real-git origins + squash merge + failure scripts | static seeded store, no git | internal/demo/sandbox.go:19-195, worktrees.go:54-109 | ~300 | medium | conflict/refresh paths no longer honest |
| shrink: | tiny files: press.go 29, forge_control.go 45, git.go 51, world.go 46, clock.go 58 fold into owners | inline | internal/demo/*.go | ~60 overhead | low | none |
| idiom: | Player holds two mutexes + wake chan for what one struct+context could do once controls go | single goroutine loop | internal/demo/player.go:50-59 | ~40 | medium | none |
| delete: | seam only demo uses if demo deleted: WithWorktrees/SetWorktrees/git.Worktrees interface (loop tests may use; verify first) | direct git.CLI | internal/app/app.go:96, internal/loop/loop.go:92-94, internal/git/worktrees.go | ~25 | low | tests may rely |
| comments: | see §4 | | | ~20 | high | none |

## 4. Comment violations
- Plan-style comments naming demo in non-demo code: internal/loop/loop.go:89,92; internal/loop/clock.go:5; internal/web/server.go:120; internal/gh/gh.go:122; internal/git/worktrees.go:10. All refer to "the demo" as a reason for an exported seam. Contract is fine; the "demo" clause is a pointer to a non-product consumer and goes if demo goes.
- demo/: unexported types/funcs with doc comments (Sim, Forge, Agent, agent.Step, Worktrees.New cut scripts, Advance) exceed the rule that only exported identifiers earn docs; the package is not public API. Several run past 3 lines (forge.go:116-117, agent.go:22-24, sim.go:39-40 fine at 2-3). Estimate ~15-20 comment lines removable; none judged confessions.
- docs/plans/demo-mode.md duplicates the design in prose; deletable with the package.

## 5. Subtotals (removable lines)
- A delete entirely: ~3,400 (Go+tmpl ~2,510, TOML 610, doc 283); +~25 optional seam removal.
- B shrink with existing fakes/static seeded board: ~2,700-2,900 (keep ~500 Go + 1-2 scenarios or static seed).
- C keep but simplify: ~1,000-1,200 (player controls+strip ~330, extra scenarios 368, expect/test machinery ~300, file folding/comments ~100).
Cost of deletion: near zero for the product (no web/CSS/JS/template coupling; one build-tagged file and a 4-line hook in cmd/cc/main.go), CI unaffected (demo tests need `-tags demo`, check .github not referencing it: grep shows none). Only loss is the dev UX-iteration surface the plan was written for.
