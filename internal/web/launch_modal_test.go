package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
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
	if got := htmltest.Parse(t, body).One(t, "[hx-get]").Text(); !strings.Contains(got, "importing project:x") {
		t.Errorf("pending fragment text = %q, want importing project:x", got)
	}
	if !nudged.Load() {
		t.Error("handleLaunchOpen did not nudge the loop")
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatalf("PendingVerbIntents(import): %v", err)
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
	root := htmltest.Parse(t, response.Body)
	dialog := root.One(t, "dialog#launch-modal-body")
	dialog.One(t, `cc-launch-modal[tickets="ticket=sandbox%3A%2F%2FCC-1&ticket=sandbox%3A%2F%2FCC-2"]`)
	if got := dialog.Text(); strings.Contains(got, "importing") {
		t.Errorf("ticket-based modal queues an import: %s", dialog.HTML())
	}
	if nudged.Load() {
		t.Error("a ticket-based open must not nudge the loop -- nothing needs importing")
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatalf("PendingVerbIntents(import): %v", err)
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

func TestHandleCandidatesFragment(t *testing.T) {
	t.Parallel()
	type message struct {
		Feature, Importing, Refused, Empty bool
	}
	type modal struct {
		Feature, Tickets                     string
		ContainsImport, ContainsCandidateURL bool
	}
	type outcome struct {
		Status, Buttons, Cancels int
		Titles                   []string
		DialogsOpen              []bool
		EmbedsCandidateURL       bool
		Polling                  []string
		Messages                 []message
		Modals                   []modal
		Scripts                  []string
	}
	const featurePath = "/launch/candidates?feature=project%3Ax"
	const ticketQuery = "ticket=sandbox%3A%2F%2FCC-1&ticket=sandbox%3A%2F%2FCC-2"
	featureTitle := []string{"Launch project:x"}
	featureModal := []modal{{Feature: "project:x"}}
	featureScript := []string{"/assets/dist/launch-modal.js"}
	noSeed := func(*testing.T, *storepkg.Store) {}
	cases := []struct {
		name, path string
		seed       func(*testing.T, *storepkg.Store)
		want       outcome
	}{
		{
			name: "pending import", path: featurePath,
			seed: func(t *testing.T, store *storepkg.Store) {
				t.Helper()
				if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
					Ticket: "project:x", Verb: "import", At: testNow,
				}); err != nil {
					t.Fatalf("QueueVerbIntent(project:x, import): %v", err)
				}
			},
			want: outcome{Status: http.StatusOK, Titles: featureTitle, DialogsOpen: []bool{true},
				Polling: []string{featurePath}, Messages: []message{{Feature: true, Importing: true}}, Cancels: 1},
		},
		{
			name: "imported feature mounts island", path: featurePath,
			seed: func(t *testing.T, store *storepkg.Store) {
				t.Helper()
				storetest.Tickets(t, store,
					storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
					storepkg.Ticket{
						URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:x",
						BlockedBy: []string{"sandbox://CC-1"},
					},
				)
			},
			want: outcome{Status: http.StatusOK, Titles: featureTitle, DialogsOpen: []bool{false},
				Modals: featureModal, Scripts: featureScript},
		},
		{
			name: "ticket slice mounts island", path: "/launch/candidates?" + ticketQuery,
			seed: func(t *testing.T, store *storepkg.Store) {
				t.Helper()
				storetest.Tickets(t, store,
					storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
					storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:y"},
				)
			},
			want: outcome{Status: http.StatusOK, Titles: []string{"Launch tickets"}, DialogsOpen: []bool{false},
				Modals: []modal{{Tickets: ticketQuery}}, Scripts: featureScript},
		},
		{
			name: "refusal", path: featurePath,
			seed: func(t *testing.T, store *storepkg.Store) {
				t.Helper()
				claimed := "https://github.com/acme/alpha/issues/1"
				storetest.Tickets(t, store, storepkg.Ticket{URL: claimed, Repo: "alpha", Branch: "b", Feature: "project:y"})
				conflict := &storepkg.FeatureConflictError{URL: claimed, Existing: "project:y", Importing: "project:x"}
				if err := store.RecordImportRefusal(t.Context(), "project:x", conflict, testNow); err != nil {
					t.Fatalf("RecordImportRefusal(project:x): %v", err)
				}
			},
			want: outcome{Status: http.StatusOK, Titles: featureTitle, DialogsOpen: []bool{true},
				Messages: []message{{Feature: true, Refused: true}}, Cancels: 1},
		},
		{
			name: "candidates outrank stale refusal", path: featurePath,
			seed: func(t *testing.T, store *storepkg.Store) {
				t.Helper()
				storetest.Tickets(t, store, storepkg.Ticket{
					URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
				})
				conflict := &storepkg.FeatureConflictError{URL: "sandbox://CC-1", Existing: "project:y", Importing: "project:x"}
				if err := store.RecordImportRefusal(t.Context(), "project:x", conflict, testNow); err != nil {
					t.Fatalf("RecordImportRefusal(project:x): %v", err)
				}
			},
			want: outcome{Status: http.StatusOK, Titles: featureTitle, DialogsOpen: []bool{false},
				Modals: featureModal, Scripts: featureScript},
		},
		{
			name: "empty feature", path: featurePath,
			seed: noSeed,
			want: outcome{Status: http.StatusOK, Titles: featureTitle, DialogsOpen: []bool{true},
				Messages: []message{{Feature: true, Empty: true}}, Cancels: 1},
		},
		{
			name: "unknown ticket", path: "/launch/candidates?ticket=sandbox%3A%2F%2FGHOST",
			seed: noSeed,
			want: outcome{Status: http.StatusUnprocessableEntity},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			tt.seed(t, store)
			response := do(t, newServer(store, testNow), http.MethodGet, tt.path,
				req{Header: http.Header{"Hx-Request": {"true"}}})
			root := htmltest.Parse(t, response.Body)
			got := outcome{Status: response.Code, Buttons: len(root.Find("button")),
				Cancels: len(root.Find(`a[href="/f/project:x"]`))}
			for _, node := range root.Find("dialog#launch-modal-body") {
				_, open := node.Attr("open")
				got.DialogsOpen = append(got.DialogsOpen, open)
				got.EmbedsCandidateURL = strings.Contains(node.HTML(), "sandbox://CC-1")
			}
			for _, node := range root.Find("h2#launch-dialog-title") {
				got.Titles = append(got.Titles, node.Text())
			}
			for _, node := range root.Find("[hx-get]") {
				value, _ := node.Attr("hx-get")
				got.Polling = append(got.Polling, value)
			}
			for _, node := range root.Find("dialog p") {
				text := node.Text()
				got.Messages = append(got.Messages, message{
					Feature: strings.Contains(text, "project:x"), Importing: strings.Contains(text, "importing project:x"),
					Refused: strings.Contains(text, "already belongs to feature"),
					Empty:   strings.Contains(text, "no tickets to launch"),
				})
			}
			for _, node := range root.Find("cc-launch-modal") {
				feature, _ := node.Attr("feature")
				tickets, _ := node.Attr("tickets")
				got.Modals = append(got.Modals, modal{Feature: feature, Tickets: tickets,
					ContainsImport:       strings.Contains(node.HTML(), "importing"),
					ContainsCandidateURL: strings.Contains(node.HTML(), "sandbox://CC-1")})
			}
			for _, node := range root.Find(`script[type="module"]`) {
				src, _ := node.Attr("src")
				got.Scripts = append(got.Scripts, src)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("candidates fragment (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHandleCandidatesJSONAnswersJSONRegardlessOfHeaders(t *testing.T) {
	t.Parallel()

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
			t.Parallel()
			response := do(t, srv, http.MethodGet, "/launch/candidates.json?feature=project%3Ax", req{Header: tt.header})
			if ct := response.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

func TestLaunchRoutesRejectMissingSelection(t *testing.T) {
	t.Parallel()
	server := newServer(openStore(t), testNow)
	for _, tt := range []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{name: "open", method: http.MethodPost, path: "/launch/open", form: url.Values{}},
		{name: "candidates", method: http.MethodGet, path: "/launch/candidates"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := do(t, server, tt.method, tt.path, req{Form: tt.form})
			if response.Code != http.StatusBadRequest {
				t.Errorf("%s %s status = %d, want 400", tt.method, tt.path, response.Code)
			}
			if !strings.Contains(response.Body, "either feature or at least one ticket is required") {
				t.Errorf("%s %s body = %q, want missing-selection error", tt.method, tt.path, response.Body)
			}
		})
	}
}
