package cc_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func TestRecordMergedEventsAppendsOnceWithGitHubsMergeTime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	tickAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	observed := cc.Observation{
		PRs: map[string]gh.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: gh.Merged, MergedAt: mergedAt},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (cc.Observation, error) { return observed, nil },
		fixedClock(tickAt), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	merges := mergedEvents(events)
	if len(merges) != 1 {
		t.Fatalf("pr_merged events = %d, want exactly 1: %+v", len(merges), events)
	}
	if merges[0].TicketURL != ticket.URL {
		t.Errorf("ticket url = %q, want %q", merges[0].TicketURL, ticket.URL)
	}
	if !merges[0].At.Equal(mergedAt) {
		t.Errorf("event time = %s, want GitHub's own merge time %s, not the tick clock %s",
			merges[0].At, mergedAt, tickAt)
	}

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	events, err = store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 1 {
		t.Errorf("pr_merged events after a second tick = %d, want still 1", got)
	}
}

func TestRecordMergedEventsDedupeSurvivesAClearedMetaTable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	mergedAt := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	observed := cc.Observation{
		PRs: map[string]gh.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: gh.Merged, MergedAt: mergedAt},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (cc.Observation, error) { return observed, nil },
		fixedClock(mergedAt.Add(time.Hour)), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`DELETE FROM meta`); err != nil {
		t.Fatalf("simulate a restart onto a reset meta table: %v", err)
	}

	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce after clearing meta: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 1 {
		t.Errorf("pr_merged events after a cleared meta table = %d, want still 1: %+v", got, events)
	}
}

func TestRecordMergedEventsSkipsAnUnmergedPR(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	observed := cc.Observation{
		PRs: map[string]gh.PR{
			cc.BranchKey("cc-sandbox", "cc-1-first"): {Number: 41, State: gh.Closed},
		},
	}
	loop := cc.NewLoop(store,
		func(context.Context) (cc.Observation, error) { return observed, nil },
		fixedClock(time.Now()), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := len(mergedEvents(events)); got != 0 {
		t.Errorf("pr_merged events for a closed-unmerged PR = %d, want 0: %+v", got, events)
	}
}

func mergedEvents(events []cc.Event) []cc.Event {
	var merges []cc.Event
	for _, e := range events {
		if e.Kind == "pr_merged" {
			merges = append(merges, e)
		}
	}
	return merges
}
