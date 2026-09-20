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
	t *testing.T, store *cc.Store, ticketURL string, endedAt time.Time, tokensIn, tokensOut int64, settled bool,
) {
	t.Helper()
	ctx := t.Context()
	runID, err := store.InsertRunSkeleton(ctx, ticketURL, "agent", "deadbeef", "hash-"+ticketURL)
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	metrics := &agentlog.RunMetrics{TokensIn: tokensIn, TokensOut: tokensOut, Settled: settled}
	if err := store.RecordDisposition(ctx, runID, plan.OutcomePush, nil, endedAt, metrics); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func civilDay(t *testing.T, year int, month time.Month, day int) time.Time {
	t.Helper()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func bucketByDay(t *testing.T, buckets []cc.InsightsBucket) map[string]cc.InsightsBucket {
	t.Helper()
	byDay := make(map[string]cc.InsightsBucket, len(buckets))
	for _, b := range buckets {
		byDay[b.Day.Format("2006-01-02")] = b
	}
	return byDay
}

// TestRunInsightsZeroFillsDaysWithNoRuns covers the range's own quiet days: generate_series left-
// joined against runs must still produce a row for a day nothing happened on, not a gap.
func TestRunInsightsZeroFillsDaysWithNoRuns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	disposeInsightsRun(t, store, "sandbox://CC-1", time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC), 100, 20, true)
	disposeInsightsRun(t, store, "sandbox://CC-1", time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC), 50, 10, true)

	buckets, err := store.RunInsights(ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 12))
	if err != nil {
		t.Fatalf("RunInsights: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("buckets = %d, want 3 (one per day in range)", len(buckets))
	}
	byDay := bucketByDay(t, buckets)
	quiet := byDay["2026-06-11"]
	if quiet.Runs != 0 || quiet.TokensIn != 0 || quiet.TokensOut != 0 || quiet.Unsettled != 0 {
		t.Errorf("quiet day = %+v, want a zero-valued bucket", quiet)
	}
	if got := byDay["2026-06-10"]; got.Runs != 1 || got.TokensIn != 100 || got.TokensOut != 20 {
		t.Errorf("2026-06-10 = %+v, want one run of 100/20", got)
	}
}

// TestRunInsightsBucketsByLocalDayNotUTC is the ticket's own boundary case: two runs land on the
// same UTC calendar day but either side of local midnight in Europe/London (BST, UTC+1 in June),
// so bucketing by UTC would wrongly merge them into one day.
func TestRunInsightsBucketsByLocalDayNotUTC(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")

	// 2026-06-15 22:30 UTC = 2026-06-15 23:30 BST -- still the 15th, locally.
	disposeInsightsRun(t, store, "sandbox://CC-1", time.Date(2026, 6, 15, 22, 30, 0, 0, time.UTC), 111, 11, true)
	// 2026-06-15 23:30 UTC = 2026-06-16 00:30 BST -- already the 16th, locally.
	disposeInsightsRun(t, store, "sandbox://CC-1", time.Date(2026, 6, 15, 23, 30, 0, 0, time.UTC), 222, 22, true)

	london := mustLoadLocation(t, "Europe/London")
	buckets, err := store.RunInsights(
		ctx, "", "", london.String(), civilDay(t, 2026, 6, 15), civilDay(t, 2026, 6, 16),
	)
	if err != nil {
		t.Fatalf("RunInsights: %v", err)
	}
	byDay := bucketByDay(t, buckets)
	if got := byDay["2026-06-15"]; got.Runs != 1 || got.TokensIn != 111 {
		t.Errorf("2026-06-15 = %+v, want exactly the pre-midnight run", got)
	}
	if got := byDay["2026-06-16"]; got.Runs != 1 || got.TokensIn != 222 {
		t.Errorf("2026-06-16 = %+v, want exactly the post-midnight run", got)
	}
}

// TestRunInsightsCountsUnsettledSeparately covers a killed run's partial metrics
// (metrics_settled = false): its tokens still land in the totals, and it also increments the
// bucket's own unsettled count, but a fully-settled run never does.
func TestRunInsightsCountsUnsettledSeparately(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", day, 100, 10, true)
	disposeInsightsRun(t, store, "sandbox://CC-1", day, 50, 5, false)

	buckets, err := store.RunInsights(ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10))
	if err != nil {
		t.Fatalf("RunInsights: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(buckets))
	}
	got := buckets[0]
	if got.Runs != 2 || got.TokensIn != 150 || got.TokensOut != 15 {
		t.Errorf("totals = %+v, want both runs summed", got)
	}
	if got.Unsettled != 1 {
		t.Errorf("unsettled = %d, want 1", got.Unsettled)
	}
}

// TestRunInsightsFeatureMatchesTheTicketsOwnColumn covers the grain decision that ADR 13's
// closure over unmerged blockers does not apply to insights: filtering by ?feature= must exclude
// a run whose own ticket belongs to a different feature, blocker or not.
func TestRunInsightsFeatureMatchesTheTicketsOwnColumn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	insightsTicket(t, store, "sandbox://CC-2", "cc-sandbox", "feat-b")
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", day, 100, 10, true)
	disposeInsightsRun(t, store, "sandbox://CC-2", day, 999, 999, true)

	buckets, err := store.RunInsights(ctx, "", "feat-a", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10))
	if err != nil {
		t.Fatalf("RunInsights: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Runs != 1 || buckets[0].TokensIn != 100 {
		t.Fatalf("buckets = %+v, want only feat-a's run", buckets)
	}
}

// TestRunInsightsIncludesWithdrawnTicketRuns covers the other grain decision: a withdrawn ticket
// (remove-worktree's own doing) keeps its runs in every total, breaking the withdrawn_at IS NULL
// convention every other read follows.
func TestRunInsightsIncludesWithdrawnTicketRuns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	day := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", day, 100, 10, true)

	if err := store.WithdrawTicket(ctx, "sandbox://CC-1", day, false); err != nil {
		t.Fatalf("WithdrawTicket: %v", err)
	}

	buckets, err := store.RunInsights(ctx, "", "", "UTC", civilDay(t, 2026, 6, 10), civilDay(t, 2026, 6, 10))
	if err != nil {
		t.Fatalf("RunInsights: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Runs != 1 || buckets[0].TokensIn != 100 {
		t.Fatalf("buckets = %+v, want the withdrawn ticket's run still counted", buckets)
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
	Since    string           `json:"since"`
	Until    string           `json:"until"`
	Timezone string           `json:"timezone"`
	Buckets  []insightsBucket `json:"buckets"`
}

type insightsBucket struct {
	Day       string `json:"day"`
	TokensIn  int64  `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
	Runs      int64  `json:"runs"`
	Unsettled int64  `json:"unsettled"`
}

// TestHandleInsightsServesTheDocumentedShape covers the route's wire contract end to end: content
// type, top-level fields and one populated bucket for a run seeded on the clock's own day.
func TestHandleInsightsServesTheDocumentedShape(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	insightsTicket(t, store, "sandbox://CC-1", "cc-sandbox", "feat-a")
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	disposeInsightsRun(t, store, "sandbox://CC-1", now, 412000, 18400, true)
	_ = ctx

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
	var today *insightsBucket
	for i, b := range body.Buckets {
		if b.Day == "2026-09-20" {
			today = &body.Buckets[i]
		}
	}
	if today == nil {
		t.Fatalf("no bucket for 2026-09-20 in %+v", body.Buckets)
	}
	if today.Runs != 1 || today.TokensIn != 412000 || today.TokensOut != 18400 || today.Unsettled != 0 {
		t.Errorf("today = %+v, want the one seeded run", today)
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
