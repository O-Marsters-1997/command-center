package cc_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestPostTicketRequiresBrowserOrigin(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"ticket": {"sandbox://CC-2"}, "branch": {"cc-2-second"}}.Encode()
	resp, err := http.Post(srv.URL+"/ticket", "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 with no Origin header", resp.StatusCode)
	}
}

func TestPostTicketQueuesEditIntentAndRedirects(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	// sandbox://CC-2 has no worktree in seededStore, so its branch is free to change too.
	body := url.Values{
		"ticket":     {"sandbox://CC-2"},
		"branch":     {"cc-2-renamed"},
		"blocked_by": {"sandbox://CC-1", "sandbox://CC-3"},
	}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/ticket", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

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

	// The handler only ever queues an intent; the table itself is untouched until the loop runs.
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

	store := seededStore(t, time.Now()) // sandbox://CC-1 has a worktree at cc-1-first
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-renamed"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/ticket", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body2 := string(raw)
	if !strings.Contains(body2, "/repos/cc-sandbox-cc-1-first") {
		t.Errorf("refusal does not name the worktree: %s", body2)
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

	store := seededStore(t, time.Now()) // sandbox://CC-1 has a worktree
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	// Same branch, only blocked_by changes: no worktree conflict since the branch is unchanged.
	body := url.Values{"ticket": {"sandbox://CC-1"}, "branch": {"cc-1-first"}, "blocked_by": {"sandbox://CC-2"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/ticket", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

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

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct{ name, query string }{
		{name: "unknown ticket", query: "ticket=sandbox://GHOST&branch=cc-1-first"},
		{name: "missing branch", query: "ticket=sandbox://CC-1"},
		{name: "missing ticket", query: "branch=cc-1-first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/ticket?"+tt.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", srv.URL)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}
