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

// TestHandleLaunchOpenQueuesImportAndNudgesTheLoop covers ADR 14's opening step: the intent lands
// as an unconsumed "import" verb intent named after the feature, the loop is nudged, and the
// response is the modal's own pending fragment rather than the JSON candidate set.
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

// TestHandleLaunchOpenRejectsAMissingOrigin covers requireBrowserOrigin: opening the modal
// writes, so it is gated exactly like every other mutating handler.
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

// TestHandleCandidatesFragmentStillPendingWhileImportUnconsumed covers the poll's first answer:
// an intent queued but not yet applied by a tick.
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

// TestHandleCandidatesFragmentShowsTheCandidateSetOnceImported covers the poll's terminal
// success answer, once the loop has applied the import.
func TestHandleCandidatesFragmentShowsTheCandidateSetOnceImported(t *testing.T) {
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
	wants := []string{"sandbox://CC-1", "sandbox://CC-2", "<td>now</td>", "<td>on unlock</td>", "origin/main"}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("candidate fragment does not contain %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "hx-get=") {
		t.Errorf("terminal candidate fragment must not keep polling:\n%s", body)
	}
}

// TestHandleCandidatesFragmentShowsARefusalNamingTheFeature covers the poll's other terminal
// answer: a closure or two-feature refusal that left no ticket imported.
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

// TestHandleCandidatesFragmentPrefersCandidatesOverAStaleRefusal covers a feature that failed to
// reimport but already had tickets from an earlier successful import: LastImportError is not
// cleared by a later success (store.go), so the fragment must prefer the real candidate set it
// can show over a refusal that may no longer describe the feature's current state.
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
	if !strings.Contains(body, "sandbox://CC-1") {
		t.Errorf("fragment = %q, want the existing candidate shown despite the stale refusal", body)
	}
	if strings.Contains(body, "already belongs to feature") {
		t.Errorf("fragment = %q, want the stale refusal not to shadow a real candidate set", body)
	}
}

// TestHandleCandidatesDefaultsToJSONWithoutHtmx covers content negotiation: everyone but the
// modal's own poll, including a plain http client, still gets #257's JSON.
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

// TestHandleCandidatesFragmentShowsAnAlreadyAuthorisedMemberAsRefused covers relaunching a
// feature with an active launch: a member already authorised renders refused, naming the launch,
// rather than offering it again.
func TestHandleCandidatesFragmentShowsAnAlreadyAuthorisedMemberAsRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := getModalFragment(t, srv, "/launch/candidates?feature=project%3Ax")
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if !strings.Contains(body, "already authorised in launch") {
		t.Errorf("fragment = %q, want the already-authorised member labelled refused", body)
	}
	if strings.Contains(body, `name="ticket" value="sandbox://CC-1"`) {
		t.Errorf("fragment = %q, want no re-launch field for an already-authorised member", body)
	}
}

// TestHandleCandidatesFragmentShowsAnEmptyFeatureWithoutOfferingConfirm covers a feature that
// imported cleanly but has no tickets: the poll must not fall into a fourth, unhandled state that
// renders a confirm button with no ticket fields for POST /launch to reject.
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
