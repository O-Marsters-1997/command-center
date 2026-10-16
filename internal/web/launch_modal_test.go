package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestLaunchOpen(t *testing.T) {
	t.Parallel()

	t.Run("a feature queues its import and nudges the loop", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		var nudged atomic.Bool
		server := openNudgingServer(store, func() { nudged.Store(true) })

		response := do(t, server, http.MethodPost, "/launch/open", req{
			Form: url.Values{"feature": {"project:x"}}, Header: http.Header{"Hx-Request": {"true"}},
		})
		if response.Code != http.StatusOK {
			t.Fatalf("POST /launch/open status = %d, want 200", response.Code)
		}
		dialog := htmltest.Parse(t, response.Body).One(t, "dialog[hx-get]")
		if got := dialog.Text(); !strings.Contains(got, "importing project:x") {
			t.Errorf("POST /launch/open pending text = %q, want importing project:x", got)
		}
		if !nudged.Load() {
			t.Error("POST /launch/open did not nudge the loop")
		}
		pending, err := store.PendingVerbIntents(t.Context(), "import")
		if err != nil {
			t.Fatalf("PendingVerbIntents(import): %v", err)
		}
		if len(pending) != 1 || pending[0].TicketID != "project:x" {
			t.Errorf("pending import intents = %+v, want one for project:x", pending)
		}
	})

	t.Run("a ticket slice mounts without importing or nudging", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.Tickets(t, store,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "project:y"},
		)
		var nudged atomic.Bool
		server := openNudgingServer(store, func() { nudged.Store(true) })

		response := do(t, server, http.MethodPost, "/launch/open", req{
			Form:   url.Values{"ticket": {"sandbox://CC-1", "sandbox://CC-2"}},
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if response.Code != http.StatusOK {
			t.Fatalf("POST /launch/open status = %d, want 200", response.Code)
		}
		dialog := htmltest.Parse(t, response.Body).One(t, "dialog#launch-modal-body")
		dialog.One(t, `cc-launch-modal[tickets="ticket=sandbox%3A%2F%2FCC-1&ticket=sandbox%3A%2F%2FCC-2"]`)
		if nudged.Load() {
			t.Error("POST /launch/open with tickets nudged the loop, want no nudge")
		}
		pending, err := store.PendingVerbIntents(t.Context(), "import")
		if err != nil {
			t.Fatalf("PendingVerbIntents(import): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending import intents = %+v, want none", pending)
		}
	})

	t.Run("an unknown ticket is a bad request", func(t *testing.T) {
		t.Parallel()
		response := do(t, newServer(openStore(t), testNow), http.MethodPost, "/launch/open",
			req{Form: url.Values{"ticket": {"sandbox://GHOST"}}})
		if response.Code != http.StatusBadRequest {
			t.Errorf("POST /launch/open ticket=GHOST status = %d, want 400", response.Code)
		}
	})

	t.Run("a plain feature post redirects to its launch dialog", func(t *testing.T) {
		t.Parallel()
		got := do(t, newServer(openStore(t), testNow), http.MethodPost, "/launch/open", req{
			Form: url.Values{"feature": {"x"}}, Origin: "-",
		})
		if got.Code != http.StatusSeeOther || got.Location != "/f/x/launch" {
			t.Errorf("POST /launch/open feature=x = %d %q, want 303 /f/x/launch", got.Code, got.Location)
		}
	})

	t.Run("a plain ticket post goes to the picker with the dialog open", func(t *testing.T) {
		t.Parallel()
		server := fragmentServer(t)
		response := do(t, server, http.MethodPost, "/launch/open", req{Form: url.Values{"ticket": {"sandbox://CC-1"}}})
		if want := "/launch?ticket=sandbox%3A%2F%2FCC-1"; response.Code != http.StatusSeeOther || response.Location != want {
			t.Fatalf("POST /launch/open ticket=CC-1 = %d %q, want 303 %q", response.Code, response.Location, want)
		}
		htmltest.Parse(t, renderPath(t, server, response.Location)).One(t, "dialog#launch-modal-body").
			One(t, `cc-launch-modal[tickets]`)
	})
}

func TestCandidates(t *testing.T) {
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
				Cancels: len(root.Find(`.launch-foot a[href="/f/project:x"]`))}
			for _, node := range root.Find("dialog#launch-modal-body") {
				_, open := node.Attr("open")
				got.DialogsOpen = append(got.DialogsOpen, open)
				got.EmbedsCandidateURL = strings.Contains(node.HTML(), "sandbox://CC-1")
			}
			for _, node := range root.Find("h2#launch-dialog-title") {
				got.Titles = append(got.Titles, node.Text())
			}
			for _, node := range root.Find("dialog[hx-get]") {
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
