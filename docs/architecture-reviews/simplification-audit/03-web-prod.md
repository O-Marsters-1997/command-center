# 03 Web layer production code audit (read-only)

Slice sizes: internal/web/*.go 1.4k, view/*.go 2k, 12 templates ~540, web/src 1.2k (incl. 3 test files ~280), app.css 509.

## 1. Essential core
- Go html/template board: `GET /`, `GET /board` (htmx poll, `id="board"`, `hx-preserve` detail row), view.Reader deriving rows from plan.Snapshot (deriveRows, groupRows, scope filters, band, chrome).
- Write routes that only queue intents: `POST /verb`, `POST /launch`, `POST /launch/open`, `POST /features/{f}/import`.
- Launch flow: graph island (`graph.tsx` + `/graph.json`), launch-modal island (`launch-modal.tsx`, `launch.ts`, `/launch/candidates` JSON).
- Run log: `GET /ticket/{t}/log` SSE + logview + logline template; context-curve SVG.
- Features page, masthead gauges, 4-card band (documented in design doc, keep).
- CSS: `@theme` tokens, named state grammar (pill/ribbon/meter/segbar/banner/flag/line/data-tone), data-depth rules, sidebar, repo-switcher.
- Build: tailwind CLI + vite lib build, 6 devDeps, 2 runtime deps. No dependency cuts available (every devDep used).
- `internal/cc/`: CONFIRMED dead. Untracked dir (git status `??`), 48K, only `assets/dist/{graph,insights,launch-modal}.js` + chunks, zero references anywhere (every "internal/cc" grep hit is `internal/cctest` or a prose/fixture string). Delete.

## 2. Findings (biggest cut first)

| tag | what to cut | replacement | path:line | est. lines removed | confidence | risk |
|---|---|---|---|---|---|---|
| delete | `internal/cc/` untracked stale dist dir (5 built JS files, ~48K) | `rm -rf internal/cc` | internal/cc/ | 0 source lines (48K junk) | high | none |
| comments | ~220 of ~311 Go comment lines in web prod (server.go 75, board.go 70, chrome.go 32, reader.go 26, logview 23, params 22, insights 16, logstream 14) are design-doc cross-refs, "why", invariant citations, field docs on unexported/JSON fields | keep ≤3-line doc on exported identifiers only; delete rest | server.go, view/board.go:20-110, view/chrome.go:10-45, view/reader.go, view/logview.go, view/params.go | 220 | high | none |
| shrink | 39 repeated `if err != nil { http.Error(w, err.Error(), 500); return }` blocks | `type handler func(w,r) error` adapter (or `fail(w,err)` helper; status via `view.IsInvalid`/typed err) | server.go:176-540, confirm.go, insights.go, logstream.go | 70 | high | low |
| stdlib | 12 `//go:embed x.tmpl` + 12 `var xSource string` + `template.Must(page.New(..).Parse(..))` registrations and 4 "blank identifier is deliberate" comments (server.go:20-112, confirm.go:11-14, insights.go:11-14, logline.go:11-17) | one `//go:embed *.tmpl` `embed.FS` + `template.Must(template.New("").Funcs(fm).ParseFS(fs,"*.tmpl"))`, `ExecuteTemplate(w,"page.tmpl",..)`; launch modal joins same set | server.go:20-112 | 85 | high | low (golden unaffected; rename inner `template` calls to file names or keep `define`s) |
| delete | `POST /ticket` handleTicket + ticketByURL: no template, TS, README, CONTEXT, design or ADR caller; only `server_ticket_test.go` hits it. Speculative edit-branch/blocked_by API | delete handler+route; then also store.QueueEditTicketIntent, loop.applyEditTicketIntents, ticket_edit.sql (outside slice, ~100 more) and server_ticket_test.go (~200) | server.go:168, 450-515 (handleTicket), 523-530 (ticketByURL) | 60 (+~300 outside slice) | medium-high | low; confirm with owner no skill/script curls it |
| TS dead | `charts.tsx` Sparkline, BarChart, Histogram, sparkPoints, histogramBars, areaPath, linePath, Bar/Point/ChartDims types: only `niceTicks` and `rollingMedian` are imported (insights.tsx:3); grep confirms zero other users | keep the two functions (~35 lines) | web/src/charts.tsx:35-172 | 135 (+~40 in charts.test.ts) | high | none |
| native | `GET /confirm` page + `destructiveVerbs` map + `confirmView` + confirm.tmpl (3 sources for one "are you sure") | `hx-confirm` / `onsubmit="return confirm('kill ... cannot be undone')"` on the two destructive row forms; effect text inlined in board.tmpl | confirm.go:1-79, confirm.tmpl, server.go:164, server.go:62 (`destructive` func), board.tmpl:80-84 | 95 | medium | medium (loses pgid/worktree "at risk" line; product call) |
| structural | Third Solid island `insights.tsx` + vite entry + `/insights.json` handler contradicts CLAUDE.md ("two islands"). Context curve is already Go-rendered SVG, and Go already has a second copy of niceTicks (contextcurve.go:114-142 "ported" from charts.tsx) | render insights scatter server-side with the same pattern as contextcurve (one niceTicks, one rolling median) | web/src/insights.tsx (121), web/src/charts.tsx, vite.config.ts:14, insights.go:26-32, insights.tmpl | ~150 net | medium-low | medium (ADR 12 names /insights.json; tests) |
| TS | `types.ts` mirrors ~45 Row fields; graph.tsx reads 7 (url,title,state,tone,unattended,alive,blocking). Check/LogDetail/LogPhase/LogFilterLink interfaces (30 lines) unused | trim to the 7 fields + Group; (also `/graph.json` marshals the whole Row incl. Log phases; fine once Go side tagged `json:"-"` for the rest) | web/src/types.ts:1-81 | 60 | high | none |
| TS | launch-modal's mini-DAG preview duplicates the table beneath it: dagNodes/dagEdges/dagBounds + SVG + `columnsFor` + `stackByColumn` | table only; `layout.ts` then serves graph alone (fold `layoutGroups/edgePath/edgesFor/bounds` into graph.tsx, delete layout.ts's shared-by-two-islands generics) | launch-modal.tsx:79-82,100-122; launch.ts:46-62; layout.ts:50-60 | 50 | medium | medium (visual feature; layout.test/launch.test update) |
| shrink | `handleCandidates` HX-Request branch re-implements `handleLaunchOpen` (feature modal vs ticket modal, 25 lines each) | extract `s.modalFor(ctx, feature, tickets)`; both handlers call it | server.go:233-262 vs 195-231 | 25 | high | low |
| css | Dead rules: `.spark-line/.spark-area/.spark-point` (13), `.chart-bar/.chart-bar-out/.chart-unsettled` (18, last two have no user anywhere), the `background:` duplicate in every chart-* rule (6), `.line-skill,.line-file,...{--tone}` (8: `.line` never reads `--tone`) | delete | web/app.css:299-390 (spark..chart-series), 395-401 | 45 | high | low (rebuild dist/app.css and commit) |
| css | pill disc/ring/pulse implemented twice (bare dot + `.pill.pill-x::before`), 70 lines | single `::before` dot plus one modifier set; bare `.pill-disc` spans in band/masthead become `.pill` children via one rule | web/app.css:185-262 | 20 | low | medium (golden files, visual) |
| comments | TS comments 58 lines (graph 11, launch.ts 14, layout.ts 13, launch-modal 7, charts 5) and the repeated 2-line "solid-element inserts into the element" note x3; CSS 9; vite.config 3; HTML comments 22 (page.tmpl 6, board.tmpl 6, launch_modal 4, insights 3, layout JS 2) | delete (none cite an outside constraint worth keeping beyond `getCurrentElement().textContent=""` quirk which can stay as 1 line) | graph.tsx:21-24,42-44,56-59,111-115,126-127; launch.ts all; layout.ts:1-2,5-7,16,31-33,40-41,76-79; page.tmpl:11-13,21-23; board.tmpl:97-101; launch_modal.tmpl:9-12; insights.tmpl:5-8; app.css:226-228,300,330-340 | 90 | high | none |
| shrink | Four page templates repeat the 9-line docHead/skipLink/topbar/masthead/shell/sidebar scaffold (page, features, confirm, insights) | one `shell` layout cloned per page with `{{template "content" .}}` | page.tmpl:1-9, features.tmpl:1-8, confirm.tmpl:1-8, insights.tmpl:1-8 | 25 | medium | low |
| shrink | `rowSlot`, `newRowSlot`, `head`, `child` funcs only to smuggle verb-name constants + scopes into `{{template "row"}}` | compare against literal `"launch"`/`"follow-up"`/`"cancel"` strings in board.tmpl and pass a small dict via `dict`-less `.` (Row already carries Scope fields if view sets them) | server.go:88-112 (+ FuncMap 25-38), board.tmpl:9-12,23,78-79,89 | 20 | medium-low | medium |
| yagni | `handleFeatureRedirect` (+route) exists so features.tmpl can link `/features/{f}`; `handleStylesheet` (+route) exists to alias `/assets/app.css` to `/assets/dist/app.css` | link to `/?feature={{.Feature | urlquery}}`; reference `/assets/dist/app.css` in docHead | server.go:156-157,159,198-200,494-500; features.tmpl:22; layout.tmpl:8 | 17 | high | low (golden href change) |
| delete | `/events` audit dump: no UI caller; only README.md:169 | keep only if owner uses it for debugging; else drop with `store.Events` | server.go:163,226-233 | 10 (+store) | low | medium (documented feature) |
| delete | `spendEntry.reads` written, never read; `Params.withFeature` called only by its own test; `Server.rawMux` field exists only for export_test.go:59; `Server.SetBoardPollSeconds/SetSpendLimit5h` pure pass-through to Reader | delete; pass both as NewServer/NewReader args | view/spend.go:22,38; view/params.go:128-132; server.go:135,169; server.go:187-191 | 15 | high | low |
| idiom | `insightsTimezone` + `systemTimezoneName` (reads TZ / readlink /etc/localtime to give Postgres an IANA name) | store query grouping done in Go with `time.Local`, or `time.Now().Location().String()` and accept "Local"->UTC | view/insights.go:46-84 | 28 | low | medium |
| native | inline `ago()` 1s ticker + `<time datetime>` machinery in docHead (16 lines JS) and `Age.Stamp` plumbing; board/masthead already re-render every 5s | drop; server text refreshes with poll | layout.tmpl:18-34, view/chrome.go (Age.Stamp), view/reader.go:248-254, masthead.tmpl:1 | 25 | low | low (features/insights pages don't poll, so ages go stale there) |
| TS | `ticketRef(url)` copied in graph.tsx:34 and launch-modal.tsx:15 although server already sends `Candidate.ref` | use `c.ref` (modal) and one shared fn | graph.tsx:34-37, launch-modal.tsx:15-18 | 6 | high | none |
| idiom | `Params.withLog/withRepo/withFeature` three identical copy-and-set methods; `toggleSel`/`toggleTicket` similar; whole Params type is a query-string DSL for 6 params | keep but collapse after deleting withFeature; no framework needed | view/params.go:108-150 | (counted above) | n/a | n/a |

## 3. Comment violations summary per file
Go (comment-only lines, approx; most fail the test: they restate design-doc sections, justify code, or document unexported/JSON fields):
- server.go 75 (keep ~12: NewServer, Server, Clock, Set* exported contracts, trimmed)
- view/board.go 70 (keep ~6 on exported Row/Group/Board/PercentOf)
- view/chrome.go 32 (keep ~6; every field comment on an exported struct cites CC-3xx tickets / CONTEXT.md sections)
- view/reader.go 26, view/logview.go 23, view/params.go 22, view/insights.go 16, logstream.go 14, candidates.go 9, logline.go 9, features.go 5, contextcurve.go 4, others 1-2
- "The blank identifier is deliberate, not dead code" x4 (server.go:36,50,57,69,92): confessions, disappear with ParseFS change.
- Confessions: logstream.go `ponytail:` marker comment (user rule says markers get no exemption unless it names an outside constraint: it does not), server.go:108 rowSlot paragraph explaining html/template `$`.
TS: graph.tsx 11, launch.ts 14, layout.ts 13, launch-modal.tsx 7, charts.tsx 5, types.ts 3, insights.tsx 2, vite.config.ts 3. `// biome-ignore ... useSemanticElements` (launch-modal.tsx:21) is a suppression with reason "byte-for-byte match"; stylistic-rule suppression, acceptable but fixable by using `<output>` or dropping the match claim.
CSS: app.css 3 blocks (~9 lines) at 226-228 area (`cc-graph` note), repo-switcher x2. Third explains popover top layer (browser constraint: arguably permitted, keep 1 line).
Templates: HTML comments in page.tmpl (2 blocks), board.tmpl (1), launch_modal.tmpl (1), insights.tmpl (1), layout.tmpl JS (2 `//` lines). The `hx-preserve` comment in board.tmpl is a genuine htmx constraint: keep as 2 lines.
Total comment lines removable: Go ~220, TS ~45, CSS ~8, templates ~18.

## 4. Subtotals (lines removable, production code only; test files excluded unless noted)

High-confidence:
- Go: comments 220 + err adapter 70 + template wiring 85 + modal dedup 25 + redirect/stylesheet 17 + dead fields 15 = ~430
- Templates: HTML comments ~18 + link tweaks ~2 = ~20
- TS: charts.tsx dead 135 + types.ts 60 + comments 45 + ticketRef 6 = ~245 (plus ~40 test lines in charts.test.ts)
- CSS: dead rules 45 + comments 8 = ~53
- High total: ~750 lines (plus `internal/cc/` stale dir, 48K, 5 files)

All (adds medium/low items):
- Go: +POST /ticket 60, +confirm.go 79, +rowSlot 20, +insights tz 28, +/events 10, +insights island Go delta (counted in TS) = ~630
- Templates: +confirm.tmpl 20, +shell dedupe 25, +ago() JS 17 = ~80
- TS: +mini-DAG 50, +insights island swap ~150 (net, includes added Go), +ago plumbing = ~445
- CSS: +pill dedupe 20 = ~75
- All total: ~1,230 lines of ~5.5k in slice (~22%), before counting ~300 lines outside slice behind POST /ticket and ~280 lines of tests that go with the cuts.

Net judgement: the web layer is not structurally huge; the bloat is (1) comment volume, (2) dead chart code from a Sparkline/Histogram library nobody uses, (3) template registration ceremony, (4) per-handler error boilerplate, (5) one speculative write endpoint. Any single bigger win requires dropping a feature (confirm page, mini-DAG, third island).
