package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestFeatures(t *testing.T) {
	t.Parallel()

	t.Run("handle features shows the last refusal", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		store := openStore(t)
		ticketURL := "https://github.com/acme/alpha/issues/1"
		seed := []storepkg.ImportedTicket{{Ticket: tracker.Ticket{URL: ticketURL, Number: 1, Title: "Add x"}, Repo: "alpha"}}
		if err := store.ImportTickets(ctx, "project:x", seed, testNow); err != nil {
			t.Fatalf("seed ImportTickets: %v", err)
		}

		conflict := &storepkg.FeatureConflictError{URL: ticketURL, Existing: "project:x", Importing: "project:y"}
		if err := store.RecordImportRefusal(ctx, "project:y", conflict.URL, conflict.Error(), testNow); err != nil {
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
	})

	t.Run("handle import feature queues import and nudges the loop", func(t *testing.T) {
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
	})

	t.Run("handle import feature carries the repo and returns to projects", func(t *testing.T) {
		t.Parallel()

		store := openStore(t)
		form := url.Values{"repo": {"acme/alpha"}, "return": {"/repos"}}
		result := do(t, newServer(store, testNow), http.MethodPost, "/features/"+url.PathEscape("project:x")+"/import",
			req{Form: form})
		if result.Code != http.StatusSeeOther || result.Location != "/repos" {
			t.Errorf("POST import = %d %q, want 303 to /repos", result.Code, result.Location)
		}
		pending, err := store.PendingVerbIntents(t.Context(), "import")
		if err != nil {
			t.Fatalf("PendingVerbIntents(import): %v", err)
		}
		if len(pending) != 1 || pending[0].Payload != "acme/alpha" {
			t.Errorf("pending import intents = %+v, want one scoped to acme/alpha", pending)
		}
		malformed := req{Form: url.Values{"repo": {"nope"}}}
		bad := do(t, newServer(store, testNow), http.MethodPost, "/features/x/import", malformed)
		if bad.Code != http.StatusBadRequest {
			t.Errorf("POST import with a malformed repo = %d, want 400", bad.Code)
		}
	})

	t.Run("handle import feature redirects without htmx", func(t *testing.T) {
		t.Parallel()

		result := do(t, newServer(openStore(t), testNow), http.MethodPost,
			"/features/"+url.PathEscape("project:x")+"/import", req{})
		if result.Code != http.StatusSeeOther || result.Location != "/" {
			t.Errorf("POST import = %d %q, want 303 to /", result.Code, result.Location)
		}
	})
}
