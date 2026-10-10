# New UI gap audit: the Signal session-rail prototype against Command Centre

**Date:** 2026-10-09 · **Prototype:** `command-center-design/index.html` (read-only reference) ·
**Handoff:** `.claude/handoffs/command-center-design__to__command-center__new-ui-audit-and-migration.md`

Each row of the prototype's capability inventory, checked against `main` at `6f3067f`. Status is one
of **exists**, **partial**, **missing** or **conflicts** (exists or is buildable, but an accepted
plan, PRD or ADR says otherwise). Paths are relative to the repo root unless marked _(proto)_.

Only the `acme` billing feature in the prototype comes from real fixtures. The other projects, run
logs, insights numbers, extra repos and the "ready" tickets #13, #46 and #92 are illustrative.

## Summary

| # | Capability | Status | Blocks migration? | Size |
|---|---|---|---|---|
| 1 | Projects above repos | missing, conflicts | no | M |
| 2 | Session rail (Needs you / In flight / Settled) | partial | **yes** (it is the shell) | S–M |
| 3 | Settle, auto-settle, Undo | partial | no | M |
| 4 | Rail search / ⌘K | partial, conflicts | no | S–L |
| 5 | Session view header, prompt, "worked for" | partial | no | S–M |
| 6 | Typed run-log transcript + Raw log | partial | no | M |
| 7 | Composer to steer a running agent | missing | no | L |
| 8 | "Merging this unlocks #x", blocked-by | partial | no | S |
| 9 | Feature view: graph waves, board by attention | partial | no | S–M |
| 10 | Launch dialog (context, pickers, Undo) | partial | no | S–L |
| 11 | Armed tickets, per-ticket toggle | partial, conflicts | no | S–M |
| 12 | "Ready to launch" state | **exists** | — | — |
| 13 | All tickets, cross-project, filter tabs | partial | no | S |
| 14 | Insights: limits with resets, split table | partial | no | S |
| 15 | Repos: grouped, four checks, live progress | partial | no | S–M |
| 16 | Repo page: banner, checklist, tickets | partial | no | S |
| 17 | Verbs with Undo toast | partial | no | S–M |
| 18 | Keyboard model | missing, conflicts | no | M |
| 19 | Deep-linkable routes | partial | **yes** (route map) | S–M |
| 20 | Phone layout | missing, conflicts | no (in scope per page) | M |
| 21 | Signal design system | conflicts | **yes** | M–L |
| 22 | Awaiting-input state | missing | no | L |

Three rows must be settled before any migration slice can start: the shell (2), the route map (19)
and the token system (21). Everything else can follow the migration.

## Cross-cutting findings

- **Glossary collisions.**
  - `CONTEXT.md` defines **Rail** as the sidebar's collapsed icon-only state. The prototype's
    "session rail" is a different thing.
  - `CONTEXT.md` lists **project** under _Avoid_: it is the tracker's label prefix for a feature,
    and "the app has no project concept".
  - Both need resolving before code or plans use the words.
- **Accepted plans the prototype contradicts.**
  - `docs/plans/side-nav.md` § Not in this plan excludes:
    - a command palette and any keybinding scheme
    - an off-canvas mobile drawer ("there is no phone that can reach `127.0.0.1`")
    - a repos destination
  - `docs/prds/prd-fleet-view.md` § Out of scope defers the project rail until "a second repo, or
    when the board stops fitting on a screen". With tracked repos, the second-repo trigger has fired.
- **Stale plan references.** `docs/plans/frontend-pass.md` cites
  `docs/adr/0007-utilities-for-layout-grammar-for-state.md`, which does not exist (ADR 1 is the
  record), and `internal/cc/` paths that are now `internal/web/`.
- **State fan-in.** `plan.State` has 22 values (`internal/plan/plan.go:130-152`). They collapse to
  five tones (`internal/plan/verbs.go:74-88`). Signal has eight glyphs. The glyph has to be one
  more pure function of `State`, like `Tone`, or Go would end up deciding classes.
- **Needs you ≠ tone `stop`.** `ReviewMe` tones `wait`, but the prototype puts review in Needs you.
  The rail grouping is a new derivation, not a re-use of `Tone`.

## Rows

### 1. Projects as a grouping above repos — missing, conflicts

- **Evidence:**
  - The schema has `tickets.repo` and `tickets.group_key` (feature) only
    (`internal/store/migrations/0001_init.sql`).
  - `0012_repos.sql` has no parent column, and config has no project key.
  - Repo scope is a view param: `internal/web/view/reader.go:89-140`, `view/repos.go:73-82`.
- **Overlap:** `CONTEXT.md` (project is _Avoid_; "no project concept"); ADR 7 (scope is a view
  concern); `prd-fleet-view.md` § Out of scope (re-entry trigger arguably met).
- **Needs:**
  - A definition of a project: owner prefix, config, or a stored grouping.
  - Probably a `projects` table plus a repo→project mapping (sqlc, ADR 5), and a `?project=` scope.
  - A glossary change.
- **Size:** M. Can follow the migration; the rail works grouped by feature without it.

### 2. Session rail — partial

- **Evidence:**
  - All the states the rail needs are derived: `plan.go:130-152`.
  - The unattended set (`verbs.go:65-72`) and tones (`verbs.go:74-88`) are enough to sort.
  - Features are `tickets.group_key`.
  - The board already orders by blocker group (`view/board.go:247-299`).
  - Nothing derives Needs you / In flight / Settled, and there is no per-feature rollup or
    "feature settled" flag.
- **Overlap:** ADR 15 (only plan derives, so the grouping function belongs in `plan`);
  `PRODUCT.md` principle 1.
- **Needs:**
  - A pure `plan.Attention(State)` (or similar) returning one of three words.
  - A view-model rollup per feature.
  - No schema.
- **Size:** S–M. **Blocks migration**: every page sits beside the rail.

### 3. Settle, auto-settle on merge, Undo — partial

- **Evidence:**
  - Auto-settle is effectively there: `PRMerged` (`plan.go:250-251`) offers only
    `remove-worktree` (`verbs.go:46-47`).
  - There is no stored acknowledgement, dismiss or archive for a non-merged ticket. `launches.state`
    is launch-level.
- **Overlap:** ADR 15 and ADR 11. A manual settle is a stored human fact, not a derived state, so it
  sits beside derivation as an input.
- **Needs:**
  - A `ticket_settles(url, settled_at)` table read as a rail filter, with Undo deleting the row.
  - "New activity brings it back" means comparing `settled_at` to the latest fact time.
- **Size:** M. Can follow: v1 settles merged tickets only, which is derivable.

### 4. Rail search / ⌘K — partial, conflicts

- **Evidence:**
  - Only repo search exists: `features.tmpl:36-38` → `/features/search` → `view/repos.go:122-146`.
  - There is no ticket, ref or feature search, and no ⌘K anywhere.
- **Overlap:** `side-nav.md` excludes a command palette.
- **Needs:**
  - A server-side match over the board's rows (titles, refs, repo, feature). The rows are in
    memory per render, so no index is needed.
  - A small htmx-driven palette.
- **Size:** S for search, M with the palette. Can follow.

### 5. Session view: badge, prompt bubble, "worked for" — partial

- **Evidence:**
  - "Worked for" is computed from the log at render: `view/logview.go:97`,
    `view/logreadout.go:133-145`, `detail.tmpl:32-35`.
  - Per-run totals are stored at disposition: `0004_run_metrics.sql`, `store/runs.go:46-70`.
  - The prompt is stored only as `runs.prompt_hash`. The text goes to `runs/<id>.prompt`
    (`loop/loop.go:618`) and is deleted by `pruneRunLogs` (`loop/verbs.go:234-235`).
- **Overlap:** ADR 10 (pruned logs cannot be re-parsed); `run-insights.md`.
- **Needs:**
  - Persist the prompt text (a column or `run_prompts`).
  - Take "worked for" from the DB totals when the log is gone.
  - Wall time from `started_at`/`ended_at`.
- **Size:** S–M. Can follow; the migration can render what the log has.

### 6. Typed run-log transcript + Raw log — partial

- **Evidence:**
  - Runs are `claude -p … --output-format stream-json` to `runs/<id>.jsonl`
    (`config/config.go:52-58`, `loop/loop.go:623`).
  - `internal/agentlog/parse.go` already types events: Skill/File/Tool/Fail/Pass/Say (`:20-29`),
    with run phases at skill boundaries (`:63-72`), a result (`:74-80`) and a live tail
    (`:259-279`).
  - The log is served over SSE at `GET /ticket/{ticket}/log` (`server.go:127`, `logstream.go:19-40`)
    with four `?log=` modes.
- **Mapping to `app/logs.js` _(proto)_:**

  | Proto event | Source | Status |
  |---|---|---|
  | sys | runner (worktree, baseline SHA), not the JSONL | partial |
  | phase, msg, tools, error, live | JSONL, parsed | exists |
  | edit ± | Edit/Write tool inputs, not git numbers | partial |
  | cmd exit/duration/output | `tool_result` has output and `is_error`; no exit code or duration | partial |
  | git | only inferable from a Bash `git commit` | missing |
  | pr, checks | loop observations, not the log | missing (join at render) |
  | you | nothing reaches a running agent | missing (row 7) |

- **Raw log:** the JSONL is not served raw.
- **Needs:**
  - Extend `agentlog` with cmd/edit detail.
  - Merge pr/checks/git from the store at render.
  - A small route for the raw JSONL.
- **Size:** M. Can follow; the first slice ships the events `agentlog` already types.

### 7. Composer to steer a running agent — missing

- **Evidence:**
  - `runner.Spawn` sets no `cmd.Stdin`.
  - The prompt is in argv.
  - The system prompt says "This session is single-shot … and will not resume"
    (`loop/settings.go:26`).
  - The model is hard-coded in `defaultAgentCommand` (`config/config.go:58`), and there is no effort
    setting.
  - Post-run steering exists as the `follow-up` verb with a textarea (`detail.tmpl:98-108`,
    `store/runs.go:273`).
- **Overlap:** ADR 13 (handoffs split at durable points). No plan covers steering.
- **Needs:**
  - A stdin stream-json input path in `Spawn`, plus routing to the live process.
  - A change to the single-shot system prompt.
  - Model and effort as argv substitutions (`{model}` is planned in `fleet-orchestration.md:34` but
    not built).
  - Verify the CLI's input-format and effort flags first.
- **Size:** L for live steering. S–M for model and effort pickers.
- **Cheap substitute:** the composer posts `follow-up` on settled states and is hidden while running.

### 8. "Merging this unlocks #x" and blocked-by — partial

- **Evidence:**
  - `BlockedBy` is on every row (`view/board.go:33`, filled at `:178`).
  - There is no reverse index.
- **Overlap:** ADR 9.
- **Needs:** invert `BlockedBy` once per board build into `Unlocks`. No schema.
- **Size:** S. Can follow.

### 9. Feature view: graph waves, board by attention — partial

- **Evidence:**
  - The graph is a two-column root/child layout (`web/src/layout.ts:23-43`), with click-to-toggle
    one-hop highlighting (`graph.tsx:73-90`).
  - Board and graph are sidebar links, not tabs (`layout.tmpl:97-98`).
  - The board groups by blocker chain (`view/board.go:247-299`), not attention.
- **Needs:**
  - Depth columns with wave labels. Depth is already computed in `flattenChain`.
  - A hover trace, client-only in `graph.tsx`.
  - Attention grouping reusing the row 2 function.
- **Size:** S–M. Can follow.

### 10. Launch dialog — partial

- **Evidence:**
  - One flat table with a now/on-unlock label column, per-ticket checkboxes, closure guards and a DAG
    preview (`web/src/launch-modal.tsx:59-156`, `web/src/launch.ts:22-31`).
  - Opened only by POST from a feature (`server.go:249-277`).
  - `plan.Compose(t)` takes only the ticket (`plan/compose.go:11`), so there is nowhere for extra
    context.
  - Base branch is derived (`plan.go:59`, `:77-121`).
- **Overlap:**
  - `feature-launch.md` Phases 3–4.
  - ADR 9.
  - The confirm-only-writes rule in `CONTEXT.md` § Launch modal.
- **Needs:**
  - Extra context: a launch payload field plus a `Compose` arg. This changes `prompt_hash`, so it
    must be decided before anything relies on hashes.
  - A feature picker: S.
  - Model and effort: see row 7.
  - Base override: L, because it touches conflict logic.
  - Undo: see row 17.
- **Size:** S–L. Can follow; the migration restyles the current island.

### 11. Armed tickets — partial, conflicts

- **Evidence:**
  - "On unlock" is persisted as `launch_members` under an active launch; a blocked member derives
    `Queued` (`plan.go:266`).
  - `cancel` is launch-scoped: `CancelActiveLaunches` withdraws every sibling.
  - The prototype toggles one ticket.
- **Needs:** a per-member state (or a split launch) so one ticket can be disarmed. Needs a migration.
- **Size:** M. Can follow; until then the toggle maps to "cancel the launch".

### 12. "Ready to launch" — exists

- `plan.Ready` (`plan.go:132`, derived at `:263-264`) offers `launch` and tones `idle`. It maps 1:1
  to the prototype's `s: "ready"`.

### 13. All tickets, cross-project, filter tabs — partial

- **Evidence:**
  - The unscoped board at `/` already spans every repo and feature (`server.go:121`,
    `reader.go:89`).
  - There are no state filter tabs.
- **Needs:** a `?filter=` param applied after `groupRows`, admitting groups whole (ADR 7).
- **Size:** S.

### 14. Insights — partial

- **Evidence:**
  - Chart and rolling median of 10 exist (`insights.tmpl:1-32`, `view/insightschart.go:15,70`).
  - The agent / resolve / follow-up split is computed for JSON only (`view/insights.go:127-135`).
  - Limits are masthead meters only (`masthead.tmpl:18-24`).
  - `store.Gauge.ResetsAt` is stored (`store/utilization.go:30`) but dropped by `view.Gauge`
    (`view/chrome.go:52-57`).
- **Overlap:** ADR 12.
- **Needs:**
  - Thread `ResetsAt` through.
  - Render the split as a table.
  - Select `feature` in `queries/insights.sql`.
  - No schema.
- **Size:** S.

### 15. Repos — partial

- **Evidence:**
  - Flat table (`view/repos.go:98-118`, `features.tmpl`).
  - Four checks (`view/banner.go:106-127`).
  - A refusal sentence (`banner.go:91-104`).
  - Track search via `gh` (`server.go:534`).
  - Track again (`banner.go:41-49`).
  - While cloning, all four steps show "…" at once.
- **Needs:**
  - Project grouping (row 1).
  - Per-check live progress, which needs a step column the loop writes.
- **Size:** S to restyle, M for live per-check progress.
- **Naming hazard:** `features.tmpl` is the repos page and `repo.tmpl` is the per-repo features
  page.

### 16. Repo page — partial

- **Evidence:**
  - `/features?repo=` renders the banner, checklist and a features table (`repo.tmpl:1-48`).
  - Tickets live only on `/?repo=`.
- **Overlap:** `tracked-repos.md:37-40` specifies the features table, which contradicts the
  prototype's tickets section.
- **Size:** S once the page's job is decided.

### 17. Verbs with Undo — partial

- **Evidence:**
  - Kill, re-run, resolve, refresh and remove-worktree are row verbs (`plan/verbs.go:5-60`).
  - All go through `POST /verb` (`server.go:362-395`), which queues an intent the loop consumes on a
    later tick.
  - Open PR and Open log are links, not verbs.
  - There is no undo.
- **Overlap:** ADR 14 (two-phase tick); `CONTEXT.md` § Verb, Intent.
- **Needs:**
  - A withdrawable intent. Undo deletes an unconsumed intent before the next tick.
  - Either a short hold (intents become due N seconds after creation) or accept that undo can race
    the tick and answer "too late".
- **Size:** S–M. Can follow.

### 18. Keyboard model — missing, conflicts

- **Evidence:**
  - No keydown handling in any template.
  - `graph.tsx:135-140` handles arrow keys on nodes only.
- **Overlap:**
  - `side-nav.md` excludes any keybinding scheme.
  - `frontend-pass.md` Phase 6 asks only for a keyboard path through the board.
- **Needs:** one small vanilla script in the layout, keyed off `data-` anchors in the templates. No
  island and no dependency.
- **Size:** M. Can follow.

### 19. Deep-linkable routes — partial

- **Current routes** (`internal/web/server.go:120-139`):
  - `/`, `/board`, `/graph.json`
  - `/insights`, `/insights.json`
  - `/ticket/{ticket}/log` (SSE)
  - `/features`, `/features/search`, `/features/banner`, `/features/{feature}` (303 to `/?feature=`)
  - `POST /repos/track`, `/features/{feature}/import`, `/ticket`, `/verb`, `/launch`,
    `/launch/open`
  - `GET /launch/candidates`, `/events`
- **Query params** (`view/params.go:22-44`): `sel`, `ticket`, `view`, `log`, `repo`, `feature`,
  `phase`.
- **Proto mapping:**

  | Proto route | Today |
  |---|---|
  | `#/s/<repo>#<n>` session | `?sel=` on the board (partial) |
  | `#/f/<f>/graph`, `/board` | `/?feature=X&view=graph` (partial; the 303 drops `view`) |
  | `#/f/<f>/launch`, `#/launch` | none (POST-only fragment) |
  | `#/tickets` | `/` |
  | `#/insights` | `/insights` |
  | `#/repos`, `#/repos/<o>/<n>` | `/features`, `/features?repo=` |
  | `?` shortcut sheet | none |

- **Needs:** a path-based route map (`/s/{repo}/{n}`, `/f/{feature}`, `/repos/{owner}/{name}`, …)
  with redirects from the old query-string URLs, and a GET for the launch dialog.
- **Size:** S–M. **Blocks migration**: it fixes the shape of every page slice.

### 20. Phone layout — missing, conflicts

- **Evidence:**
  - One `@media (max-width: 640px)` block (`web/app.css:748`).
  - The sidebar expands on hover (`web/app.css:787-836`).
  - There is no 900px rail rule (contrary to `CONTEXT.md` § Rail), no 44px targets and no sheets.
- **Overlap:** `side-nav.md` rules out a mobile drawer because "there is no phone that can reach
  `127.0.0.1`". That reasoning is stale: the `Caddyfile` and `internal/auth` (`docs/deployment.md`,
  issue #294) already put the daemon behind TLS and a login.
- **Needs:** responsive templates only.
- **Size:** M.

### 21. Signal design system — conflicts

- **Evidence:**
  - `web/app.css:1-58` `@theme`: `--color-*` tokens with tones `s-done/live/wait/stop/idle` and a
    `[data-theme="dark"]` block (`:33-56`).
  - The theme toggle is in `layout.tmpl:9-17,62`.
  - Signal (`design-system/tokens.css`) is plain custom properties, light only.
  - It imports Inter and JetBrains Mono from Google Fonts. The repo loads no remote font.
  - Its state tokens are named by state, not by tone.
  - None of the protected grammar classes (pill family, ribbon, meter, segbar, banner, flag, line-*,
    `[data-tone]`, `data-depth`) has a Signal equivalent. The pill becomes the status glyph.
- **Overlap:** ADR 1 (named grammar, one sheet); `CLAUDE.md` (no new dependency, colour only as
  `@theme` oklch tokens with a dark entry).
- **Needs:**
  - Port Signal's values into `@theme` as oklch tokens, and self-host the fonts.
  - Decide what happens to dark mode.
  - Re-express the grammar as Signal recipes under the same named classes, adding a glyph class
    family keyed by a fixed set of words.
  - Update `pill_test.go`'s contract deliberately, not by accident.
- **Size:** M–L. **Blocks migration.**

### 22. Awaiting-input state — missing

- **Evidence:**
  - Runs are non-interactive with stdin unset.
  - No session id is stored (`0001_init.sql:34-46`), so `--resume` is impossible.
  - The nearest thing is `NeedsYou` (`plan.go:139`, `verdict/verdict.go:29`): the run ends and parks
    for a human.
- **Overlap:** ADR 13; `PRODUCT.md` open question.
- **Needs (full):**
  1. Capture the session id from stream-json.
  2. Store it on the run.
  3. Add a state.
  4. Resume the spawn with the answer.
- **Needs (cheap):** a question parks as `NeedsYou` and the answer is a follow-up run.
- **Size:** L (full) or S (cheap). Product decision first.

## Data the prototype expects with no backing

From `app/data.js` _(proto)_ against `view.Row` (`internal/web/view/board.go:19-76`):

- `project` on features and tickets
- a feature `launched` time and `settled` flag
- `updated` as a relative age per ticket
- `last`, a one-line latest activity
- `diff` as PR adds and removes
- `turns` on the row
- `armed` per ticket

Backed already:
- ref, repo, title, state, reason, PR, checks
- `blockedBy`, verbs, spend, elapsed, feature

`/launch/candidates` returns exactly the 11 keys of `data/candidates.json` _(proto)_, so the
prototype's launch fixture is a valid contract sample. It is not realistic for refused rows or base
verdicts.
