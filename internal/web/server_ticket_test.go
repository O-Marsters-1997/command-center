package web_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestPostTicketQueuesEditIntentAndRedirects(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	server := newServer(store, testNow)

	form := url.Values{
		"ticket":     {"sandbox://CC-2"},
		"branch":     {"cc-2-renamed"},
		"blocked_by": {"sandbox://CC-1", "sandbox://CC-3"},
	}
	response := do(t, server, http.MethodPost, "/ticket", req{Form: form})
	if response.Code != http.StatusSeeOther || response.Location != "/" {
		t.Errorf("POST /ticket = (%d, %q), want (303, /)", response.Code, response.Location)
	}

	pending, err := store.PendingEditTicketIntents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-2" {
		t.Fatalf("pending edit intents = %+v, want exactly one for sandbox://CC-2", pending)
	}
	if pending[0].Branch != "cc-2-renamed" {
		t.Errorf("branch = %q, want cc-2-renamed", pending[0].Branch)
	}
	if !slices.Equal(pending[0].BlockedBy, []string{"sandbox://CC-1", "sandbox://CC-3"}) {
		t.Errorf("blocked_by = %v", pending[0].BlockedBy)
	}

	tickets, err := store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, ticket := range tickets {
		if ticket.URL == "sandbox://CC-2" && ticket.Branch != "cc-2-second" {
			t.Errorf("ticket row changed by the handler itself: branch = %q", ticket.Branch)
		}
	}
}

func TestPostTicketRefusesBranchChangeWhenWorktreeExists(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	server := newServer(store, testNow)
	form := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-renamed"}}
	response := do(t, server, http.MethodPost, "/ticket", req{Form: form})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if !strings.Contains(response.Body, "/repos/cc-sandbox-cc-1-first") {
		t.Errorf("refusal does not name the worktree: %s", response.Body)
	}

	pending, err := store.PendingEditTicketIntents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending edit intents = %+v, want none: the refusal must not queue anything", pending)
	}
}

func TestPostTicketAllowsBlockedByEditWithoutTouchingBranchCheck(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	server := newServer(store, testNow)
	form := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-first"}, "blocked_by": {"sandbox://CC-2"}}
	response := do(t, server, http.MethodPost, "/ticket", req{Form: form})
	if response.Code != http.StatusSeeOther || response.Location != "/" {
		t.Errorf("POST /ticket = (%d, %q), want (303, /)", response.Code, response.Location)
	}

	pending, err := store.PendingEditTicketIntents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
		t.Fatalf("pending edit intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
}

func TestPostTicketRejectsUnknownTicketOrMissingFields(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)

	tests := []struct{ name, query string }{
		{name: "unknown ticket", query: "ticket=sandbox://GHOST&branch=cc-1-first"},
		{name: "missing branch", query: "ticket=sandbox://CC-1"},
		{name: "missing ticket", query: "branch=cc-1-first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := do(t, server, http.MethodPost, "/ticket?"+tt.query, req{})
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", response.Code)
			}
		})
	}
}

func TestSessionPageRendersEditFormPrefilled(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	storetest.Tickets(t, st, []storepkg.Ticket{
		{URL: "https://github.com/o/r/issues/1", Repo: "o/r", Branch: "cc-1-first"},
		{
			URL: "https://github.com/o/r/issues/2", Repo: "o/r", Branch: "cc-2-second",
			BlockedBy: []string{"https://github.com/o/r/issues/1"},
		},
	}...)

	page := renderPath(t, newServer(st, testNow), "/s/o/r/2")

	form := htmltest.Parse(t, page).One(t, `form[action="/ticket"]`)
	for name, value := range map[string]string{
		"ticket": "https://github.com/o/r/issues/2", "branch": "cc-2-second",
		"blocked_by": "https://github.com/o/r/issues/1",
	} {
		form.One(t, `input[name="`+name+`"][value="`+value+`"]`)
	}
}
