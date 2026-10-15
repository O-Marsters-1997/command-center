package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestVerbQueuesExactlyOneKillOrCancelIntent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		verb plan.Verb
		seed func(*testing.T) *storepkg.Store
	}{
		{name: "kill running", verb: "kill", seed: seededRunning},
		{name: "cancel queued", verb: "cancel", seed: seededQueued},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := tt.seed(t)
			server := openServer(store, fixedClock(testNow), "")
			got := do(t, server, http.MethodPost, "/verb?verb="+string(tt.verb)+"&ticket=sandbox://CC-1", req{})
			if got.Code != http.StatusSeeOther || got.Location != "/" {
				t.Fatalf("POST %s = %d %q, want 303 /", tt.verb, got.Code, got.Location)
			}
			pending, err := store.PendingVerbIntents(t.Context(), tt.verb)
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
				t.Fatalf("pending %s intents = %+v, want one for sandbox://CC-1", tt.verb, pending)
			}
		})
	}
}

func TestVerbRejectsUnknownTicketOrUnsupportedVerb(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), fixedClock(testNow), "")

	tests := []struct{ name, query string }{
		{name: "unknown ticket", query: "verb=kill&ticket=sandbox://GHOST"},
		{name: "unsupported verb", query: "verb=bogus-verb&ticket=sandbox://CC-1"},
		{name: "missing verb", query: "ticket=sandbox://CC-1"},
		{name: "missing ticket", query: "verb=kill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := do(t, server, http.MethodPost, "/verb?"+tt.query, req{})
			if got.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", got.Code)
			}
		})
	}
}

func TestVerbAcceptsFormEncodedFields(t *testing.T) {
	t.Parallel()

	store := seededRunning(t)
	server := openServer(store, fixedClock(testNow), "")
	form := url.Values{"verb": {"kill"}, "ticket": {"sandbox://CC-1"}}
	got := do(t, server, http.MethodPost, "/verb", req{Form: form})
	if got.Code != http.StatusSeeOther || got.Location != "/" {
		t.Fatalf("POST kill form = %d %q, want 303 /", got.Code, got.Location)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
		t.Fatalf("pending kill intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
}

func TestVerbLandsTheBrowserBackOnTheBoard(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(openServer(seededRunning(t), fixedClock(testNow), ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?verb=kill&ticket=sandbox://CC-1", nil)
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
	htmltest.Parse(t, string(body)).One(t, `main#main[data-home]`).One(t, `a[href="/tickets"]`)
}

func TestVerbRejectsFollowUpWithNoPromptText(t *testing.T) {

	store := seededStore(t, testNow)
	server := openServer(store, fixedClock(testNow), "")

	tests := []struct {
		name   string
		prompt string
	}{
		{name: "missing prompt field", prompt: ""},
		{name: "whitespace-only prompt", prompt: "   \n\t  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{"verb": {"follow-up"}, "ticket": {"sandbox://CC-1"}}
			if tt.prompt != "" {
				form.Set("prompt", tt.prompt)
			}
			got := do(t, server, http.MethodPost, "/verb", req{Form: form})
			if got.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", got.Code)
			}
		})
	}

	pending, err := store.PendingVerbIntents(t.Context(), "follow-up")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending follow-up intents = %+v, want none queued", pending)
	}
}

func TestVerbQueuesFollowUpIntentCarryingThePromptAsPayload(t *testing.T) {
	t.Parallel()

	store := seededFailed(t)
	server := openServer(store, fixedClock(testNow), "")

	form := url.Values{
		"verb": {"follow-up"}, "ticket": {"sandbox://CC-1"}, "prompt": {"fix the flaky test"},
	}
	got := do(t, server, http.MethodPost, "/verb", req{Form: form})
	if got.Code != http.StatusSeeOther || got.Location != "/" {
		t.Fatalf("POST follow-up = %d %q, want 303 /", got.Code, got.Location)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "follow-up")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
		t.Fatalf("pending follow-up intents = %+v, want exactly one for sandbox://CC-1", pending)
	}
	if pending[0].Payload != "fix the flaky test" {
		t.Errorf("payload = %q, want the typed prompt text", pending[0].Payload)
	}
}

func TestVerbAnswers409WhenTheRowsStateDoesNotOfferIt(t *testing.T) {
	t.Parallel()

	store := seededStore(t, testNow)
	server := openServer(store, fixedClock(testNow), "")
	got := do(t, server, http.MethodPost, "/verb?verb=kill&ticket=sandbox://CC-1", req{})
	if got.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: a ready row does not offer kill", got.Code)
	}
	if !strings.Contains(got.Body, "ready") || !strings.Contains(got.Body, "kill") {
		t.Errorf("body = %q, want the row's state and the refused verb named", got.Body)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending kill intents = %+v, want none queued on a 409", pending)
	}
}
