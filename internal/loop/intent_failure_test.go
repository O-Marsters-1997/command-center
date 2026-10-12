package loop_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

type failingCancel struct{ *runner.Fake }

func (failingCancel) Cancel(int) error { return errors.New("no such process") }

func TestAFailingIntentIsConsumedWithAnEventAndTheTickCarriesOn(t *testing.T) {
	f := newLoopFixture(t, withMaxAgents(2), withTickets(sandboxTicket("1"), sandboxTicket("2")))
	first, second := f.Tickets[0], f.Tickets[1]
	authoriseTicket(t, f.Store, first.URL, plan.Hash(plan.Compose(plan.Ticket{URL: first.URL})), testAt)
	f.TickPastExplore(t)
	if _, ok := f.Latest(t)[first.URL]; !ok {
		t.Fatal("the first ticket never launched")
	}

	f.Loop = loop.NewLoop(f.Store, noOpObserve, fixedClock(testAt), f.Cfg, f.WS, failingCancel{f.Fake})
	if err := f.Store.QueueVerbIntent(t.Context(), first.URL, plan.VerbKill, testAt); err != nil {
		t.Fatal(err)
	}
	authoriseTicket(t, f.Store, second.URL, plan.Hash(plan.Compose(plan.Ticket{URL: second.URL})), testAt)
	spawnsBefore := len(f.Fake.Spawns)

	f.Tick(t)

	pending, err := f.Store.PendingVerbIntents(t.Context(), plan.VerbKill)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending kill intents = %d, want 0", len(pending))
	}
	failures := intentFailures(t, f, first.URL)
	if len(failures) != 1 {
		t.Fatalf("intent_failed events = %v, want 1", failures)
	}
	if !strings.HasPrefix(failures[0], plan.VerbKill+": ") || !strings.Contains(failures[0], "no such process") {
		t.Errorf("intent_failed detail = %q, want it to name the verb and the error", failures[0])
	}
	if len(f.Fake.Spawns) == spawnsBefore {
		t.Error("no later act step ran: the second ticket did not launch in the same tick")
	}

	f.Tick(t)
	if again := intentFailures(t, f, first.URL); len(again) != 1 {
		t.Errorf("intent_failed events after a second tick = %v, want still 1", again)
	}
}

func intentFailures(t *testing.T, f *loopFixture, ticketURL string) []string {
	t.Helper()
	events, err := f.Store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for _, e := range events {
		if e.Kind == "intent_failed" && e.TicketURL == ticketURL {
			details = append(details, e.Detail)
		}
	}
	return details
}
