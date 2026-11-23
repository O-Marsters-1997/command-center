package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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
