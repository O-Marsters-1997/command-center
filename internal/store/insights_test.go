package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

var june10 = storepkg.SpendQuery{
	Loc:   time.UTC,
	Since: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
	Until: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
}

func boardSpend(t *testing.T, store *storepkg.Store, repo string) map[string]storepkg.BoardTicketSpend {
	t.Helper()
	byURL, err := store.BoardTicketSpend(t.Context(), repo, "")
	if err != nil {
		t.Fatalf("BoardTicketSpend(%q): %v", repo, err)
	}
	return byURL
}

func TestStore_BoardTicketSpend(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	t.Run("a merged ticket counts only pre-merge runs", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", at.Add(-time.Hour), 1.00)
		storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", at.Add(time.Hour), 5.00)
		storetest.Merged(t, store, "sandbox://CC-1", at)

		got, ok := boardSpend(t, store, "")["sandbox://CC-1"]
		if !ok || !got.Merged || got.TotalUSD() != 1.00 {
			t.Errorf("BoardTicketSpend()[CC-1] = %+v, %v, want merged with total 1.00", got, ok)
		}
	})

	t.Run("an open ticket sums its spend so far", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", at, 1.00)
		storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", at, 0.50)

		got, ok := boardSpend(t, store, "")["sandbox://CC-1"]
		if !ok || got.Merged || got.AgentUSD != 1.00 || got.FollowUpUSD != 0.50 {
			t.Errorf("BoardTicketSpend()[CC-1] = %+v, %v, want unmerged with agent 1.00, follow-up 0.50", got, ok)
		}
	})

	t.Run("scopes by repo", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.FeatureTicket(t, store, "sandbox://CC-2", "cc-other", "feat-b")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", at, 1.00)
		storetest.Disposed(t, store, "sandbox://CC-2", "agent", at, 9.00)

		byURL := boardSpend(t, store, "cc-sandbox")
		_, hasCC1 := byURL["sandbox://CC-1"]
		_, hasCC2 := byURL["sandbox://CC-2"]
		if !hasCC1 || hasCC2 {
			t.Errorf("BoardTicketSpend(cc-sandbox) = %+v, want CC-1 only", byURL)
		}
	})
}

func TestStore_MergedTicketFacts(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	first := mergedAt.Add(-5 * time.Hour)
	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-2", "cc-other", "feat-a")

	run := func(url string, started, ended time.Time, usd float64, m agentlog.RunMetrics) {
		t.Helper()
		id := storetest.Run(t, store, storetest.RunOpts{
			TicketURL: url, Kind: "agent", BaselineSHA: "deadbeef", PromptHash: "h" + ended.String(), StartedAt: started,
		})
		err := store.RecordDisposition(t.Context(), storepkg.Disposition{
			RunID: plan.RunID(id), Outcome: plan.OutcomePush, Cost: option.Some(usd),
			Metrics: option.Some(m), EndedAt: ended,
		})
		if err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
	}
	run("sandbox://CC-1", first.Add(time.Hour), mergedAt.Add(-3*time.Hour), 1, agentlog.RunMetrics{
		Input: 10, CacheWrite: 20, CacheRead: 1000, Output: 30, Tools: map[string]int{"Bash": 2, "Read": 3},
	})
	run("sandbox://CC-1", first, mergedAt.Add(-2*time.Hour), 2, agentlog.RunMetrics{
		Input: 1, Output: 1, Tools: map[string]int{"Bash": 1},
	})
	run("sandbox://CC-1", first, mergedAt.Add(time.Hour), 9, agentlog.RunMetrics{Input: 999})
	storetest.Merged(t, store, "sandbox://CC-1", mergedAt)
	event := func(kind storepkg.EventKind, when time.Time, detail string) {
		t.Helper()
		ev := storepkg.Event{At: when, TicketURL: "sandbox://CC-1", Kind: kind, Detail: detail}
		if err := store.AppendEvent(t.Context(), ev); err != nil {
			t.Fatalf("AppendEvent(%s): %v", kind, err)
		}
	}
	for i, tip := range []string{"t1", "t2", "t3"} {
		at := mergedAt.Add(time.Duration(i-4) * time.Hour)
		if i == 2 {
			at = mergedAt.Add(time.Hour)
		}
		storetest.Pushed(t, store, storetest.PushedOpts{URL: "sandbox://CC-1", Tip: tip, Base: "main", BaseTip: "b", At: at})
	}
	event(storepkg.EventVerdictTransition, mergedAt.Add(-4*time.Hour+time.Minute), "ci_failed: lint")
	event(storepkg.EventVerdictTransition, mergedAt.Add(-3*time.Hour+time.Minute), "review_me: green")
	event(storepkg.EventRefreshed, mergedAt.Add(-5*time.Hour), "merged origin/main")
	event(storepkg.EventRefreshed, mergedAt.Add(-4*time.Hour), "merged origin/main")
	event(storepkg.EventRestacked, mergedAt.Add(-3*time.Hour), "restacked onto origin/main")
	event(storepkg.EventRefreshConflicted, mergedAt.Add(-2*time.Hour), "conflict")
	event(storepkg.EventRefreshRefused, mergedAt.Add(-time.Hour), "dirty")
	event(storepkg.EventRefreshed, mergedAt.Add(time.Hour), "after merge")
	run("sandbox://CC-2", first, mergedAt.Add(-time.Hour), 1, agentlog.RunMetrics{})
	storetest.Merged(t, store, "sandbox://CC-2", mergedAt)

	q := june10
	q.Repo = "cc-sandbox"
	got, err := store.MergedTicketFacts(t.Context(), q)
	if err != nil {
		t.Fatalf("MergedTicketFacts: %v", err)
	}
	want := []insight.TicketFact{{
		Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Feature: "feat-a", FirstRunAt: first, MergedAt: mergedAt,
		WeightUSD: 3, Tokens: 62, ToolCalls: 6,
		Pushes: []insight.PushFact{
			{At: mergedAt.Add(-4 * time.Hour), Verdict: insight.PushRed},
			{At: mergedAt.Add(-3 * time.Hour), Verdict: insight.PushGreen},
		},
		Refreshes: 4, Conflicted: 1,
	}}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(insight.TicketFact{}, "Title")); diff != "" {
		t.Errorf("MergedTicketFacts(cc-sandbox, june 10) mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_ReworkFacts(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 6, day, hour, minute, 0, 0, time.UTC) }
	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-2", "other", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-3", "cc-sandbox", "feat-b")

	event := func(url string, kind storepkg.EventKind, when time.Time, detail string) {
		t.Helper()
		if err := store.AppendEvent(ctx, storepkg.Event{At: when, TicketURL: url, Kind: kind, Detail: detail}); err != nil {
			t.Fatalf("AppendEvent(%s): %v", kind, err)
		}
	}
	run := func(url string, cause plan.FollowUpCause, started, ended time.Time, m agentlog.RunMetrics) {
		t.Helper()
		id, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: url, Kind: plan.RunKindAgent, BaselineSHA: "deadbeef", PromptHash: "h" + started.String(), Cause: cause,
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton: %v", err)
		}
		if err := store.RecordSpawn(ctx, storepkg.Spawn{RunID: plan.RunID(id), StartedAt: started}); err != nil {
			t.Fatalf("RecordSpawn: %v", err)
		}
		if err := store.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(id), Outcome: plan.OutcomePush, Metrics: option.Some(m), EndedAt: ended,
		}); err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
	}

	run("sandbox://CC-1", "", at(10, 7, 0), at(10, 8, 0), agentlog.RunMetrics{
		Input: 10, CacheWrite: 20, CacheRead: 999, Output: 30,
	})
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(9, 8, 0), "old check failed")
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(10, 8, 30), "unit tests failed")
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(10, 8, 30), "lint failed")
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(10, 8, 30), "ok-status-check failed")
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(10, 8, 35), "docs passed")
	run("sandbox://CC-1", plan.CauseCIFailed, at(10, 8, 45), at(10, 9, 10), agentlog.RunMetrics{Input: 40, Output: 60})
	event("sandbox://CC-1", storepkg.EventChecksLanded, at(10, 9, 15), "lint passed")
	event("sandbox://CC-1", storepkg.EventParked, at(10, 9, 0), "verify_command failed; log at /data/1.log")
	run("sandbox://CC-1", plan.CauseVerifyFailed, at(10, 9, 20), at(10, 9, 30), agentlog.RunMetrics{Input: 5})

	event("sandbox://CC-2", storepkg.EventChecksLanded, at(10, 8, 30), "unit tests failed")
	event("sandbox://CC-2", storepkg.EventParked, at(10, 9, 0), "other repo")
	event("sandbox://CC-3", storepkg.EventParked, at(10, 9, 0), "other feature")

	q := june10
	q.Repo, q.Feature = "cc-sandbox", "feat-a"
	got, err := store.ReworkFacts(ctx, q)
	if err != nil {
		t.Fatalf("ReworkFacts(%+v): %v", q, err)
	}
	want := insight.ReworkFacts{
		Parks: []insight.ParkFact{{
			Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Reason: "verify_command failed",
			Detail: "verify_command failed; log at /data/1.log", At: at(10, 9, 0), Tokens: 60,
		}},
		Failed: []insight.FailedCheck{
			{Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Name: "unit tests", At: at(10, 8, 30)},
			{Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Name: "lint", At: at(10, 8, 30)},
			{Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Name: "ok-status-check", At: at(10, 8, 30)},
		},
		Fixes: []insight.FixRun{{
			Ticket: "sandbox://CC-1", LaunchedAt: at(10, 8, 45), Tokens: 100,
			Checks: []string{"lint", "ok-status-check", "old check", "unit tests"},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ReworkFacts(%+v) mismatch (-want +got):\n%s", q, diff)
	}
}

func TestStore_ConflictFacts(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	at := func(day, hour int) time.Time { return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC) }
	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-2", "other", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-3", "cc-sandbox", "feat-b")

	start := func(url string, when time.Time) {
		t.Helper()
		id, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: url, Kind: plan.RunKindAgent, BaselineSHA: "deadbeef", PromptHash: "h" + url,
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton: %v", err)
		}
		if err := store.RecordSpawn(ctx, storepkg.Spawn{RunID: plan.RunID(id), StartedAt: when}); err != nil {
			t.Fatalf("RecordSpawn: %v", err)
		}
	}
	conflict := func(url string, paths ...insight.ConflictPath) {
		t.Helper()
		if err := store.RecordRefreshConflict(ctx, url, "conflicted", paths); err != nil {
			t.Fatalf("RecordRefreshConflict(%s): %v", url, err)
		}
	}
	start("sandbox://CC-1", at(9, 8))
	start("sandbox://CC-2", at(9, 8))
	start("sandbox://CC-3", at(9, 8))
	storetest.Merged(t, store, "sandbox://CC-1", at(11, 9))

	conflict("sandbox://CC-1", insight.ConflictPath{Path: "a.go"}, insight.ConflictPath{Path: "gen.sql", Generated: true})
	conflict("sandbox://CC-2", insight.ConflictPath{Path: "a.go"})
	conflict("sandbox://CC-3", insight.ConflictPath{Path: "a.go"})
	legacy := storepkg.Event{TicketURL: "sandbox://CC-1", Kind: storepkg.EventRefreshConflicted}
	if err := store.AppendEvent(ctx, legacy); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	now := time.Now().UTC()
	q := storepkg.SpendQuery{
		Loc: time.UTC, Repo: "cc-sandbox", Feature: "feat-a",
		Since: now.AddDate(0, 0, -1), Until: now.AddDate(0, 0, 1),
	}
	got, err := store.ConflictFacts(ctx, q)
	if err != nil {
		t.Fatalf("ConflictFacts(%+v): %v", q, err)
	}
	want := insight.ConflictFacts{
		Conflicts: []insight.ConflictFact{{
			Ticket: "sandbox://CC-1", Repo: "cc-sandbox", Feature: "feat-a",
			Paths: []insight.ConflictPath{{Path: "a.go"}, {Path: "gen.sql", Generated: true}},
		}},
		Spans: []insight.OpenSpan{{Ticket: "sandbox://CC-1", Feature: "feat-a", From: at(9, 8), To: option.Some(at(11, 9))}},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(insight.ConflictFact{}, "At"),
		cmp.AllowUnexported(option.Option[time.Time]{}),
	}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("ConflictFacts(%+v) mismatch (-want +got):\n%s", q, diff)
	}

	q.Since, q.Until = at(1, 0), at(2, 0)
	if got, err := store.ConflictFacts(ctx, q); err != nil || len(got.Conflicts) != 0 || len(got.Spans) != 0 {
		t.Errorf("ConflictFacts(%+v) = %+v, %v, want nothing outside the window", q, got, err)
	}
}

func TestStore_TicketInsightFacts(t *testing.T) {
	store := openStore(t)
	const url = "sandbox://CC-1"
	base := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	storetest.FeatureTicket(t, store, url, "cc-sandbox", "feat-a")
	storetest.FeatureTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-a")

	run := func(ticket string, started, ended time.Time, usd float64, m agentlog.RunMetrics) int64 {
		t.Helper()
		id := storetest.Run(t, store, storetest.RunOpts{
			TicketURL: ticket, Kind: "agent", Step: "implement", BaselineSHA: "deadbeef",
			PromptHash: "h" + ended.String(), StartedAt: started,
		})
		err := store.RecordDisposition(t.Context(), storepkg.Disposition{
			RunID: plan.RunID(id), Outcome: plan.OutcomePush, Cost: option.Some(usd),
			Metrics: option.Some(m), EndedAt: ended,
		})
		if err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
		return id
	}
	verdict := func(when time.Time, detail string) {
		t.Helper()
		ev := storepkg.Event{At: when, TicketURL: url, Kind: storepkg.EventVerdictTransition, Detail: detail}
		if err := store.AppendEvent(t.Context(), ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	push := func(when time.Time, tip string) {
		storetest.Pushed(t, store, storetest.PushedOpts{URL: url, Tip: tip, Base: "main", BaseTip: "b", At: when})
	}

	first := run(url, at(0), at(10), 1, agentlog.RunMetrics{
		Input: 10, CacheWrite: 20, CacheRead: 1000, Output: 30, Tools: map[string]int{"Bash": 2, "Read": 3},
	})
	run(url, at(20), at(50), 2, agentlog.RunMetrics{Input: 1, Output: 1, Tools: map[string]int{"Bash": 1}})
	run("sandbox://CC-2", at(0), at(5), 7, agentlog.RunMetrics{Input: 500})
	push(at(11), "t1")
	verdict(at(12), "ci_failed: lint")
	push(at(30), "t2")
	verdict(at(31), "review_me: green")
	push(at(45), "t3")

	t.Run("unmerged ticket", func(t *testing.T) {
		got, err := store.TicketInsightFacts(t.Context(), url)
		if err != nil {
			t.Fatalf("TicketInsightFacts: %v", err)
		}
		if got.MergedAt.IsSome() {
			t.Errorf("MergedAt = %v, want none", got.MergedAt)
		}
		wantPushes := []insight.PushFact{
			{At: at(11), Verdict: insight.PushRed},
			{At: at(30), Verdict: insight.PushGreen},
			{At: at(45), Verdict: insight.PushPending},
		}
		if diff := cmp.Diff(wantPushes, got.Pushes); diff != "" {
			t.Errorf("TicketInsightFacts pushes mismatch (-want +got):\n%s", diff)
		}
		if len(got.Runs) != 2 {
			t.Fatalf("runs = %d, want 2", len(got.Runs))
		}
		wantFirst := insight.RunFact{
			ID: first, Step: "implement", Kind: "agent", StartedAt: at(0), EndedAt: at(10),
			WeightUSD: 1, Tokens: 60, Tools: map[string]int{"Bash": 2, "Read": 3}, Outcome: plan.OutcomePush.String(),
		}
		if diff := cmp.Diff(wantFirst, got.Runs[0]); diff != "" {
			t.Errorf("TicketInsightFacts first run mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("merged ticket matches its TicketFact", func(t *testing.T) {
		storetest.Merged(t, store, url, at(60))
		run(url, at(70), at(80), 9, agentlog.RunMetrics{Input: 999})
		got, err := store.TicketInsightFacts(t.Context(), url)
		if err != nil {
			t.Fatalf("TicketInsightFacts: %v", err)
		}
		if got.MergedAt.IsNone() || !got.MergedAt.Unwrap().Equal(at(60)) {
			t.Errorf("MergedAt = %v, want %v", got.MergedAt, at(60))
		}
		report := insight.DeriveTicket(got, spend.Result{Factor: 0.01, Samples: spend.MinSamples})

		facts, err := store.MergedTicketFacts(t.Context(), june10)
		if err != nil {
			t.Fatalf("MergedTicketFacts: %v", err)
		}
		i := slices.IndexFunc(facts, func(f insight.TicketFact) bool { return f.Ticket == url })
		if i < 0 {
			t.Fatalf("MergedTicketFacts has no %s", url)
		}
		fact := facts[i]
		if report.Tokens != fact.Tokens || report.ToolCalls != fact.ToolCalls {
			t.Errorf("report tokens, tools = %d, %d, want %d, %d", report.Tokens, report.ToolCalls, fact.Tokens, fact.ToolCalls)
		}
		if want := fact.MergedAt.Sub(fact.FirstRunAt); report.LaunchToMerge.UnwrapOr(0) != want {
			t.Errorf("LaunchToMerge = %v, want %v", report.LaunchToMerge, want)
		}
		if want := spend.PctWeek(fact.WeightUSD, 0.01); report.PctWeek.UnwrapOr(-1) != want {
			t.Errorf("PctWeek = %v, want %v", report.PctWeek, want)
		}
	})
}
