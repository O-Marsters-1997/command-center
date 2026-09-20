package cc_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func getModalFragment(t *testing.T, srv *httptest.Server, target string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, srv.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHandleLaunchOpenQueuesImportAndNudgesTheLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	server := cc.NewServer(store, time.Now, nil, "")
	var nudged atomic.Bool
	server.SetNudge(func() { nudged.Store(true) })
	srv := httptest.NewServer(server)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch/open", strings.NewReader("feature=project%3Ax"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "importing project:x") {
		t.Errorf("open response is not the pending fragment:\n%s", body)
	}
	if !nudged.Load() {
		t.Error("handleLaunchOpen did not nudge the loop")
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "project:x" {
		t.Fatalf("pending import intents = %+v, want one queued for project:x", pending)
	}
}

func TestHandleLaunchOpenRejectsAMissingOrigin(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(openStore(t), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/launch/open", "application/x-www-form-urlencoded", strings.NewReader("feature=x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandleCandidatesFragmentStillPendingWhileImportUnconsumed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.QueueVerbIntent(ctx, "project:x", "import", time.Now()); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, "importing project:x") {
		t.Errorf("still-pending poll = %q, want the pending fragment", body)
	}
	if !strings.Contains(body, `hx-get="/launch/candidates?feature=project%3Ax"`) {
		t.Errorf("pending fragment does not keep polling itself:\n%s", body)
	}
}

func TestHandleCandidatesFragmentMountsTheIslandOnceImported(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
		{
			URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:x",
			BlockedBy: []string{"sandbox://CC-1"},
		},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, `<cc-launch-modal feature="project:x">`) {
		t.Errorf("ready fragment does not mount the island:\n%s", body)
	}
	if !strings.Contains(body, `<script type="module" src="/assets/dist/launch-modal.js">`) {
		t.Errorf("ready fragment does not load the island's script:\n%s", body)
	}
	if strings.Contains(body, "sandbox://CC-1") {
		t.Errorf("ready fragment embeds candidate data, want the island to fetch its own JSON:\n%s", body)
	}
	if strings.Contains(body, "hx-get=") {
		t.Errorf("terminal candidate fragment must not keep polling:\n%s", body)
	}
}

func TestHandleCandidatesFragmentShowsARefusalNamingTheFeature(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	// The refused ticket must exist for the refusal event's own FK; it belongs to project:y,
	// leaving project:x itself with no imported ticket to show instead of the refusal.
	claimed := "https://github.com/acme/alpha/issues/1"
	seed := []cc.Ticket{{URL: claimed, Repo: "alpha", Branch: "b", Feature: "project:y"}}
	if err := store.UpsertTickets(ctx, seed); err != nil {
		t.Fatal(err)
	}
	conflict := &cc.FeatureConflictError{URL: claimed, Existing: "project:y", Importing: "project:x"}
	if err := store.RecordImportRefusal(ctx, "project:x", conflict, time.Now()); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, "project:x") || !strings.Contains(body, "already belongs to feature") {
		t.Errorf("refused fragment = %q, want it to name the feature and the refusal", body)
	}
	if strings.Contains(body, "hx-get=") {
		t.Errorf("terminal refused fragment must not keep polling:\n%s", body)
	}
}

func TestHandleCandidatesFragmentPrefersCandidatesOverAStaleRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
	}); err != nil {
		t.Fatal(err)
	}
	conflict := &cc.FeatureConflictError{URL: "sandbox://CC-1", Existing: "project:y", Importing: "project:x"}
	if err := store.RecordImportRefusal(ctx, "project:x", conflict, time.Now()); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, `<cc-launch-modal feature="project:x">`) {
		t.Errorf("fragment = %q, want the island mounted despite the stale refusal", body)
	}
	if strings.Contains(body, "already belongs to feature") {
		t.Errorf("fragment = %q, want the stale refusal not to shadow a real candidate set", body)
	}
}

func TestHandleCandidatesDefaultsToJSONWithoutHtmx(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/launch/candidates?feature=project%3Ax")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json without an HX-Request header", ct)
	}
}

func TestHandleCandidatesFragmentShowsAnEmptyFeatureWithoutOfferingConfirm(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(openStore(t), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, "no tickets to launch") {
		t.Errorf("fragment = %q, want the empty-feature message", body)
	}
	if strings.Contains(body, "[ confirm ]") {
		t.Errorf("fragment = %q, want no confirm button with zero candidates", body)
	}
}
