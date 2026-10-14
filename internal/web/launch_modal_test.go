package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestHandleLaunchOpenQueuesImportAndNudgesTheLoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	var nudged atomic.Bool
	server := openNudgingServer(store, func() { nudged.Store(true) })
	response := do(t, server, http.MethodPost, "/launch/open", req{
		Form: url.Values{"feature": {"project:x"}}, Header: http.Header{"Hx-Request": {"true"}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body
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

func TestHandleLaunchOpenMountsATicketSliceWithoutImportingOrNudging(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:y"},
	}
	storetest.Tickets(t, store, tickets...)

	var nudged atomic.Bool
	server := openNudgingServer(store, func() { nudged.Store(true) })
	response := do(t, server, http.MethodPost, "/launch/open", req{
		Form:   url.Values{"ticket": {"sandbox://CC-1", "sandbox://CC-2"}},
		Header: http.Header{"Hx-Request": {"true"}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	got := response.Body
	want := `<cc-launch-modal tickets="ticket=sandbox%3A%2F%2FCC-1&amp;ticket=sandbox%3A%2F%2FCC-2">`
	if !strings.Contains(got, want) {
		t.Errorf("open response = %q, want it to mount the island by ticket:\n%s", want, got)
	}
	if strings.Contains(got, "importing") {
		t.Errorf("a ticket-based open must not queue an import:\n%s", got)
	}
	if nudged.Load() {
		t.Error("a ticket-based open must not nudge the loop -- nothing needs importing")
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending import intents = %+v, want none", pending)
	}
}

func TestHandleLaunchOpenRejectsAnUnknownTicket(t *testing.T) {
	t.Parallel()

	srv := newServer(openStore(t), testNow)
	response := do(t, srv, http.MethodPost, "/launch/open", req{Form: url.Values{"ticket": {"sandbox://GHOST"}}})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestHandleCandidatesFragmentStillPendingWhileImportUnconsumed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.QueueVerbIntent(ctx, "project:x", "import", testNow); err != nil {
		t.Fatal(err)
	}

	srv := newServer(store, testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?feature=project%3Ax", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := response.Body
	if !strings.Contains(body, "importing project:x") {
		t.Errorf("still-pending poll = %q, want the pending fragment", body)
	}
	if !strings.Contains(body, `hx-get="/launch/candidates?feature=project%3Ax"`) {
		t.Errorf("pending fragment does not keep polling itself:\n%s", body)
	}
}

func TestHandleCandidatesFragmentMountsTheIslandOnceImported(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
		{
			URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:x",
			BlockedBy: []string{"sandbox://CC-1"},
		},
	}
	storetest.Tickets(t, store, tickets...)

	srv := newServer(store, testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?feature=project%3Ax", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := response.Body
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

func TestHandleCandidatesFragmentMountsATicketSlice(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:y"},
	}
	storetest.Tickets(t, store, tickets...)

	srv := newServer(store, testNow)

	response := do(t, srv, http.MethodGet,
		"/launch/candidates?ticket=sandbox%3A%2F%2FCC-1&ticket=sandbox%3A%2F%2FCC-2",
		req{Header: http.Header{"Hx-Request": {"true"}}})
	body := response.Body
	want := `<cc-launch-modal tickets="ticket=sandbox%3A%2F%2FCC-1&amp;ticket=sandbox%3A%2F%2FCC-2">`
	if !strings.Contains(body, want) {
		t.Errorf("ready fragment = %q, want it to mount the island by ticket:\n%s", want, body)
	}
	if strings.Contains(body, "hx-get=") {
		t.Errorf("terminal candidate fragment must not keep polling:\n%s", body)
	}
}

func TestHandleCandidatesFragmentRejectsAnUnknownTicket(t *testing.T) {
	t.Parallel()

	srv := newServer(openStore(t), testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?ticket=sandbox%3A%2F%2FGHOST", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", response.Code)
	}
}

func TestHandleCandidatesFragmentShowsARefusalNamingTheFeature(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	claimed := "https://github.com/acme/alpha/issues/1"
	seed := []storepkg.Ticket{{URL: claimed, Repo: "alpha", Branch: "b", Feature: "project:y"}}
	storetest.Tickets(t, store, seed...)
	conflict := &storepkg.FeatureConflictError{URL: claimed, Existing: "project:y", Importing: "project:x"}
	if err := store.RecordImportRefusal(ctx, "project:x", conflict, testNow); err != nil {
		t.Fatal(err)
	}

	srv := newServer(store, testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?feature=project%3Ax", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := response.Body
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
	storetest.Tickets(t, store, storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
	})
	conflict := &storepkg.FeatureConflictError{URL: "sandbox://CC-1", Existing: "project:y", Importing: "project:x"}
	if err := store.RecordImportRefusal(ctx, "project:x", conflict, testNow); err != nil {
		t.Fatal(err)
	}

	srv := newServer(store, testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?feature=project%3Ax", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := response.Body
	if !strings.Contains(body, `<cc-launch-modal feature="project:x">`) {
		t.Errorf("fragment = %q, want the island mounted despite the stale refusal", body)
	}
	if strings.Contains(body, "already belongs to feature") {
		t.Errorf("fragment = %q, want the stale refusal not to shadow a real candidate set", body)
	}
}

func TestHandleCandidatesJSONAnswersJSONRegardlessOfHeaders(t *testing.T) {
	store := openStore(t)
	storetest.Tickets(t, store, storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
	})

	srv := newServer(store, testNow)

	for _, tt := range []struct {
		name   string
		header http.Header
	}{
		{name: "without HX-Request"},
		{name: "with HX-Request", header: http.Header{"Hx-Request": {"true"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := do(t, srv, http.MethodGet, "/launch/candidates.json?feature=project%3Ax", req{Header: tt.header})
			if ct := response.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

func TestHandleCandidatesFragmentShowsAnEmptyFeatureWithoutOfferingConfirm(t *testing.T) {
	t.Parallel()

	srv := newServer(openStore(t), testNow)

	response := do(t, srv, http.MethodGet, "/launch/candidates?feature=project%3Ax", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := response.Body
	if !strings.Contains(body, "no tickets to launch") {
		t.Errorf("fragment = %q, want the empty-feature message", body)
	}
	if strings.Contains(body, "[ confirm ]") {
		t.Errorf("fragment = %q, want no confirm button with zero candidates", body)
	}
}
