package store_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func appendEvents(t *testing.T, store *storepkg.Store, events ...storepkg.Event) {
	t.Helper()
	for _, event := range events {
		if err := store.AppendEvent(t.Context(), event); err != nil {
			t.Fatalf("AppendEvent(%s): %v", event.Kind, err)
		}
	}
}

func TestStore_RestackedSinceLastPush(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	appendEvents(t, st,
		storepkg.Event{At: testNow, Kind: storepkg.EventRestacked},
		storepkg.Event{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventRestacked},
	)

	got, err := st.RestackedSinceLastPush(t.Context())
	if err != nil {
		t.Fatalf("RestackedSinceLastPush: %v", err)
	}
	if diff := cmp.Diff(map[plan.TicketURL]bool{"sandbox://CC-1": true}, got); diff != "" {
		t.Errorf("RestackedSinceLastPush() with a ticketless event mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_PushFacts(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	other := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"}
	storetest.Tickets(t, st, ticket, other)
	appendEvents(t, st,
		storepkg.Event{At: testNow, Kind: storepkg.EventPushRefused, Detail: "no ticket"},
		storepkg.Event{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventPushRefused, Detail: "policy hit"},
		storepkg.Event{
			At: testNow, TicketURL: other.URL, Kind: storepkg.EventPushRefused,
			Detail: `[{"path":".env.local","rule":".env*","tier":"denied"},{"path":".npmrc","rule":".npmrc","tier":"denied"}]`,
		},
	)

	facts, err := st.PushFacts(t.Context())
	if err != nil {
		t.Fatalf("PushFacts: %v", err)
	}
	if _, ok := facts[""]; ok {
		t.Errorf("PushFacts() = %+v, want no ticketless entry", facts)
	}
	want := plan.PushFact{Refused: true, Denied: []plan.PathMatch{{Path: "policy hit"}}}
	if diff := cmp.Diff(want, facts[plan.TicketURL(ticket.URL)]); diff != "" {
		t.Errorf("PushFacts()[CC-1] mismatch (-want +got):\n%s", diff)
	}
	wantJSON := plan.PushFact{Refused: true, Denied: []plan.PathMatch{
		{Path: ".env.local", Rule: ".env*", Tier: plan.TierDenied},
		{Path: ".npmrc", Rule: ".npmrc", Tier: plan.TierDenied},
	}}
	if diff := cmp.Diff(wantJSON, facts[plan.TicketURL(other.URL)]); diff != "" {
		t.Errorf("PushFacts()[CC-2] mismatch (-want +got):\n%s", diff)
	}
}
