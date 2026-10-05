# Plan: ticket spend

> Source: the grilling session of 2026-09-27, and ADRs 17, 18 and 19. Diagram:
> `docs/designs/fleet-observability.excalidraw`.

The goal is one chart: spend per merged ticket over time, in % of the weekly window, so that a
change to prompts, orchestration or model shows up as a step in the median. Everything else in this
plan exists to make that number trustworthy or to explain it.

## Technical design decisions

**Schema** (goose, next migration is `0006`):

- `run_requests`: `run_id` (FK `runs`, cascade), `seq`, `request_id`, `at`, `model`, `thread`
  (NULL for the main thread, otherwise the spawning `tool_use` id), `input_tokens`,
  `cache_create_tokens`, `cache_read_tokens`, `output_tokens`, `context_tokens`, `tool`. PK
  `(run_id, seq)`.
- `utilization_readings`: `at`, `window` (`five_hour` | `seven_day`), `utilization` (0–1),
  `resets_at`, `run_id` (the run whose log carried it). Unique on `(at, window)`, because runs that
  overlap in time log the same event.
- `utilization_intervals`: `window`, `start_at`, `end_at`, `delta`, `weight` (API-price-weighted
  tokens across every local session in the interval), `contaminated` (bool). This is the fit's
  sample set. It is stored because the transcripts it was summed from get pruned.
- `tickets` gains `first_push_ci` (nullable bool) and `hand_churn_lines` (nullable int).

**Key models** (`internal/agentlog`):

- `Request`: one deduplicated API request with the four token classes, `Thread` and `Tool`.
- `Reading`: one `rate_limit_event` window reading.
- `ParseMetrics` returns `RunMetrics` with `Requests []Request` and `Readings []Reading` added. The
  existing totals become sums over `Requests`.
- `Weight(model string, u Usage) float64`: API-price weighting from a small per-model price table.
  It must agree with `total_cost_usd` on run27 to within 1%. That agreement is its test.

**Module boundaries:**

- `internal/agentlog` owns every stream-json parse, including transcript files, which share the
  dialect.
- A new package, `internal/usage`, owns the fit:
  - `Intervals(readings, transcriptsDir)` builds samples;
  - `Fit(intervals) Factor` is least squares through the origin, per window, over a trailing seven
    days, dropping contaminated samples;
  - `Share(weight, Factor) Percent`.
  - It is pure apart from reading the transcript directory, and it is the deep module that warrants
    isolated tests.
- `internal/cc` wires it. The existing disposition transaction writes requests, readings and
  intervals.

**Config:** `claude_projects_dir`, default `~/.claude/projects`.

**Merge time:** the `pr_merged` event's `at` (`merge_events.go`). No new column.

**Views:**

- Run detail: an inline SVG context curve rendered by the Go template. It is static, so it is not an
  island.
- Board masthead: gauges.
- Insights: the `cc-insights` island is repointed at the new JSON.
- Features page: the ticket bill.

Colours are `@theme` tokens only.

---

## Phase 1: Per-request capture and the context curve

**User stories:** see context growth per run; separate main thread from subagents; stop losing
fields to log pruning.

### What to build

- The parser learns `parent_tool_use_id` and `rate_limit_event`.
- Migration `0006` adds `run_requests` and `utilization_readings`.
- `RecordDisposition` writes both in its existing transaction.
- `BackfillMetrics` fills them for logs that still resolve.
- The run detail row gains a context-tokens-per-request curve, with the main thread and each
  subagent drawn as separate series.

### Acceptance criteria

- [ ] run27 parses to 4 main-thread requests with context 53,464 / 125,241 / 176,299 / 256,072.
- [ ] A fixture with a Task call attributes the subagent's requests to its `tool_use` id.
- [ ] Run-level token columns equal the sum of the run's `run_requests` rows.
- [ ] Readings from two overlapping runs are stored once.
- [ ] The run detail shows the curve for a disposed run, and nothing for a run with no rows.

---

## Phase 2: The % fit and the board gauges

**User stories:** see spend as a share of the 5h and weekly windows; see how much of current use is
cc and how much is other use.

### What to build

- On disposition, `internal/usage` builds intervals between consecutive readings for each window.
  The interval between the previous run's last reading and this run's first is included.
- Each interval's weight sums over every transcript request under `claude_projects_dir` in that
  interval, subagent transcripts included.
- Samples whose residual against the current fit is large are marked contaminated.
- The masthead shows each window's latest utilization, split into cc's share (the cc runs' weight in
  the window × factor) and everything else. It shows "calibrating" until there are enough samples.

### Acceptance criteria

- [ ] `Fit` recovers a known factor from synthetic intervals with contaminated outliers.
- [ ] An interval with interactive transcripts counts their weight.
- [ ] The gauges render from the stored readings and stay stable across polls (`hx-preserve`
      rules respected).
- [ ] Gauges read "calibrating" below the sample threshold instead of showing a number.

---

## Phase 3: Ticket spend over time

**User stories:** see spend per merged ticket over time, and see whether it drops after a change.

### What to build

- A query sums every run of a ticket, all kinds, until its `pr_merged` event, as the ticket's weight.
- The ticket's spend is that weight × the factor, in % of the week.
- `/insights.json` returns one point per merged ticket: ticket, merged at, % week, and the split by
  kind.
- It also returns withdrawn tickets' total as waste.
- The island draws a dot per ticket with a rolling median over the last 10 tickets, and the waste
  total. This replaces tokens/day.

### Acceptance criteria

- [ ] A ticket with an agent run, a resolve and a follow-up sums all three.
- [ ] Runs disposed after the merge are excluded.
- [ ] Withdrawn tickets appear only in the waste total.
- [ ] The rolling median is correct at the start of the series, when fewer than 10 points exist.

---

## Phase 4: Guardrails and the ticket bill

**User stories:** catch a change that is cheaper but produces worse PRs.

### What to build

- **`first_push_ci`:** set from the first terminal CI verdict after the ticket's first push. The
  source is the `verdict_transition` events.
- **`hand_churn_lines`:** set at merge observation from commits that cc did not make, landing after
  the ticket's last run was disposed. It reuses the foreign-commit detection in `git.go`.
- The features page gets a bar per ticket, stacked by run kind, with flags for a failed first push
  and for hand churn.
- The insights points carry both guardrails.

### Acceptance criteria

- [ ] A ticket whose first CI verdict failed and a later one passed records `false`.
- [ ] Commits pushed by a person after the last run count towards churn; the agent's commits do not.
- [ ] The bill's stacked total equals the ticket's spend.

---

## Phase 5: Tail guards

**User stories:** stop one runaway run from eating a 5h window.

### What to build

- **`max_turns`:** a config key substituted into `agent_command` as `--max-turns`.
- **`spend_limit_5h`:** a config key in %. `launchEligible` stops spawning new runs while the latest
  five-hour reading is at or above it, and the masthead says why.
- Nothing is killed automatically.

### Acceptance criteria

- [ ] A run hitting `max_turns` disposes as failed with its metrics captured.
- [ ] No spawn happens above the limit; spawning resumes when a later reading drops below it.

Handoff item #8, making cancel kill live runs, is dropped. `cancel` is only offered on queued
tickets, which have no live run, and `kill` already covers a running one.
