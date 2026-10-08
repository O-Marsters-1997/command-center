package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web"
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
	if err := store.ImportTickets(ctx, "project:x", seed, time.Now()); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	conflict := &storepkg.FeatureConflictError{URL: url, Existing: "project:x", Importing: "project:y"}
	if err := store.RecordImportRefusal(ctx, "project:y", conflict, time.Now()); err != nil {
		t.Fatal(err)
	}

	server := web.NewServer(store, loop.RealClock{}, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"project:y", url, "already belongs to feature"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleFeatureRedirectScopesTheBoard(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(web.NewServer(openStore(t), loop.RealClock{}, ""))
	t.Cleanup(srv.Close)

	resp, err := noRedirect(srv).Get(srv.URL + "/features/" + url.PathEscape("project:x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/?feature=project%3Ax" {
		t.Fatalf("Location = %q, want /?feature=project%%3Ax", got)
	}
}

func TestHandleImportFeatureQueuesImportAndNudgesTheLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	server := web.NewServer(store, loop.RealClock{}, "")
	var nudged atomic.Bool
	server.SetNudge(func() { nudged.Store(true) })
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/features/"+url.PathEscape("project:x")+"/import", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("HX-Request", "true")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
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

func TestHandleImportFeatureRejectsAForeignOrigin(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(web.NewServer(openStore(t), loop.RealClock{}, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/features/"+url.PathEscape("project:x")+"/import", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://evil.example")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandleImportFeatureAllowsAMissingOrigin(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(web.NewServer(openStore(t), loop.RealClock{}, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/features/"+url.PathEscape("project:x")+"/import", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleImportFeatureRedirectsWithoutHtmx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(web.NewServer(openStore(t), loop.RealClock{}, ""))
	t.Cleanup(srv.Close)

	resp := postVerb(t, srv, "/features/"+url.PathEscape("project:x")+"/import", nil)
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)
}
