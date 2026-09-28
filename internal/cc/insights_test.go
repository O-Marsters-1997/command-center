package cc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// insightsTicket upserts one ticket with an explicit repo and feature, since seedOneTicket leaves
// both blank.
func insightsTicket(t *testing.T, store *cc.Store, url, repo, feature string) {
	t.Helper()
	ticket := cc.Ticket{URL: url, Repo: repo, Branch: "branch-" + url, Feature: feature}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
}

func disposeInsightsRun(
	t *testing.T, store *cc.Store, ticketURL, kind string, endedAt time.Time, costUSD float64,
) {
	t.Helper()
	ctx := t.Context()
	runID, err := store.InsertRunSkeleton(ctx, ticketURL, kind, "deadbeef", "hash-"+ticketURL)
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	metrics := &agentlog.RunMetrics{CostUSD: &costUSD, Settled: true}
	if err := store.RecordDisposition(ctx, runID, plan.OutcomePush, nil, endedAt, metrics); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
}

func mergeInsightsTicket(t *testing.T, store *cc.Store, ticketURL string, mergedAt time.Time) {
	t.Helper()
	err := store.AppendEvent(t.Context(), cc.Event{At: mergedAt, TicketURL: ticketURL, Kind: "pr_merged"})
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
	disposeInsightsRun(t, store, "sandbox://CC-1", "resolve", before, 0.25)
	disposeInsightsRun(t, store, "sandbox://CC-1", "follow_up", before, 0.10)
	mergeInsightsTicket(t, store, "sandbox://CC-1", mergedAt)

	points, err := store.MergedTicketSpend(
		ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10),
	)
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1: %+v", len(points), points)
	}
	got := points[0]
	if got.AgentUSD != 1.00 || got.ResolveUSD != 0.25 || got.FollowUpUSD != 0.10 {
		t.Errorf("kind split = %+v, want 1.00/0.25/0.10", got)
	}
	if got.TotalUSD() != 1.35 {
		t.Errorf("TotalUSD() = %v, want 1.35", got.TotalUSD())
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

	points, err := store.MergedTicketSpend(
		ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10),
	)
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

	points, err := store.MergedTicketSpend(
		ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10),
	)
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
	disposeInsightsRun(t, store, "sandbox://CC-1", "resolve", now, 0.50)

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
	if got.AgentUSD != 1.00 || got.ResolveUSD != 0.50 {
		t.Errorf("kind split = %+v, want 1.00/0.50", got)
	}
}

func TestTicketSpendApportionsExploreCostAcrossLaunchMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, ticket := range tickets {
		if err := store.QueueLaunchIntent(ctx, ticket.URL, "hash-"+ticket.URL, "group-a", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	pending, err := store.PendingExploreRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending explore runs = %+v, want exactly 1 for the one launch", pending)
	}
	cost := 3.00
	metrics := &agentlog.RunMetrics{CostUSD: &cost, Settled: true}
	err = store.RecordDisposition(ctx, pending[0].ID, plan.OutcomePush, nil, at.Add(time.Minute), metrics)
	if err != nil {
		t.Fatal(err)
	}

	byURL, err := store.BoardTicketSpend(ctx, "", "")
	if err != nil {
		t.Fatalf("BoardTicketSpend: %v", err)
	}
	for _, ticket := range tickets {
		if got := byURL[ticket.URL].AgentUSD; got != 1.00 {
			t.Errorf("%s AgentUSD = %v, want 1.00 (a third of the explore run's $3.00)", ticket.URL, got)
		}
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

	waste, err := store.WithdrawnTicketWaste(ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10))
	if err != nil {
		t.Fatalf("WithdrawnTicketWaste: %v", err)
	}
	if waste != 2.00 {
		t.Errorf("waste = %v, want 2.00 (only CC-1, the unmerged withdrawal)", waste)
	}

	points, err := store.MergedTicketSpend(
		ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10),
	)
	if err != nil {
		t.Fatalf("MergedTicketSpend: %v", err)
	}
	if len(points) != 1 || points[0].Ticket != "sandbox://CC-2" {
		t.Fatalf("points = %+v, want only CC-2, the merged ticket", points)
	}
}

func fetchInsights(t *testing.T, server *cc.Server, query string) (*http.Response, insightsJSON) {
	t.Helper()
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/insights.json?" + query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	var body insightsJSON
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /insights.json: %v", err)
	}
	return resp, body
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
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", "agent", now.Add(-time.Hour), 4.20)
	mergeInsightsTicket(t, store, "sandbox://CC-1", now)

	server := cc.NewServer(store, fixedClock(now), nil, "")
	resp, body := fetchInsights(t, server, "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
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

// TestHandleInsightsFallsBackToThirtyDaysOnBadSince covers both an absent and an unparseable
// ?since=, following normalizeLogFilter's rule that a bad query value is silently the default
// rather than a 400.
func TestHandleInsightsFallsBackToThirtyDaysOnBadSince(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	for _, query := range []string{"", "since=not-a-date", "since=2026-13-40"} {
		_, body := fetchInsights(t, server, query)
		if body.Since != "2026-08-21" {
			t.Errorf("query %q: since = %q, want the 30-day default 2026-08-21", query, body.Since)
		}
	}
}
