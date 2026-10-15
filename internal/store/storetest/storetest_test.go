package storetest_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestRunSeedsDisposedAndAliveRuns(t *testing.T) {
	t.Parallel()
	st := cctest.Store(t)
	ticket := store.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	completedID := storetest.Run(t, st, storetest.RunOpts{
		TicketURL: ticket.URL, Kind: plan.RunKindAgent, Pgid: 123, StartedAt: at,
		Session: store.RunSession{StartNew: true, Message: "first run"},
		Outcome: option.Some(plan.OutcomePush), EndedAt: at.Add(time.Minute),
	})
	runs, err := st.SessionRunsForTicket(t.Context(), ticket.URL)
	if err != nil {
		t.Fatalf("SessionRunsForTicket(%q): %v", ticket.URL, err)
	}
	if len(runs) != 1 || runs[0].ID != completedID || runs[0].Message != "first run" {
		t.Errorf("SessionRunsForTicket = %+v, want the seeded run with its message", runs)
	}
	latest, err := st.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	if got := latest[plan.TicketURL(ticket.URL)].Outcome; !got.IsSome() || got.Unwrap() != plan.OutcomePush {
		t.Errorf("LatestRunsByTicket outcome = %v, want push", got)
	}
	storetest.Run(t, st, storetest.RunOpts{TicketURL: ticket.URL, Kind: plan.RunKindAgent, StartedAt: at})
	latest, err = st.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket after alive run: %v", err)
	}
	if got := latest[plan.TicketURL(ticket.URL)].Outcome; got.IsSome() {
		t.Errorf("alive run outcome = %v, want none", got)
	}
}

func TestTicketsAndPushedSeedTheStore(t *testing.T) {
	t.Parallel()
	st := cctest.Store(t)
	ticket := store.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	obs, found, err := st.LastObservation(t.Context())
	if err != nil || !found {
		t.Fatalf("LastObservation = %+v, %v, %v; want saved empty observation", obs, found, err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	storetest.Pushed(t, st, storetest.PushedOpts{
		URL: ticket.URL, Tip: "head", Base: "main", BaseTip: "base", At: at,
	})
	pushes, err := st.LatestPushes(t.Context())
	if err != nil {
		t.Fatalf("LatestPushes: %v", err)
	}
	if got := pushes[plan.TicketURL(ticket.URL)]; got.PushedTip != "head" || got.BaseSHAAtPush != "base" {
		t.Errorf("LatestPushes[%s] = %+v, want head/base", ticket.URL, got)
	}
}
