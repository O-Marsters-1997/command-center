package store_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func insightsTicket(t *testing.T, store *storepkg.Store, url, repo, feature string) {
	t.Helper()
	ticket := storepkg.Ticket{URL: url, Repo: repo, Branch: "branch-" + url, Feature: feature}
	storetest.Tickets(t, store, ticket)
}

func disposeInsightsRun(
	t *testing.T, store *storepkg.Store, ticketURL string, kind plan.RunKind, endedAt time.Time, costUSD float64,
) {
	t.Helper()
	ctx := t.Context()
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: kind, BaselineSHA: "deadbeef", PromptHash: "hash-" + ticketURL,
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	if err := store.RecordDisposition(ctx, storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.None[int](),
		Cost: option.Some(costUSD), EndedAt: endedAt,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
}

func mergeInsightsTicket(t *testing.T, store *storepkg.Store, ticketURL string, mergedAt time.Time) {
	t.Helper()
	err := store.AppendEvent(t.Context(), storepkg.Event{At: mergedAt, TicketURL: ticketURL, Kind: "pr_merged"})
	if err != nil {
		t.Fatalf("AppendEvent pr_merged: %v", err)
	}
}

func civilDay(t *testing.T, year int, month time.Month, day int) time.Time {
	t.Helper()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestMergedTicketSpendSumsAllRunKinds(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	before := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", before, 1.00)
	disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", before, 0.10)
	mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

	points, err := store.MergedTicketSpend(ctx, storepkg.SpendQuery{
		Loc: time.UTC, Since: civilDay(t, 2026, 6, 10), Until: civilDay(t, 2026, 6, 10),
	})
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1: %+v", len(points), points)
	}
	got := points[0]
	if got.AgentUSD != 1.00 || got.FollowUpUSD != 0.10 {
		t.Errorf("kind split = %+v, want 1.00/0.10", got)
	}
	if got.TotalUSD() != 1.10 {
		t.Errorf("TotalUSD() = %v, want 1.10", got.TotalUSD())
	}
}

func TestMergedTicketSpendExcludesRunsDisposedAfterMerge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-time.Hour), 1.00)
	disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(time.Hour), 5.00)
	mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

	points, err := store.MergedTicketSpend(ctx, storepkg.SpendQuery{
		Loc: time.UTC, Since: civilDay(t, 2026, 6, 10), Until: civilDay(t, 2026, 6, 10),
	})
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 1 || points[0].TotalUSD() != 1.00 {
		t.Fatalf("points = %+v, want the one pre-merge run only", points)
	}
}

func TestMergedTicketSpendExcludesUnmergedTickets(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", day, 3.00)
	if err := store.WithdrawTicket(ctx, "sandbox://CC-1", day, false); err != nil {
		t.Fatalf("WithdrawTicket: %v", err)
	}

	points, err := store.MergedTicketSpend(ctx, storepkg.SpendQuery{
		Loc: time.UTC, Since: civilDay(t, 2026, 6, 10), Until: civilDay(t, 2026, 6, 10),
	})
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("points = %+v, want none for a withdrawn ticket with no pr_merged event", points)
	}
}

func TestTicketSpendWeighsAMergedTicketTheSameAsMergedTicketSpend(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	mergedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-time.Hour), 1.00)
	disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(time.Hour), 5.00)
	mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

	byURL, err := store.BoardTicketSpend(ctx, "", "")
	if err != nil {
		t.Fatalf("TicketSpend: %v", err)
	}
	got, ok := byURL["sandbox://CC-1"]
	if !ok {
		t.Fatalf("byURL = %+v, want an entry for CC-1", byURL)
	}
	if !got.Merged {
		t.Error("Merged = false, want true")
	}
	if got.TotalUSD() != 1.00 {
		t.Errorf("TotalUSD() = %v, want 1.00 (the pre-merge run only, same as insights)", got.TotalUSD())
	}
}

func TestTicketSpendSumsSpendSoFarForAnOpenTicket(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now, 1.00)
	disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", now, 0.50)

	byURL, err := store.BoardTicketSpend(ctx, "", "")
	if err != nil {
		t.Fatalf("TicketSpend: %v", err)
	}
	got, ok := byURL["sandbox://CC-1"]
	if !ok {
		t.Fatalf("byURL = %+v, want an entry for CC-1", byURL)
	}
	if got.Merged {
		t.Error("Merged = true, want false: no pr_merged event")
	}
	if got.AgentUSD != 1.00 || got.FollowUpUSD != 0.50 {
		t.Errorf("kind split = %+v, want 1.00/0.50", got)
	}
}

func TestTicketSpendScopesByRepoAndFeature(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	insightsTicket(t, store, "sandbox://CC-2", "cc-other", "feat-b")
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", time.Now(), 1.00)
	disposeInsightsRun(t, store, "sandbox://CC-2", "agent", time.Now(), 9.00)

	byURL, err := store.BoardTicketSpend(ctx, "cc-sandbox", "")
	if err != nil {
		t.Fatalf("TicketSpend: %v", err)
	}
	if _, ok := byURL["sandbox://CC-2"]; ok {
		t.Errorf("byURL = %+v, want CC-2 excluded by repo scope", byURL)
	}
	if _, ok := byURL["sandbox://CC-1"]; !ok {
		t.Errorf("byURL = %+v, want CC-1 included", byURL)
	}
}

func TestWithdrawnTicketWasteSumsOnlyUnmergedWithdrawals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", day, 2.00)
	if err := store.WithdrawTicket(ctx, "sandbox://CC-1", day, false); err != nil {
		t.Fatalf("WithdrawTicket CC-1: %v", err)
	}

	insightsTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-a")
	disposeInsightsRun(t, store, "sandbox://CC-2", "agent", day.Add(-time.Hour), 9.00)
	mergeInsightsTicket(t, store, "sandbox://CC-2", day)
	if err := store.WithdrawTicket(ctx, "sandbox://CC-2", day, true); err != nil {
		t.Fatalf("WithdrawTicket CC-2: %v", err)
	}

	waste, err := store.WithdrawnTicketWaste(ctx, storepkg.SpendQuery{
		Loc: time.UTC, Since: civilDay(t, 2026, 6, 10), Until: civilDay(t, 2026, 6, 10),
	})
	if err != nil {
		t.Fatalf("WithdrawnTicketWaste: %v", err)
	}
	if waste != 2.00 {
		t.Errorf("waste = %v, want 2.00 (only CC-1, the unmerged withdrawal)", waste)
	}

	points, err := store.MergedTicketSpend(ctx, storepkg.SpendQuery{
		Loc: time.UTC, Since: civilDay(t, 2026, 6, 10), Until: civilDay(t, 2026, 6, 10),
	})
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 1 || points[0].Ticket != "sandbox://CC-2" {
		t.Fatalf("points = %+v, want only CC-2, the merged ticket", points)
	}
}
