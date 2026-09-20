# Plan: run insights

> Source: the grilling session of 2026-09-20. The feature scaffolds the path from an agent's
> stdout to a plotted number and proves it once end to end. It does not attempt the full set of
> insights; those are added a panel at a time afterwards.

## Technical design decisions

**Scope of the metric.** Mechanical facts only — tokens, turns, duration, tool failures, cost.
Quality and hallucination rate are judgements, nothing emits them, and they stay out until
something exists that can score a run. Every number this feature shows is read off a log line.

**Where the numbers come from.** The agent's own stdout, never a vendor usage API. The Claude CLI
already emits everything needed on the `result` line of its `stream-json` output, and
`internal/agentlog` already decodes it. A vendor API would be account-scoped rather than
run-scoped, would need an admin key, and could not attribute spend to a ticket. So "provider"
here means "stdout dialect", and the seam sits at a parse boundary rather than a network one.

**The seam.** `internal/agentlog` becomes the dialect-neutral package it is already named after.
It owns one normalised type and one parse function per dialect:

```go
// RunMetrics is what every dialect must produce. A field a dialect cannot supply is nil or zero
// with Settled false, never a guess.
type RunMetrics struct {
	TokensIn     int64
	TokensOut    int64
	Turns        int
	Duration     time.Duration
	CostUSD      *float64
	ToolCalls    int
	ToolFailures int
	Model        string
	Settled      bool
}

func ParseMetrics(logPath string) (RunMetrics, error) // the Claude stream-json dialect
```

`internal/cc` injects it as a func type, matching `ObserveFunc`, `CheckoutFunc`, `RepoCheckFunc`
and `TrackerSource` rather than adding a fifth interface to a repo that has four:

```go
type MetricsParser func(logPath string) (agentlog.RunMetrics, error)
func WithMetricsParser(p MetricsParser) Option
```

A second agent CLI is a second function in `agentlog` and a different `WithMetricsParser`. No
factory, no `Kind`, no config key — the `agent_command` argv already says which agent is running,
and inventing a second way to say it would be two sources of one truth.

**Cost is nullable, deliberately.** `total_cost_usd` is computed by the Claude CLI itself. A
dialect that does not price its own tokens leaves it nil, and nothing downstream may treat nil as
zero. This is why the headline panel plots tokens rather than dollars: tokens are the one quantity
every dialect emits, so the first panel is not the first thing to break under a second provider.

**Capture point.** At `RecordDisposition`, once per run, immediately after the process is known
dead. The JSONL stays prunable — `pruneRunLogs` is unchanged — which means the field set is frozen
for all history the moment this ships. A metric not captured now can never be backfilled for
today's runs, and that is the whole reason `tool_calls` and `tool_failures` are in the first
migration despite no panel yet reading them.

**Schema.** One migration, `0004_run_metrics.sql`, adding nullable columns to `runs`:

```sql
ALTER TABLE runs
    ADD COLUMN tokens_in       bigint,
    ADD COLUMN tokens_out      bigint,
    ADD COLUMN turns           bigint,
    ADD COLUMN duration_ms     bigint,
    ADD COLUMN cost_usd        double precision,
    ADD COLUMN tool_calls      bigint,
    ADD COLUMN tool_failures   bigint,
    ADD COLUMN model           text,
    ADD COLUMN metrics_settled boolean;
```

Every column is nullable because NULL and zero mean different things. A `cut_failed` run never had
a process, so it has no log and keeps NULLs. A killed run has a log but no `result` line, so it
gets summed partials with `metrics_settled = false`. Only a clean run is settled.

**Partial runs count.** Those tokens were genuinely spent, and excluding them makes the chart
under-report on exactly the worst days. The aggregate returns an unsettled count per bucket so a
day that is partly estimated can be marked as such.

**Grain, and how tasks compose into features.** `runs.ticket_id` is `NOT NULL` and references
`tickets(url)`, and `tickets.feature` names the feature, so the chain `run -> ticket -> feature`
already exists and no column is added for it. A task's figure is a sum over its runs — a ticket
has many, across the `agent`, `resolve` and `follow_up` kinds — and a feature's figure is the same
sum with one more join. Composition is a `GROUP BY`, which is why a per-task or per-feature rollup
panel can be added later without touching the migration.

Three semantics follow from that, and each is a deliberate choice rather than an accident of the
join:

A run counts toward the feature named by its ticket's `feature` column, and nothing else. ADR 13
closes a feature over its unmerged blockers, so the launch modal will show a feature containing
tickets filed under another one; spend does not follow that closure. Under the column reading every
run belongs to exactly one feature, so the features sum to the true total with nothing
double-counted, and a shared blocker's cost sits with the feature that scoped it.

Withdrawn tickets count. Every other read in this codebase filters `withdrawn_at IS NULL`; insights
deliberately do not. A ticket abandoned after three failed runs is precisely the signal a health
view exists to surface, and since remove-worktree withdraws the ticket, following the convention
would shrink the totals every time a finished ticket is tidied away. The ADR must state this
explicitly, or it will be read as an oversight and corrected.

The feature join is live, not snapshotted. Re-filing a ticket in the tracker moves its past spend
with it, because `ImportTicket` overwrites `feature` on conflict. That is accepted: a feature costs
whatever the tickets currently in it cost. Denormalising `feature` onto the run would buy immutable
history at the price of a column that backfilled rows could only fill with today's answer anyway.

**Merge time.** `gh.PR` gains `MergedAt`, added to `bulkFields` and `fallbackFields`. When the
observation shows a PR newly merged, the tick appends one `pr_merged` event stamped with GitHub's
own timestamp, not tick time. Nothing durably records this today: merged-ness is read off the
single `meta.observation` row that is overwritten twice per tick, and `recordVerdictTransitions`
skips any ticket whose PR is not currently open, so the terminal transition into merged is the one
transition it never logs. Adding a field to an existing query costs no extra API call.

Nothing in this feature yet reads `pr_merged`. It lands first anyway, because it is the one fact
here that cannot be recovered after the fact.

**Backfill.** A one-shot pass on first start after the migration, over every run with a `log_path`
that still resolves and NULL metrics. Idempotent by that predicate, so a restart mid-backfill
resumes rather than repeats. Runs whose logs were pruned keep NULLs forever. This also exercises
the parser against real historical logs before it ever runs on a live disposition.

**Two spend paths, by design.** A running agent has no metrics row — the row is written at
disposition. So the in-memory `Accumulator` in `internal/cc/spend.go` keeps serving live rows on
the board and band, and Postgres serves everything disposed. The rule is one line: running reads
memory, ended reads the database, and the disposition write is the handover. Neither path is
deleted.

**Routes.** `GET /insights` renders the page; `GET /insights.json` serves the data. Both take
`repo` and `feature`, inherited verbatim from ADR 11's scope grammar so a board pill can link
straight through, plus `since`, the one axis the board has no concept of. `since` defaults to 30
days. An unrecognised value falls back to the default rather than erroring, following
`normalizeLogFilter`.

The page is static per load. The data changes only when a run disposes, roughly once every few
minutes at best, so a five-second poll running aggregate SQL twelve times a minute buys nothing.

**Contract.** `GET /insights.json` returns the bucket series directly:

```json
{
  "since": "2026-08-21",
  "until": "2026-09-20",
  "timezone": "Europe/London",
  "buckets": [
    { "day": "2026-09-19", "tokens_in": 412000, "tokens_out": 18400, "runs": 6, "unsettled": 1 }
  ]
}
```

**Bucketing.** Days are local to the daemon, not UTC: under BST a late-evening run would otherwise
land on the previous day. The range is generated with `generate_series` and left-joined, so a
quiet weekend reads as two flat days rather than the line jumping across the gap.

**Module boundaries.** `internal/agentlog` owns every stdout dialect and the normalised
`RunMetrics`, and imports nothing from `internal/cc`. It is the deep module here: the whole
wire format sits behind one struct and one function, and it is tested in isolation against fixture
JSONL. `internal/cc` owns the disposition write, the backfill, the aggregate query and the
handlers. `internal/gh` gains one field and keeps its rule that nothing outside the file reads
`rawPR`'s names.

**Rendering.** A third Solid island, `web/src/insights.tsx`, hand-rolled inline SVG, no charting
library. The measured case: the entire current JS payload is 8.2 KB gzipped, against 58.5 KB for a
tree-shaken Chart.js and 185.9 KB for ECharts. More decisively, a canvas library cannot read a CSS
custom property, so every themed chart would need `getComputedStyle` token resolution, a
`MutationObserver` on `[data-theme]`, and a destroy-and-rebuild cycle. An SVG chart takes
`fill: var(--color-accent)` from `app.css` and gets dark mode for nothing. The hand-rolled
sparkline, bar chart, tick ladder and histogram binner measure 112 lines and 1.8 KB gzipped. The
DAG layout in `web/src/layout.ts` is the same move already made once.

The island calls `noShadowDOM()` like the other two (ADR 2), and its classes live in `app.css`
beside the existing `meter` and `segbar-segment` grammar.

**Test boundary.** The `graph.tsx` precedent exactly: the parser is unit-tested against fixture
JSONL, the page shell is a golden file, and `GET /insights.json` is asserted in a `.txtar` e2e.
The island itself stays untested — it renders data proven correct one layer down, and covering it
would put a browser tier into a `just test` that CI deliberately keeps Go-only.

**Documentation owed.** One ADR recording that run metrics are captured at disposition from the
agent's own stdout. That is the contested call: observability re-enters ahead of the trigger §12
named for it, and it reads stdout at run end rather than pushing to OTel, which §12 named as the
alternative. The ADR also records why the field set is frozen. The design doc's §12 and §13 need
the matching revision, since its preamble makes it normative and its own rule says a disagreement
with the code is a bug in the code.

No ADR is owed for the renderer. Hand-rolling is CLAUDE.md's existing no-dependency rule applied,
not a departure from it.

**Navigation.** The `/insights` link sits in the masthead beside the scope pills, following how
`/features` is reached.

**Auth.** None. Both routes are GETs on a localhost-bound server; `requireBrowserOrigin` guards
POSTs and neither of these is one.

---

## Phase 1: Record when a PR merged

**Question it answers**: how long did this ticket actually take, from launch to merge?

### What to build

Add `MergedAt` to `gh.PR` and to the field lists `bulkFields` and `fallbackFields`, decoded in
`rawPR` like every other field. In the tick, when the observation shows a branch's PR in `Merged`
state and no `pr_merged` event exists for that ticket, append one, stamped with GitHub's
timestamp rather than the tick's.

Nothing reads the event yet. It lands first because it is the only fact in this feature that
cannot be recovered later: every merge between now and whenever this ships is a duration that can
never be measured.

Dedupe on the event log itself, not on a `meta` blob. `last_verdicts` is already a mutable blob
whose loss re-emits phantom transitions, and repeating that mistake would make merge times
duplicate on a reset.

### Acceptance criteria

- [ ] `gh.PR` carries `MergedAt`, populated from the same `gh pr list` call with no extra request
- [ ] A ticket whose PR merges gets exactly one `pr_merged` event, with GitHub's merge time
- [ ] A second tick over the same merged PR appends nothing
- [ ] Dedupe survives a daemon restart and a cleared `meta` table
- [ ] A PR that closes unmerged produces no `pr_merged` event
- [ ] Covered by a `.txtar` e2e driving a fixture through `fakegh`

---

## Phase 2: Persist and serve run metrics

**Question it answers**: what did my agents spend, and how much of it was wasted?

### What to build

The full path from log to endpoint, with no UI. `internal/agentlog` gains `RunMetrics` and
`ParseMetrics`, reading the `result` line for the settled case and summing per-assistant-line usage
for the unsettled one, plus counting tool calls and tool failures from the events it already
parses. `internal/cc` gains the `MetricsParser` func type, the `WithMetricsParser` option, the
`0004_run_metrics.sql` migration, the write inside `RecordDisposition`, and the one-shot backfill
on start.

Then the aggregate: daily buckets in the daemon's local timezone, zero-filled across the requested
range, summing input and output tokens separately, counting runs and unsettled runs per bucket, and
filtered by `repo` and `feature` through a join on `tickets`. Served at `GET /insights.json`.

This phase is verifiable on its own — the endpoint is curl-able and e2e-asserted, and the columns
are queryable directly. It cuts every layer except the browser.

### Acceptance criteria

- [ ] `ParseMetrics` returns settled metrics for a clean log and partials with `Settled` false for
      a log with no `result` line, tested against fixture JSONL of both shapes
- [ ] A log that is empty, truncated mid-line or absent returns an error rather than zeroes
- [ ] `cost_usd` is nil, not zero, when the dialect emits no cost
- [ ] Disposition writes the metrics columns for every run kind; a `cut_failed` run keeps NULLs
- [ ] Backfill populates historical runs whose logs survive, leaves pruned ones NULL, and is safe
      to run twice
- [ ] `GET /insights.json` zero-fills days with no runs and honours `repo`, `feature` and `since`
- [ ] `feature` matches on the ticket's own column, so a cross-feature blocker's runs are excluded
- [ ] Runs belonging to withdrawn tickets are included in every total
- [ ] An unparseable `since` falls back to 30 days rather than erroring
- [ ] The daily bucket boundary is local time, asserted with a run either side of local midnight
- [ ] Unsettled runs are included in bucket totals and counted separately
- [ ] Covered by a `.txtar` e2e asserting the JSON shape

---

## Phase 3: The insights page

**Question it answers**: is this getting better or worse?

### What to build

`GET /insights` renders a page shell on the `/features` precedent — its own template, its own view
struct, a masthead link, the scope pills carried through. Inside it, `<cc-insights>`, a third Solid
island compiled by `solid-element` with `noShadowDOM()`, fetching `/insights.json` and drawing one
panel: tokens per day, input and output split, over the last 30 days.

The SVG is hand-rolled — a scale helper, the 1/2/5 tick ladder, a gridded y-axis, native `<title>`
tooltips. Chart classes go in `app.css` next to the existing `meter` and `segbar-segment` grammar
and take their colour from the existing oklch tokens, so dark mode needs no JavaScript at all.

Then the ADR and the design doc revision.

### Acceptance criteria

- [ ] `GET /insights` renders with the masthead, scope pills and the island, covered by a golden file
- [ ] The panel plots input and output tokens per local day, zero-filled, over the default 30 days
- [ ] A day containing unsettled runs is visibly marked
- [ ] Dark mode is correct with no JavaScript reading computed styles
- [ ] The page renders sensibly with an empty series and with a single day
- [ ] `web/package.json` is unchanged
- [ ] `just assets` has run and `internal/cc/assets/dist/app.css` and the built island are committed
- [ ] An ADR records capturing metrics at disposition from agent stdout
- [ ] The design doc's §12 and §13 are revised to match
