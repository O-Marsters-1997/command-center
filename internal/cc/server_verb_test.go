package cc_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestVerbRejectsBadOriginAndMethod(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
	}{
		{name: "GET is rejected before origin is even checked",
			method: http.MethodGet, origin: srv.URL, wantStatus: http.StatusMethodNotAllowed},
		{name: "a missing Origin is rejected",
			method: http.MethodPost, origin: "", wantStatus: http.StatusForbidden},
		{name: "a foreign Origin is rejected",
			method: http.MethodPost, origin: "http://evil.example", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+"/verb?verb=kill&task=sandbox://CC-1", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestVerbQueuesExactlyOneKillIntent(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?verb=kill&task=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "sandbox://CC-1" {
		t.Fatalf("pending kill intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
}

func TestVerbQueuesExactlyOneCancelIntent(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?verb=cancel&task=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

	pending, err := store.PendingVerbIntents(t.Context(), "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "sandbox://CC-1" {
		t.Fatalf("pending cancel intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
}

func TestVerbRejectsUnknownTaskOrUnsupportedVerb(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct{ name, query string }{
		{name: "unknown task", query: "verb=kill&task=sandbox://GHOST"},
		{name: "unsupported verb", query: "verb=bogus-verb&task=sandbox://CC-1"},
		{name: "missing verb", query: "task=sandbox://CC-1"},
		{name: "missing task", query: "verb=kill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?"+tt.query, nil)
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

func TestVerbAcceptsFormEncodedFields(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	// A handler reading only the query string sees neither of these.
	body := url.Values{"verb": {"kill"}, "task": {"sandbox://CC-1"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb", strings.NewReader(body))
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

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "sandbox://CC-1" {
		t.Fatalf("pending kill intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
}

// TestVerbLandsTheBrowserBackOnTheBoard is the redirect from the browser's side: the default
// client, like a browser, follows the 303 with a GET and ends up on the page.
func TestVerbLandsTheBrowserBackOnTheBoard(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?verb=kill&task=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after following the redirect = %d, want 200", resp.StatusCode)
	}
	if resp.Request.Method != http.MethodGet || resp.Request.URL.Path != "/" {
		t.Errorf("followed with %s %s, want GET /", resp.Request.Method, resp.Request.URL.Path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<h1>Command Centre</h1>") {
		t.Errorf("body after the redirect is not the board: %s", body)
	}
}
