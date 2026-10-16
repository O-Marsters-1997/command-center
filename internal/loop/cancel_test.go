package loop_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestCancel(t *testing.T) {
	t.Parallel()
	f := newLoopFixture(t,
		withTickets(sandboxTicket("1"), sandboxTicket("2"), sandboxTicket("3"), sandboxTicket("4")))
	for _, ticket := range f.Tickets {
		launch := storepkg.LaunchRequest{
			Ticket: ticket.URL, PromptHash: composeHash(ticket.URL), Group: "group-a", At: testAt,
		}
		if err := f.Store.QueueLaunchIntent(t.Context(), launch); err != nil {
			t.Fatalf("QueueLaunchIntent(%s): %v", ticket.URL, err)
		}
	}
	f.Tick(t)
	if got := len(f.Fake.Spawns()); got != 1 {
		t.Fatalf("spawns after launch = %d, want 1 (max_agents = 1)", got)
	}
	var running string
	for ticketURL, summary := range f.Latest(t) {
		if summary.Pgid.IsSome() {
			running = ticketURL
		}
	}
	runningPgid := f.Latest(t)[running].Pgid.Unwrap()
	sibling := f.Tickets[0].URL
	if sibling == running {
		sibling = f.Tickets[1].URL
	}
	if err := f.Store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: sibling, Verb: "cancel", At: testAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("QueueVerbIntent(cancel): %v", err)
	}

	f.Tick(t)

	if got := len(f.Fake.Spawns()); got != 1 {
		t.Errorf("spawns after cancel = %d, want 1", got)
	}
	if got := f.Fake.Canceled(); len(got) != 0 || !f.Fake.Alive(runningPgid) {
		t.Errorf("canceled = %v, running alive = %v, want the live member left running", got, f.Fake.Alive(runningPgid))
	}
	if f.Latest(t)[running].Outcome.IsSome() {
		t.Error("running member has an outcome, want it untouched")
	}
	memberships, err := f.Store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatalf("LaunchMemberships(): %v", err)
	}
	for _, ticket := range f.Tickets {
		if !memberships[plan.TicketURL(ticket.URL)].Cancelled {
			t.Errorf("LaunchMemberships()[%s].Cancelled = false, want the whole launch cancelled", ticket.URL)
		}
	}
	if !hasEventIn(t, f.Store, "launch_cancelled", "4") {
		t.Error("Events() has no launch_cancelled naming 4 members")
	}
}

func hasEventIn(t *testing.T, store *storepkg.Store, kind storepkg.EventKind, detail string) bool {
	t.Helper()
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	for _, e := range events {
		if e.Kind == kind && strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}
