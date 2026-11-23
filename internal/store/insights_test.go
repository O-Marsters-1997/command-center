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

func withdraw(t *testing.T, store *storepkg.Store, ticketURL string, at time.Time, merged bool) {
	t.Helper()
	if _, err := store.WithdrawTicket(t.Context(), ticketURL, at, merged); err != nil {
		t.Fatalf("WithdrawTicket(%s): %v", ticketURL, err)
	}
}

var june10 = storepkg.SpendQuery{
	Loc:   time.UTC,
	Since: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
	Until: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
}

func mergedSpend(t *testing.T, store *storepkg.Store) []storepkg.TicketSpend {
	t.Helper()
	points, err := store.MergedTicketSpend(t.Context(), june10)
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	return points
}

func boardSpend(t *testing.T, store *storepkg.Store, repo string) map[string]storepkg.BoardTicketSpend {
	t.Helper()
	byURL, err := store.BoardTicketSpend(t.Context(), repo, "")
	if err != nil {
		t.Fatalf("BoardTicketSpend(%q): %v", repo, err)
	}
	return byURL
}

func TestStore_MergedTicketSpend(t *testing.T) {
	t.Parallel()
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	t.Run("sums every run kind", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-2*time.Hour), 1.00)
		storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(-2*time.Hour), 0.10)
		storetest.Merged(t, store, "sandbox://CC-1", mergedAt)

		points := mergedSpend(t, store)
		if len(points) != 1 {
			t.Fatalf("MergedTicketSpend() = %+v, want one point", points)
		}
		if got := points[0]; got.AgentUSD != 1.00 || got.FollowUpUSD != 0.10 || got.TotalUSD() != 1.10 {
			t.Errorf("MergedTicketSpend()[0] = %+v, want agent 1.00, follow-up 0.10, total 1.10", got)
		}
	})

	t.Run("excludes runs disposed after the merge", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-time.Hour), 1.00)
		storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(time.Hour), 5.00)
		storetest.Merged(t, store, "sandbox://CC-1", mergedAt)

		if points := mergedSpend(t, store); len(points) != 1 || points[0].TotalUSD() != 1.00 {
			t.Errorf("MergedTicketSpend() = %+v, want the one pre-merge run only", points)
		}
	})

	t.Run("excludes unmerged tickets", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		storetest.Disposed(t, store, "sandbox://CC-1", "agent", mergedAt, 3.00)
		withdraw(t, store, "sandbox://CC-1", mergedAt, false)

		if points := mergedSpend(t, store); len(points) != 0 {
			t.Errorf("MergedTicketSpend() = %+v, want none for a withdrawn unmerged ticket", points)
		}
	})
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

func TestStore_WithdrawnTicketWaste(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	storetest.Disposed(t, store, "sandbox://CC-1", "agent", day, 2.00)
	withdraw(t, store, "sandbox://CC-1", day, false)

	storetest.FeatureTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-a")
	storetest.Disposed(t, store, "sandbox://CC-2", "agent", day.Add(-time.Hour), 9.00)
	storetest.Merged(t, store, "sandbox://CC-2", day)
	withdraw(t, store, "sandbox://CC-2", day, true)

	waste, err := store.WithdrawnTicketWaste(t.Context(), june10)
	if err != nil {
		t.Fatalf("WithdrawnTicketWaste: %v", err)
	}
	if waste != 2.00 {
		t.Errorf("WithdrawnTicketWaste(june 10) = %v, want 2.00 from the unmerged withdrawal only", waste)
	}
	if points := mergedSpend(t, store); len(points) != 1 || points[0].Ticket != "sandbox://CC-2" {
		t.Errorf("MergedTicketSpend(june 10) = %+v, want only CC-2", points)
	}
}

func TestStore_MergedTicketWorkflowSpend_Rework(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	openedAt := mergedAt.Add(-4 * time.Hour)
	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")

	storetest.Disposed(t, store, "sandbox://CC-1", "agent", openedAt.Add(-time.Hour), 1)
	storetest.Disposed(t, store, "sandbox://CC-1", "agent", openedAt.Add(time.Hour), 1)
	storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", openedAt.Add(2*time.Hour), 1)
	storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(time.Hour), 1)
	verdict := func(at time.Time, detail string) storepkg.Event {
		return storepkg.Event{At: at, TicketURL: "sandbox://CC-1", Kind: storepkg.EventVerdictTransition, Detail: detail}
	}
	appendEvents(t, store,
		verdict(openedAt.Add(-2*time.Hour), "ci_failed: Tests"),
		storepkg.Event{At: openedAt, TicketURL: "sandbox://CC-1", Kind: storepkg.EventPROpened},
		verdict(openedAt.Add(time.Hour), "ci_failed: Tests"),
		verdict(openedAt.Add(90*time.Minute), "review_me: green"),
	)
	storetest.Merged(t, store, "sandbox://CC-1", mergedAt)

	got, err := store.MergedTicketWorkflowSpend(t.Context(), june10)
	if err != nil {
		t.Fatalf("MergedTicketWorkflowSpend: %v", err)
	}
	want := storepkg.Rework{FollowUps: 1, ReRuns: 1, RedCI: 1}
	if len(got) != 1 || got[0].Rework != want {
		t.Errorf("MergedTicketWorkflowSpend(june 10) = %+v, want one ticket with rework %+v", got, want)
	}
}

func TestStore_WithdrawnWorkflowTickets(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	storetest.Disposed(t, store, "sandbox://CC-1", "agent", day, 2.00)
	storetest.Disposed(t, store, "sandbox://CC-1", "follow_up", day, 2.00)
	withdraw(t, store, "sandbox://CC-1", day, false)

	storetest.FeatureTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-a")
	storetest.Disposed(t, store, "sandbox://CC-2", "agent", day.Add(-time.Hour), 9.00)
	storetest.Merged(t, store, "sandbox://CC-2", day)
	withdraw(t, store, "sandbox://CC-2", day, true)

	got, err := store.WithdrawnWorkflowTickets(t.Context(), june10)
	if err != nil {
		t.Fatalf("WithdrawnWorkflowTickets: %v", err)
	}
	want := map[string]int{"": 1}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WithdrawnWorkflowTickets(june 10) mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_MergedTicketWorkflowSpend(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storetest.FeatureTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")

	type metrics = option.Option[agentlog.RunMetrics]
	dispose := func(step plan.StepName, hash string, cost float64, m metrics, at time.Time) {
		t.Helper()
		runID, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: "sandbox://CC-1", Kind: plan.RunKindAgent, BaselineSHA: "deadbeef", PromptHash: "h",
			Step: step, Workflow: "wf", WorkflowHash: hash,
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
		if err := store.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.OutcomePush, Cost: option.Some(cost), Metrics: m, EndedAt: at,
		}); err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
	}
	measured := func(wall time.Duration, turns int) metrics {
		tools := map[string]int{"Bash": turns}
		return option.Some(agentlog.RunMetrics{Wall: option.Some(wall), Turns: turns, Tools: tools})
	}

	dispose("implement", "aaa", 1, measured(time.Minute, 2), mergedAt.Add(-3*time.Hour))
	dispose("implement", "aaa", 2, measured(time.Minute, 3), mergedAt.Add(-2*time.Hour))
	dispose("review", "bbb", 4, option.None[agentlog.RunMetrics](), mergedAt.Add(-time.Hour))
	dispose("implement", "aaa", 8, measured(time.Minute, 1), mergedAt.Add(time.Hour))
	storetest.Merged(t, store, "sandbox://CC-1", mergedAt)

	got, err := store.MergedTicketWorkflowSpend(ctx, june10)
	if err != nil {
		t.Fatalf("MergedTicketWorkflowSpend: %v", err)
	}
	want := []storepkg.WorkflowTicketSpend{{
		Ticket: "sandbox://CC-1", MergedAt: mergedAt, Workflow: "wf", WorkflowHash: "aaa",
		Steps: []storepkg.StepSpend{
			{Step: "implement", WeightUSD: 3, Metrics: agentlog.RunMetrics{
				Wall: option.Some(2 * time.Minute), Turns: 5, Tools: map[string]int{"Bash": 5},
			}},
			{Step: "review", WeightUSD: 4},
		},
	}}
	opts := cmp.AllowUnexported(option.Option[time.Duration]{}, option.Option[float64]{})
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("MergedTicketWorkflowSpend(june 10) mismatch (-want +got):\n%s", diff)
	}
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
	}}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(insight.TicketFact{}, "Title")); diff != "" {
		t.Errorf("MergedTicketFacts(cc-sandbox, june 10) mismatch (-want +got):\n%s", diff)
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
