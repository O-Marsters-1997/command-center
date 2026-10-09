package web_test

import (
	"net/http"
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
		if err := st.RecordDisposition(ctx, runID, plan.OutcomePush, &exit, testNow, nil); err != nil {
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
