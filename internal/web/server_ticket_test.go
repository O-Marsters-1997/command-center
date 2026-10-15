package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPostTicket(t *testing.T) {
	t.Parallel()

	t.Run("from the session", testPostTicketFromSession)

	t.Run("queues an edit intent and leaves the ticket to the loop", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		form := url.Values{
			"ticket": {"sandbox://CC-2"}, "branch": {"cc-2-renamed"}, "blocked_by": {"sandbox://CC-1", "sandbox://CC-3"},
		}

		response := do(t, newServer(store, testNow), http.MethodPost, "/ticket", req{Form: form})
		if response.Code != http.StatusSeeOther || response.Location != "/" {
			t.Errorf("POST /ticket = %d %q, want 303 /", response.Code, response.Location)
		}
		pending, err := store.PendingEditTicketIntents(t.Context())
		if err != nil {
			t.Fatalf("PendingEditTicketIntents: %v", err)
		}
		type edit struct {
			Ticket, Branch string
			BlockedBy      []string
		}
		var got []edit
		for _, p := range pending {
			got = append(got, edit{Ticket: p.TicketID, Branch: p.Branch, BlockedBy: p.BlockedBy})
		}
		want := []edit{{
			Ticket: "sandbox://CC-2", Branch: "cc-2-renamed", BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-3"},
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("pending edit intents mismatch (-want +got):\n%s", diff)
		}
		tickets, err := store.Tickets(t.Context())
		if err != nil {
			t.Fatalf("Tickets: %v", err)
		}
		for _, ticket := range tickets {
			if ticket.URL == "sandbox://CC-2" && ticket.Branch != "cc-2-second" {
				t.Errorf("POST /ticket changed CC-2's branch to %q itself, want the loop to apply it", ticket.Branch)
			}
		}
	})

	t.Run("refuses a branch change while a worktree exists", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		form := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-renamed"}}

		response := do(t, newServer(store, testNow), http.MethodPost, "/ticket", req{Form: form})
		if response.Code != http.StatusConflict {
			t.Fatalf("POST /ticket branch change status = %d, want 409", response.Code)
		}
		if !strings.Contains(response.Body, "/repos/cc-sandbox-cc-1-first") {
			t.Errorf("POST /ticket 409 body = %q, want it to name the worktree", response.Body)
		}
		pending, err := store.PendingEditTicketIntents(t.Context())
		if err != nil {
			t.Fatalf("PendingEditTicketIntents: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending edit intents = %+v, want none", pending)
		}
	})

	t.Run("allows a blocked-by edit beside a worktree", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		form := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-first"}, "blocked_by": {"sandbox://CC-2"}}

		response := do(t, newServer(store, testNow), http.MethodPost, "/ticket", req{Form: form})
		if response.Code != http.StatusSeeOther || response.Location != "/" {
			t.Errorf("POST /ticket = %d %q, want 303 /", response.Code, response.Location)
		}
		pending, err := store.PendingEditTicketIntents(t.Context())
		if err != nil {
			t.Fatalf("PendingEditTicketIntents: %v", err)
		}
		if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
			t.Errorf("pending edit intents = %+v, want one for sandbox://CC-1", pending)
		}
	})

	t.Run("rejects an unknown ticket or missing fields", func(t *testing.T) {
		t.Parallel()
		server := newServer(seededStore(t, testNow), testNow)
		for _, tt := range []struct{ name, query string }{
			{name: "unknown ticket", query: "ticket=sandbox://GHOST&branch=cc-1-first"},
			{name: "missing branch", query: "ticket=sandbox://CC-1"},
			{name: "missing ticket", query: "branch=cc-1-first"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if response := do(t, server, http.MethodPost, "/ticket?"+tt.query, req{}); response.Code != http.StatusBadRequest {
					t.Errorf("POST /ticket?%s status = %d, want 400", tt.query, response.Code)
				}
			})
		}
	})
}
