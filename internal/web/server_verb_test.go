package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func pendingVerbs(t *testing.T, store *storepkg.Store, verb plan.Verb) []string {
	t.Helper()
	pending, err := store.PendingVerbIntents(t.Context(), verb)
	if err != nil {
		t.Fatalf("PendingVerbIntents(%s): %v", verb, err)
	}
	var got []string
	for _, intent := range pending {
		got = append(got, intent.TicketID+" "+intent.Payload)
	}
	return got
}

func TestVerb(t *testing.T) {
	t.Parallel()

	t.Run("from the session", testVerbFromSession)

	for _, tt := range []struct {
		name string
		verb plan.Verb
		seed func(*testing.T) *storepkg.Store
		form bool
	}{
		{name: "kill of a running row", verb: "kill", seed: seededRunning},
		{name: "cancel of a queued row", verb: "cancel", seed: seededQueued},
		{name: "form-encoded kill", verb: "kill", seed: seededRunning, form: true},
	} {
		t.Run(tt.name+" queues exactly one intent", func(t *testing.T) {
			t.Parallel()
			store := tt.seed(t)
			target, r := "/verb?verb="+string(tt.verb)+"&ticket=sandbox://CC-1", req{}
			if tt.form {
				target, r = "/verb", req{Form: url.Values{"verb": {string(tt.verb)}, "ticket": {"sandbox://CC-1"}}}
			}

			got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, target, r)
			if got.Code != http.StatusSeeOther || got.Location != "/" {
				t.Fatalf("POST /verb %s = %d %q, want 303 /", tt.verb, got.Code, got.Location)
			}
			if diff := cmp.Diff([]string{"sandbox://CC-1 "}, pendingVerbs(t, store, tt.verb)); diff != "" {
				t.Errorf("pending %s intents mismatch (-want +got):\n%s", tt.verb, diff)
			}
		})
	}

	t.Run("rejects a bad request", func(t *testing.T) {
		t.Parallel()
		server := openServer(seededStore(t, testNow), fixedClock(testNow), "")
		for _, tt := range []struct{ name, query string }{
			{name: "unknown ticket", query: "verb=kill&ticket=sandbox://GHOST"},
			{name: "unsupported verb", query: "verb=bogus-verb&ticket=sandbox://CC-1"},
			{name: "missing verb", query: "ticket=sandbox://CC-1"},
			{name: "missing ticket", query: "verb=kill"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := do(t, server, http.MethodPost, "/verb?"+tt.query, req{}); got.Code != http.StatusBadRequest {
					t.Errorf("POST /verb?%s status = %d, want 400", tt.query, got.Code)
				}
			})
		}
	})

	t.Run("a refusal over htmx is a flash fragment", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})

		rec := do(t, newServer(st, testNow), http.MethodPost, "/verb", req{
			Form:   url.Values{"verb": {"bogus-verb"}, "ticket": {sessionTicket}},
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("htmx POST /verb bogus-verb status = %d, want 422", rec.Code)
		}
		want := map[string]string{"HX-Retarget": "#flash", "HX-Reselect": "p.banner", "HX-Reswap": "innerHTML"}
		got := map[string]string{}
		for name := range want {
			got[name] = rec.Header.Get(name)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("htmx POST /verb bogus-verb headers mismatch (-want +got):\n%s", diff)
		}
		if text := htmltest.Parse(t, rec.Body).One(t, "p.banner").Text(); !strings.Contains(text, "bogus-verb") {
			t.Errorf("htmx POST /verb bogus-verb flash = %q, want it to name bogus-verb", text)
		}
	})

	t.Run("answers htmx with the board", func(t *testing.T) {
		t.Parallel()
		resp := do(t, newServer(seededRunning(t), testNow), http.MethodPost, "/verb?verb=kill&ticket=sandbox://CC-1", req{
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if resp.Code != http.StatusOK {
			t.Fatalf("htmx POST /verb status = %d, want 200", resp.Code)
		}
		root := htmltest.Parse(t, resp.Body)
		if got := root.Find("main#main"); len(got) != 0 {
			t.Errorf("htmx POST /verb answered a full page, want the board alone")
		}
		root.One(t, "div#board")
	})

	t.Run("a plain post lands the browser back on home", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(openServer(seededRunning(t), fixedClock(testNow), ""))
		t.Cleanup(srv.Close)
		request, err := http.NewRequest(http.MethodPost, srv.URL+"/verb?verb=kill&ticket=sandbox://CC-1", nil)
		if err != nil {
			t.Fatalf("NewRequest(POST /verb): %v", err)
		}
		request.Header.Set("Origin", srv.URL)

		resp, err := srv.Client().Do(request)
		if err != nil {
			t.Fatalf("Do(POST /verb): %v", err)
		}
		t.Cleanup(func() {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("Close(response body): %v", err)
			}
		})
		if resp.StatusCode != http.StatusOK || resp.Request.Method != http.MethodGet || resp.Request.URL.Path != "/" {
			t.Fatalf("POST /verb followed to %s %s = %d, want GET / = 200",
				resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll(response body): %v", err)
		}
		htmltest.Parse(t, string(body)).One(t, `main#main[data-home]`).One(t, `a[href="/tickets"]`)
	})

	t.Run("follow-up queues the prompt, model and effort as its payload", func(t *testing.T) {
		t.Parallel()
		store := seededFailed(t)
		form := url.Values{"verb": {"follow-up"}, "ticket": {"sandbox://CC-1"}, "prompt": {"fix the flaky test"},
			"model": {"opus"}, "effort": {"high"}}

		got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/verb", req{Form: form})
		if got.Code != http.StatusSeeOther || got.Location != "/" {
			t.Fatalf("POST /verb follow-up = %d %q, want 303 /", got.Code, got.Location)
		}
		want := []string{`sandbox://CC-1 {"message":"fix the flaky test","model":"opus","effort":"high"}`}
		if diff := cmp.Diff(want, pendingVerbs(t, store, "follow-up")); diff != "" {
			t.Errorf("pending follow-up intents mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("follow-up with no prompt text is refused", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct{ name, prompt string }{
			{name: "missing prompt field", prompt: ""},
			{name: "whitespace-only prompt", prompt: "   \n\t  "},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				store := seededStore(t, testNow)
				form := url.Values{"verb": {"follow-up"}, "ticket": {"sandbox://CC-1"}}
				if tt.prompt != "" {
					form.Set("prompt", tt.prompt)
				}

				got := do(t, openServer(store, fixedClock(testNow), ""), http.MethodPost, "/verb", req{Form: form})
				if got.Code != http.StatusBadRequest {
					t.Errorf("POST /verb follow-up prompt %q status = %d, want 400", tt.prompt, got.Code)
				}
				if pending := pendingVerbs(t, store, "follow-up"); len(pending) != 0 {
					t.Errorf("pending follow-up intents = %v, want none", pending)
				}
			})
		}
	})

	t.Run("a verb the row's state does not offer is a conflict", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)

		server := openServer(store, fixedClock(testNow), "")
		got := do(t, server, http.MethodPost, "/verb?verb=kill&ticket=sandbox://CC-1", req{})
		if got.Code != http.StatusConflict {
			t.Fatalf("POST /verb kill on a ready row status = %d, want 409", got.Code)
		}
		if !strings.Contains(got.Body, "ready") || !strings.Contains(got.Body, "kill") {
			t.Errorf("POST /verb kill 409 body = %q, want it to name ready and kill", got.Body)
		}
		if pending := pendingVerbs(t, store, "kill"); len(pending) != 0 {
			t.Errorf("pending kill intents = %v, want none", pending)
		}
	})
}
