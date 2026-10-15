package store_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestStore_RefreshFacts(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	appendEvents(t, st,
		storepkg.Event{At: testNow, Kind: storepkg.EventRefreshRefused, Detail: "no ticket"},
		storepkg.Event{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventRefreshRefused, Detail: "conflict"},
	)

	facts, err := st.RefreshFacts(t.Context())
	if err != nil {
		t.Fatalf("RefreshFacts: %v", err)
	}
	if _, ok := facts[""]; ok {
		t.Errorf("RefreshFacts() = %+v, want no ticketless entry", facts)
	}
	if got := facts[plan.TicketURL(ticket.URL)]; !got.Refused || got.Reason != "conflict" {
		t.Errorf("RefreshFacts()[CC-1] = %+v, want refused with reason %q", got, "conflict")
	}
}
