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
	storetest.Tickets(t, store, storepkg.Ticket{URL: url, Repo: repo, Branch: "branch-" + url, Feature: feature})
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
	appendEvents(t, store, storepkg.Event{At: mergedAt, TicketURL: ticketURL, Kind: storepkg.EventPRMerged})
}

func withdraw(t *testing.T, store *storepkg.Store, ticketURL string, at time.Time, merged bool) {
	t.Helper()
	if err := store.WithdrawTicket(t.Context(), ticketURL, at, merged); err != nil {
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
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-2*time.Hour), 1.00)
		disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(-2*time.Hour), 0.10)
		mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

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
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", mergedAt.Add(-time.Hour), 1.00)
		disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", mergedAt.Add(time.Hour), 5.00)
		mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

		if points := mergedSpend(t, store); len(points) != 1 || points[0].TotalUSD() != 1.00 {
			t.Errorf("MergedTicketSpend() = %+v, want the one pre-merge run only", points)
		}
	})

	t.Run("excludes unmerged tickets", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", mergedAt, 3.00)
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
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", at.Add(-time.Hour), 1.00)
		disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", at.Add(time.Hour), 5.00)
		mergeInsightsTicket(t, store, "sandbox://CC-1", at)

		got, ok := boardSpend(t, store, "")["sandbox://CC-1"]
		if !ok || !got.Merged || got.TotalUSD() != 1.00 {
			t.Errorf("BoardTicketSpend()[CC-1] = %+v, %v, want merged with total 1.00", got, ok)
		}
	})

	t.Run("an open ticket sums its spend so far", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", at, 1.00)
		disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", at, 0.50)

		got, ok := boardSpend(t, store, "")["sandbox://CC-1"]
		if !ok || got.Merged || got.AgentUSD != 1.00 || got.FollowUpUSD != 0.50 {
			t.Errorf("BoardTicketSpend()[CC-1] = %+v, %v, want unmerged with agent 1.00, follow-up 0.50", got, ok)
		}
	})

	t.Run("scopes by repo", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		insightsTicket(t, store, "sandbox://CC-2", "cc-other", "feat-b")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", at, 1.00)
		disposeInsightsRun(t, store, "sandbox://CC-2", "agent", at, 9.00)

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

	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", day, 2.00)
	withdraw(t, store, "sandbox://CC-1", day, false)

	insightsTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-a")
	disposeInsightsRun(t, store, "sandbox://CC-2", "agent", day.Add(-time.Hour), 9.00)
	mergeInsightsTicket(t, store, "sandbox://CC-2", day)
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
