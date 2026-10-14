package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestMergeFactSurvivesAClearedMetaTable(t *testing.T) {
	t.Parallel()
	dsn := cctest.DSN(t)
	st := openStoreAt(t, dsn)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	obs := plan.NewObservation(at)
	obs.PRs[ticket.Ref()] = plan.PR{Number: 41, State: plan.Merged, MergedAt: at}
	lp := loop.New(loop.Deps{
		Logger: slog.New(slog.DiscardHandler), Store: st, Clock: loop.RealClock{}, Runner: runner.ProcessRunner{},
		Observe: func(context.Context) (plan.Observation, error) { return obs, nil },
		Config:  config.Config{}, Workspace: config.Workspace{},
	})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	events, err := st.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	merges := 0
	for _, event := range events {
		if event.Kind == "pr_merged" && event.TicketURL == ticket.URL {
			merges++
		}
	}
	if merges != 1 {
		t.Fatalf("pr_merged events after first tick = %d, want 1: %+v", merges, events)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := db.ExecContext(t.Context(), "DELETE FROM meta"); err != nil {
		t.Fatal(err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce after clearing meta: %v", err)
	}
	events, err = st.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	merges = 0
	for _, event := range events {
		if event.Kind == "pr_merged" && event.TicketURL == ticket.URL {
			merges++
		}
	}
	if merges != 1 {
		t.Errorf("pr_merged events after clearing meta = %d, want 1: %+v", merges, events)
	}
}

func TestRestackedSinceLastPushSkipsANullTicketLaunchEvent(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, event := range []storepkg.Event{
		{At: at, Kind: "restacked"},
		{At: at, TicketURL: ticket.URL, Kind: "restacked"},
	} {
		if err := st.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	restacked, err := st.RestackedSinceLastPush(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if restacked[""] || !restacked[plan.TicketURL(ticket.URL)] {
		t.Errorf("RestackedSinceLastPush = %+v, want only %s", restacked, ticket.URL)
	}
}

func TestPushFactsSkipsANullTicketLaunchEvent(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, event := range []storepkg.Event{
		{At: at, Kind: "push_refused", Detail: "no ticket"},
		{At: at, TicketURL: ticket.URL, Kind: "push_refused", Detail: "policy hit"},
	} {
		if err := st.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := st.PushFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := facts[""]; ok {
		t.Errorf("PushFacts = %+v, want no empty ticket", facts)
	}
	if got := facts[plan.TicketURL(ticket.URL)]; !got.Refused || got.RefusedPath != "policy hit" {
		t.Errorf("PushFacts[%s] = %+v, want refused policy hit", ticket.URL, got)
	}
}

func TestRefreshFactsSkipsANullTicketLaunchEvent(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, event := range []storepkg.Event{
		{At: at, Kind: "refresh_refused", Detail: "no ticket"},
		{At: at, TicketURL: ticket.URL, Kind: "refresh_refused", Detail: "conflict"},
	} {
		if err := st.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := st.RefreshFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := facts[""]; ok {
		t.Errorf("RefreshFacts = %+v, want no empty ticket", facts)
	}
	if got := facts[plan.TicketURL(ticket.URL)]; !got.Refused || got.Reason != "conflict" {
		t.Errorf("RefreshFacts[%s] = %+v, want refused conflict", ticket.URL, got)
	}
}
