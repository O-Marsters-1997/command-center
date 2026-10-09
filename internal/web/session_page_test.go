package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

const sessionTicket = "https://github.com/acme/web/issues/1"

func sessionStore(t *testing.T, kind, prompt string, keepPrompt, alive bool) (*storepkg.Store, string) {
	t.Helper()
	return sessionStoreEnding(t, kind, prompt, keepPrompt, alive, plan.OutcomePush)
}

func sessionStoreEnding(t *testing.T, kind, prompt string, keepPrompt, alive bool, outcome plan.Outcome) (*storepkg.Store, string) {
	t.Helper()
	ctx := t.Context()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: sessionTicket, Repo: "acme/web", Branch: "cc-1", Feature: "checkout"}
	if err := st.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := st.InsertRunSkeleton(ctx, sessionTicket, kind, "base", "hash")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "1.jsonl")
	line := `{"type":"assistant","timestamp":"2026-08-20T12:00:01Z",` +
		`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test"}}]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if keepPrompt {
		if err := os.WriteFile(filepath.Join(dir, "1.prompt"), []byte(prompt), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RecordSpawn(ctx, runID, 99, testNow, logPath); err != nil {
		t.Fatal(err)
	}
	if !alive {
		exit := 0
		if err := st.RecordDisposition(ctx, runID, outcome, &exit, testNow, nil); err != nil {
			t.Fatal(err)
		}
	}
	obs := plan.Observation{ObservedAt: testNow, Runs: map[string]plan.RunObservation{sessionTicket: {Alive: alive}}}
	if err := st.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return st, line
}

func TestSessionPageShowsHeaderAndPromptByteForByte(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"agent", "follow-up", "resolve"} {
		prompt := "Implement <b>#1</b>\n  & keep   spacing\n"
		st, _ := sessionStore(t, kind, prompt, true, true)
		body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

		for _, want := range []string{
			`data-glyph="running"`, "#1", "acme/web", "checkout", `<main id="main"`, `class="back"`,
			`<pre class="prompt-text">Implement &lt;b&gt;#1&lt;/b&gt;` + "\n  &amp; keep   spacing\n</pre>",
			`sse-connect="/ticket/`, `id="session-actions"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s run: page lacks %q:\n%s", kind, want, body)
			}
		}
	}
}

func TestSessionPageSaysWhenPromptWasPruned(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "", false, false)
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")
	if !strings.Contains(body, "The prompt was not kept.") || strings.Contains(body, `class="prompt-text"`) {
		t.Errorf("pruned prompt not announced:\n%s", body)
	}
}

func TestSessionRawTabServesTheJSONL(t *testing.T) {
	t.Parallel()
	st, line := sessionStore(t, "agent", "p", true, false)
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1?log=raw")
	escaped := strings.TrimSpace(strings.ReplaceAll(line, `"`, "&#34;"))
	if !strings.Contains(body, `<pre class="raw-log">`) || !strings.Contains(body, escaped) {
		t.Errorf("raw tab lacks the JSONL:\n%s", body)
	}
}

func TestSessionPageUnknownTicketIs404(t *testing.T) {
	t.Parallel()
	rec := get(t, newServer(openStore(t), testNow), "/s/acme/web/9")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionPageGolden(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "Implement #1\n", true, false)
	assertGolden(t, "testdata/session.golden.html", []byte(renderPath(t, newServer(st, testNow), "/s/acme/web/1")))
}

func sessionVerb(t *testing.T, srv http.Handler, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/verb", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestSessionComposerFollowsTheState(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, "agent", "p", true, true)
	body := renderPath(t, newServer(running, testNow), "/s/acme/web/1")
	for _, want := range []string{`class="composer-running"`, "Agent is running", `value="kill"`, `href="/s/acme/web/1?log=raw"`} {
		if !strings.Contains(body, want) {
			t.Errorf("running session lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `class="composer"`) {
		t.Errorf("running session offers a follow-up composer:\n%s", body)
	}

	idle, _ := sessionStoreEnding(t, "agent", "p", true, false, plan.OutcomeFailed)
	body = renderPath(t, newServer(idle, testNow), "/s/acme/web/1")
	for _, want := range []string{`class="composer"`, `value="follow-up"`, `value="re-run"`} {
		if !strings.Contains(body, want) {
			t.Errorf("finished session lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Agent is running") {
		t.Errorf("finished session claims to be running:\n%s", body)
	}
}

func TestSessionVerbToastsAndShowsPending(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "p", true, true)
	srv := newServer(st, testNow)

	rec := sessionVerb(t, srv, url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}}, true)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `role="status">Queued kill`) {
		t.Fatalf("status %d, want a Queued kill toast:\n%s", rec.Code, body)
	}
	if !strings.Contains(body, `data-glyph="pending"`) || !strings.Contains(body, " disabled") {
		t.Errorf("queued verb shows no pending mark:\n%s", body)
	}

	poll := renderPath(t, srv, "/s/acme/web/1?part=actions")
	if !strings.Contains(poll, `data-glyph="pending"`) || strings.Contains(poll, `class="toast"`) {
		t.Errorf("polled actions should keep the pending mark and drop the toast:\n%s", poll)
	}
}

func TestSessionVerbWithoutHTMXRedirectsBack(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "p", true, true)
	rec := sessionVerb(t, newServer(st, testNow), url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/s/acme/web/1" {
		t.Errorf("got %d %q, want 303 to the session", rec.Code, rec.Header().Get("Location"))
	}
}
