package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type fakeTrackerSource struct{ features []tracker.Feature }

func (f fakeTrackerSource) Features(context.Context) ([]tracker.Feature, error) {
	return f.features, nil
}

func (fakeTrackerSource) Tickets(context.Context, string) ([]tracker.Ticket, error) {
	return nil, nil
}

func resolveByRemote(byRemote map[string]tracker.Source) tracker.Resolver {
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := byRemote[remote]
		if !ok {
			return nil, fmt.Errorf("resolveByRemote: no source for %q", remote)
		}
		return src, nil
	}
}

func TestHandleFeaturesShowsTheLastRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	url := "https://github.com/acme/alpha/issues/1"
	seed := []storepkg.ImportedTicket{{Ticket: tracker.Ticket{URL: url, Number: 1, Title: "Add x"}, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", seed, testNow); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	conflict := &storepkg.FeatureConflictError{URL: url, Existing: "project:x", Importing: "project:y"}
	if err := store.RecordImportRefusal(ctx, "project:y", conflict, testNow); err != nil {
		t.Fatal(err)
	}

	result := do(t, newServer(store, testNow), http.MethodGet, "/repos", req{})
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", result.Code, result.Body)
	}
	body := result.Body
	for _, want := range []string{"project:y", url, "already belongs to feature"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
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
		t.Fatal(err)
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
