package web_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestActivityTray(t *testing.T) {
	t.Parallel()

	htmx := http.Header{"Hx-Request": {"true"}}
	queueHeld := func(t *testing.T, st *storepkg.Store) int64 {
		t.Helper()
		id, err := st.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "sandbox://CC-1", Verb: plan.VerbRefresh, At: testNow, HoldUntil: testNow.Add(5 * time.Second),
		})
		if err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		return id
	}

	t.Run("a held verb shows Undo until its hold ends", func(t *testing.T) {
		t.Parallel()
		st := seededStoreAtTestNow(t)
		queueHeld(t, st)

		during := do(t, newServer(st, testNow.Add(time.Second)), http.MethodGet, "/activity/tray", req{Header: htmx})
		if during.Code != http.StatusOK {
			t.Fatalf("GET /activity/tray = %d, want 200", during.Code)
		}
		doc := htmltest.Parse(t, during.Body)
		if got := len(doc.Find(`form[action="/verb/undo"]`)); got != 1 {
			t.Errorf("tray during the hold has %d Undo buttons, want 1", got)
		}
		if got := doc.One(t, "#activity-button").Text(); !strings.Contains(got, "1") {
			t.Errorf("activity button = %q, want the count 1", got)
		}

		after := do(t, newServer(st, testNow.Add(10*time.Second)), http.MethodGet, "/activity/tray", req{Header: htmx})
		if got := len(htmltest.Parse(t, after.Body).Find(`form[action="/verb/undo"]`)); got != 0 {
			t.Errorf("tray after the hold has %d Undo buttons, want 0", got)
		}
	})

	t.Run("Undo withdraws the intent and the row leaves the list", func(t *testing.T) {
		t.Parallel()
		st := seededStoreAtTestNow(t)
		id := queueHeld(t, st)
		server := newServer(st, testNow.Add(time.Second))

		got := do(t, server, http.MethodPost, "/verb/undo", req{Header: htmx, Form: url.Values{
			"ticket": {"sandbox://CC-1"}, "verb": {"refresh"}, "intent": {strconv.FormatInt(id, 10)}, "from": {"tray"},
		}})

		if got.Code != http.StatusOK {
			t.Fatalf("POST /verb/undo = %d, want 200 (intent %d)", got.Code, id)
		}
		if !strings.Contains(got.Body, "Nothing in progress") {
			t.Errorf("tray after Undo = %q, want it to read Nothing in progress", got.Body)
		}
		if n := pendingMutationIntentCount(t, st); n != 0 {
			t.Errorf("pending intents after Undo = %d, want 0", n)
		}
	})

	t.Run("every page carries the button and the polling popover", func(t *testing.T) {
		t.Parallel()
		doc := htmltest.Parse(t, do(t, newServer(seededStoreAtTestNow(t), testNow), http.MethodGet, "/tickets", req{}).Body)

		body := doc.One(t, "main#main #activity-tray-body")
		if got, _ := body.Attr("hx-get"); got != "/activity/tray" {
			t.Errorf("tray hx-get = %q, want /activity/tray", got)
		}
		if got := doc.One(t, "#activity-tray-body").Text(); !strings.Contains(got, "Nothing in progress") {
			t.Errorf("empty tray = %q, want Nothing in progress", got)
		}
	})
}

func TestActivitySidebar(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		path        string
		wantSidebar bool
	}{
		{"/s/acme/web/2", true},
		{"/f/checkout", true},
		{"/f/checkout/graph", true},
		{"/insights", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			rec := do(t, newServer(stackStore(t), testNow), http.MethodGet, tt.path, req{})
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			doc := htmltest.Parse(t, rec.Body)
			if got := len(doc.Find("#rightbar #activity-tray-body")) == 1; got != tt.wantSidebar {
				t.Errorf("GET %s has the Activity sidebar = %v, want %v", tt.path, got, tt.wantSidebar)
			}
			if got := len(doc.Find("#activity-button")) == 1; got == tt.wantSidebar {
				t.Errorf("GET %s has the Activity dropdown = %v, want %v", tt.path, got, !tt.wantSidebar)
			}
		})
	}
}
