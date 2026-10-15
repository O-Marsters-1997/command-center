package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

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

func fetchInsights(t *testing.T, server *web.Server, query string) (resp, insightsJSON) {
	t.Helper()
	response := do(t, server, http.MethodGet, "/insights.json?"+query, req{})

	var body insightsJSON
	if err := json.NewDecoder(strings.NewReader(response.Body)).Decode(&body); err != nil {
		t.Fatalf("decode /insights.json: %v", err)
	}
	return response, body
}

func TestInsightsNamesTheLocationTheServerWasGiven(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation(Europe/Paris): %v", err)
	}
	server := newServer(openStore(t), testNow, web.WithLocation(paris))

	_, body := fetchInsights(t, server, "")

	if body.Timezone != "Europe/Paris" {
		t.Errorf("timezone = %q, want Europe/Paris", body.Timezone)
	}
}

type insightsJSON struct {
	Since        string          `json:"since"`
	Until        string          `json:"until"`
	Timezone     string          `json:"timezone"`
	Points       []insightsPoint `json:"points"`
	WastePctWeek float64         `json:"waste_pct_week"`
}

type insightsPoint struct {
	Ticket   string  `json:"ticket"`
	Title    string  `json:"title"`
	MergedAt string  `json:"merged_at"`
	PctWeek  float64 `json:"pct_week"`
}

func TestHandleInsightsServesTheDocumentedShape(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
	mergeInsightsTicket(t, store, "sandbox://CC-1", now)

	server := newServer(store, now)
	resp, body := fetchInsights(t, server, "")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body.Until != "2026-09-20" {
		t.Errorf("until = %q, want 2026-09-20", body.Until)
	}
	if body.Since != "2026-08-21" {
		t.Errorf("since = %q, want 2026-08-21 (30 days back)", body.Since)
	}
	if body.Timezone == "" {
		t.Error("timezone is empty")
	}
	if len(body.Points) != 1 || body.Points[0].Ticket != "sandbox://CC-1" {
		t.Fatalf("points = %+v, want the one seeded ticket", body.Points)
	}
}

func TestHandleInsightsFallsBackToThirtyDaysOnBadSince(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	now := testNow.Add(31*24*time.Hour + 3*time.Hour)
	server := newServer(store, now)

	for _, query := range []string{"", "since=not-a-date", "since=2026-13-40"} {
		_, body := fetchInsights(t, server, query)
		if body.Since != "2026-08-21" {
			t.Errorf("query %q: since = %q, want the 30-day default 2026-08-21", query, body.Since)
		}
	}
}
