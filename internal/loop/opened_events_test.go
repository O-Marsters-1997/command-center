package loop_test

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestRecordOpenedEventsAppendsOnceWhenAPRAppears(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	observed := plan.Observation{}
	lp := loop.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)), config.Config{}, config.Workspace{},
		runner.ProcessRunner{})

	runOnce := func() []storepkg.Event {
		t.Helper()
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		events, err := store.Events(ctx)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		return openedEvents(events)
	}

	if got := runOnce(); len(got) != 0 {
		t.Fatalf("pr_opened events before any PR = %+v, want none", got)
	}

	observed = plan.Observation{PRs: map[string]plan.PR{
		plan.BranchKey(ticket.Repo, ticket.Branch): {Number: 41, State: plan.Open},
	}}
	got := runOnce()
	if len(got) != 1 {
		t.Fatalf("pr_opened events after the PR appeared = %+v, want exactly 1", got)
	}
	if want := "PR #41 https://github.com/owner/cc-sandbox/pull/41"; got[0].Detail != want {
		t.Errorf("detail = %q, want %q", got[0].Detail, want)
	}
	if got[0].TicketURL != ticket.URL {
		t.Errorf("ticket url = %q, want %q", got[0].TicketURL, ticket.URL)
	}

	if got := runOnce(); len(got) != 1 {
		t.Errorf("pr_opened events after an unchanged tick = %d, want still 1", len(got))
	}
}

func TestRecordOpenedEventsIgnoresAPRAlreadyInThePreviousObservation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "owner/cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	observed := plan.Observation{PRs: map[string]plan.PR{
		plan.BranchKey(ticket.Repo, ticket.Branch): {Number: 41, State: plan.Open},
	}}
	if err := store.SaveObservation(ctx, observed); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	lp := loop.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return observed, nil },
		fixedClock(time.Now()), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got := openedEvents(events); len(got) != 0 {
		t.Errorf("pr_opened events after a restart with an unchanged observation = %+v, want none", got)
	}
}

func openedEvents(events []storepkg.Event) []storepkg.Event {
	var opened []storepkg.Event
	for _, e := range events {
		if e.Kind == "pr_opened" {
			opened = append(opened, e)
		}
	}
	return opened
}
