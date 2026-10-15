package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestHandleFeaturesShowsTheLastRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticketURL := "https://github.com/acme/alpha/issues/1"
	seed := []storepkg.ImportedTicket{{Ticket: tracker.Ticket{URL: ticketURL, Number: 1, Title: "Add x"}, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", seed, testNow); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	conflict := &storepkg.FeatureConflictError{URL: ticketURL, Existing: "project:x", Importing: "project:y"}
	if err := store.RecordImportRefusal(ctx, "project:y", conflict, testNow); err != nil {
		t.Fatalf("RecordImportRefusal(project:y): %v", err)
	}

	result := do(t, newServer(store, testNow), http.MethodGet, "/repos", req{})
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Code)
	}
	body := result.Body
	root := htmltest.Parse(t, body).One(t, "main#main")
	text := root.Text()
	if !strings.Contains(text, "project:y") || !strings.Contains(text, ticketURL) ||
		!strings.Contains(text, "already belongs to feature") {
		t.Error("refusal text missing feature, ticket URL, or already belongs to feature")
	}
}

func TestHandleImportFeatureQueuesImportAndNudgesTheLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	var nudged atomic.Bool
	server := openNudgingServer(store, func() { nudged.Store(true) })
	result := do(t, server, http.MethodPost, "/features/"+url.PathEscape("project:x")+"/import", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Code)
	}
	if !nudged.Load() {
		t.Error("handleImportFeature did not nudge the loop")
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatalf("PendingVerbIntents(import): %v", err)
	}
	if len(pending) != 1 || pending[0].TicketID != "project:x" {
		t.Fatalf("pending import intents = %+v, want one queued for project:x", pending)
	}
}

func TestHandleImportFeatureRedirectsWithoutHtmx(t *testing.T) {
	t.Parallel()

	result := do(t, newServer(openStore(t), testNow), http.MethodPost,
		"/features/"+url.PathEscape("project:x")+"/import", req{})
	if result.Code != http.StatusSeeOther || result.Location != "/" {
		t.Errorf("POST import = %d %q, want 303 to /", result.Code, result.Location)
	}
}
