package loop_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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
	storetest.Verb(t, f.Store, storepkg.VerbRequest{
		Ticket: sibling, Verb: "cancel", At: testAt.Add(time.Second),
	})

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
	memberships := planInput(t, f.Store).Memberships
	for _, ticket := range f.Tickets {
		got := memberships[plan.TicketURL(ticket.URL)]
		if disarmed := ticket.URL == sibling; disarmed == (got.LaunchID != 0) {
			t.Errorf("LaunchMemberships()[%s] = %+v, want only %s disarmed", ticket.URL, got, sibling)
		}
	}
	if !hasEvent(storetest.Events(t, f.Store, ""), "launch_cancelled", "disarmed") {
		t.Error("Events() has no launch_cancelled recording the disarm")
	}
}
