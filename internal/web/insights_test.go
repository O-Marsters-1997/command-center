package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
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
		Ticket: ticketURL, Kind: kind, PromptHash: "hash-" + ticketURL,
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

type insightsJSON struct {
	Since        string          `json:"since"`
	Until        string          `json:"until"`
	Timezone     string          `json:"timezone"`
	Points       []insightsPoint `json:"points"`
	WastePctWeek *float64        `json:"waste_pct_week"`
}

type insightsPoint struct {
	Ticket   string  `json:"ticket"`
	Title    string  `json:"title"`
	MergedAt string  `json:"merged_at"`
	PctWeek  float64 `json:"pct_week"`
}

func fetchInsights(t *testing.T, server *web.Server, query string) (resp, insightsJSON) {
	t.Helper()
	response := do(t, server, http.MethodGet, "/insights.json?"+query, req{})

	var body insightsJSON
	if err := json.NewDecoder(strings.NewReader(response.Body)).Decode(&body); err != nil {
		t.Fatalf("decode /insights.json?%s: %v", query, err)
	}
	return response, body
}

func TestInsightsJSON(t *testing.T) {
	t.Parallel()
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)

	mergedStore := func(t *testing.T) *storepkg.Store {
		t.Helper()
		store := openStore(t)
		insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
		disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
		mergeInsightsTicket(t, store, "sandbox://CC-1", now)
		return store
	}

	t.Run("serves the documented shape", func(t *testing.T) {
		t.Parallel()
		resp, body := fetchInsights(t, newServer(mergedStore(t), now), "")

		if resp.Code != http.StatusOK {
			t.Fatalf("GET /insights.json status = %d, want 200", resp.Code)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /insights.json Content-Type = %q, want application/json", ct)
		}
		if body.Since != "2026-08-21" || body.Until != "2026-09-20" || body.Timezone == "" || body.WastePctWeek == nil {
			t.Errorf("GET /insights.json = %+v, want since 2026-08-21, until 2026-09-20, a timezone and a waste total", body)
		}
		var tickets []string
		for _, p := range body.Points {
			tickets = append(tickets, p.Ticket)
		}
		if diff := cmp.Diff([]string{"sandbox://CC-1"}, tickets); diff != "" {
			t.Errorf("GET /insights.json point tickets mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an unknown repo scope has no points", func(t *testing.T) {
		t.Parallel()
		_, body := fetchInsights(t, newServer(mergedStore(t), now), "repo=not-a-repo")

		if body.Points == nil || len(body.Points) != 0 {
			t.Errorf("GET /insights.json?repo=not-a-repo points = %+v, want an empty list", body.Points)
		}
	})

	t.Run("names the local location", func(t *testing.T) {
		t.Parallel()
		_, body := fetchInsights(t, newServer(openStore(t), testNow), "")

		loc, err := time.LoadLocation(body.Timezone)
		if err != nil {
			t.Fatalf("LoadLocation(%q): %v", body.Timezone, err)
		}
		_, gotOffset := testNow.In(loc).Zone()
		_, wantOffset := testNow.In(time.Local).Zone()
		if gotOffset != wantOffset {
			t.Errorf("GET /insights.json timezone %q offset = %d, want local offset %d", body.Timezone, gotOffset, wantOffset)
		}
	})

	t.Run("a bad since falls back to thirty days", func(t *testing.T) {
		t.Parallel()
		server := newServer(openStore(t), now)

		for _, query := range []string{"", "since=not-a-date", "since=2026-13-40"} {
			if _, body := fetchInsights(t, server, query); body.Since != "2026-08-21" {
				t.Errorf("GET /insights.json?%s since = %q, want 2026-08-21", query, body.Since)
			}
		}
	})

	t.Run("a valid since is kept", func(t *testing.T) {
		t.Parallel()
		if _, body := fetchInsights(t, newServer(openStore(t), now), "since=2026-07-01"); body.Since != "2026-07-01" {
			t.Errorf("GET /insights.json?since=2026-07-01 since = %q, want 2026-07-01", body.Since)
		}
	})
}
