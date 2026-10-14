package web_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const sessionTicket = "https://github.com/acme/web/issues/1"

type sessionRunOpts struct {
	Feature    string
	Kind       plan.RunKind
	Prompt     string
	KeepPrompt bool
	Alive      bool
	Outcome    plan.Outcome
}

func sessionStore(t *testing.T, opts sessionRunOpts) (*storepkg.Store, string) {
	t.Helper()
	opts.Outcome = plan.OutcomePush
	return sessionStoreEnding(t, opts)
}

func sessionStoreEnding(t *testing.T, opts sessionRunOpts) (*storepkg.Store, string) {
	t.Helper()
	opts.Feature = "checkout"
	return sessionStoreInFeature(t, opts)
}

func sessionStoreInFeature(t *testing.T, opts sessionRunOpts) (*storepkg.Store, string) {
	t.Helper()
	ctx := t.Context()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: sessionTicket, Repo: "acme/web", Branch: "cc-1", Feature: opts.Feature}
	storetest.Tickets(t, st, ticket)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "1.jsonl")
	line := `{"type":"assistant","timestamp":"2026-08-20T12:00:01Z",` +
		`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test"}}]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if opts.KeepPrompt {
		if err := os.WriteFile(filepath.Join(dir, "1.prompt"), []byte(opts.Prompt), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outcome := option.None[plan.Outcome]()
	if !opts.Alive {
		outcome = option.Some(opts.Outcome)
	}
	storetest.Run(t, st, storetest.RunOpts{
		TicketURL: sessionTicket, Kind: opts.Kind, Pgid: 99, StartedAt: testNow,
		LogPath: logPath, Outcome: outcome, EndedAt: testNow,
	})
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: opts.Alive}},
	}
	if err := st.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return st, line
}

func TestSessionPageShowsHeaderAndPromptByteForByte(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind plan.RunKind
	}{
		{name: "agent", kind: plan.RunKindAgent},
		{name: "follow-up", kind: plan.RunKindFollowUp},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prompt := "Implement <b>#1</b>\n  & keep   spacing\n"
			st, _ := sessionStore(t, sessionRunOpts{Kind: tt.kind, Prompt: prompt, KeepPrompt: true, Alive: true})
			body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

			for _, want := range []string{
				`data-glyph="running"`, "#1", "acme/web", "checkout", `<main id="main"`, `class="back"`,
				`<pre class="prompt-text">Implement &lt;b&gt;#1&lt;/b&gt;` + "\n  &amp; keep   spacing\n</pre>",
				`sse-connect="/ticket/`, `id="session-actions"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s run: page lacks %q:\n%s", tt.kind, want, body)
				}
			}
		})
	}
}

func TestSessionPageSaysWhenPromptWasPruned(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent"})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")
	if !strings.Contains(body, "The prompt was not kept.") || strings.Contains(body, `class="prompt-text"`) {
		t.Errorf("pruned prompt not announced:\n%s", body)
	}
}

func TestSessionRawTabServesTheJSONL(t *testing.T) {
	t.Parallel()
	st, line := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1?log=raw")
	escaped := strings.TrimSpace(strings.ReplaceAll(line, `"`, "&#34;"))
	if !strings.Contains(body, `<pre class="raw-log">`) || !strings.Contains(body, escaped) {
		t.Errorf("raw tab lacks the JSONL:\n%s", body)
	}
}

func TestSessionPageShowsTheWorktreeToShellInto(t *testing.T) {
	t.Parallel()
	now := testNow
	body := renderPath(t, newServer(detailStore(t, writeLog(t, 1), now, now), now), "/s/o/r/76")
	if !strings.Contains(body, "<code>/repos/repo-cc-76</code>") {
		t.Errorf("session page does not show the ticket's worktree:\n%s", body)
	}
}

func TestSessionPageUnknownTicketIs404(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(openStore(t), testNow), http.MethodGet, "/s/acme/web/9", req{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionPageWithANonNumericTicketIs404(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(openStore(t), testNow), http.MethodGet, "/s/acme/web/nine", req{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionRefRoundTripsThroughItsPath(t *testing.T) {
	t.Parallel()
	ref, err := view.ParseSessionRef(sessionTicket)
	if err != nil {
		t.Fatal(err)
	}
	want := view.SessionRef{Owner: "acme", Name: "web", N: "1"}
	if ref != want {
		t.Errorf("ParseSessionRef(%q) = %+v, want %+v", sessionTicket, ref, want)
	}
	if got := ref.Path(); got != "/s/acme/web/1" {
		t.Errorf("Path() = %q, want /s/acme/web/1", got)
	}
}

func TestParseSessionRefRefusesATooShortURL(t *testing.T) {
	t.Parallel()
	if _, err := view.ParseSessionRef("web/issues/1"); err == nil {
		t.Error("ParseSessionRef(short URL) error = nil, want an error")
	}
}

func TestSessionPageShowsTheMergeLineOfARunWhoseLogWasPruned(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent"})
	logPath, _, err := st.LatestRunLog(ctx, sessionTicket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	merged := storepkg.Event{At: testNow, TicketURL: sessionTicket, Kind: "pr_merged", Detail: "PR #1 merged"}
	if err := st.AppendEvent(ctx, merged); err != nil {
		t.Fatal(err)
	}

	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	if !strings.Contains(body, `<span class="line-label">merged</span> PR #1 merged`) {
		t.Errorf("session page lacks the merge line:\n%s", body)
	}
	if strings.Contains(body, "Nothing to show yet") {
		t.Errorf("session page claims there is nothing to show:\n%s", body)
	}
}

func TestSessionComposerFollowsTheState(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	body := renderPath(t, newServer(running, testNow), "/s/acme/web/1")
	for _, want := range []string{
		`class="composer-running"`, "Agent is running", `value="kill"`, `href="/s/acme/web/1?log=raw"`,
		`class="composer"`, `value="follow-up"`, "Send the running agent a message",
		`class="composer-chip"`, config.AgentModel,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("running session lacks %q:\n%s", want, body)
		}
	}

	idle, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
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

func TestRunningSessionShowsAQueuedMessageUntilTheLoopConsumesIt(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	if err := running.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: sessionTicket, Verb: plan.VerbFollowUp, Payload: "use sqlite instead", At: testNow,
	}); err != nil {
		t.Fatal(err)
	}

	body := renderPath(t, newServer(running, testNow), "/s/acme/web/1")
	for _, want := range []string{`class="pending-message"`, "use sqlite instead", `class="composer"`} {
		if !strings.Contains(body, want) {
			t.Errorf("running session with a queued message lacks %q:\n%s", want, body)
		}
	}
}

func TestSessionVerbKeysAnchorOnlyWhereTheStateOffersThem(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	failed, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	tests := []struct {
		name    string
		store   *storepkg.Store
		present []string
		absent  []string
	}{
		{
			name: "failed", store: failed,
			present: []string{`value="re-run" data-key="r"`, `<textarea name="prompt" rows="2" data-key="enter"`},
			absent:  []string{`data-key="o"`, `data-key="e"`},
		},
		{
			name: "running", store: running,
			present: []string{`<textarea name="prompt" rows="2" data-key="enter"`},
			absent:  []string{`data-key="r"`, `data-key="o"`, `data-key="e"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := renderPath(t, newServer(tt.store, testNow), "/s/acme/web/1")
			for _, want := range tt.present {
				if !strings.Contains(body, want) {
					t.Errorf("actions lack %q:\n%s", want, body)
				}
			}
			for _, bad := range tt.absent {
				if strings.Contains(body, bad) {
					t.Errorf("actions carry %q:\n%s", bad, body)
				}
			}
		})
	}
}

func TestMergedSessionAnchorsRemoveWorktreeAndOpenPR(t *testing.T) {
	t.Parallel()
	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomePush})
	pr := plan.PR{Number: 7, HeadRef: "cc-1", State: plan.Merged, MergedAt: testNow}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: false}},
		PRs:        map[plan.BranchRef]plan.PR{branchRef("acme/web", "cc-1"): pr},
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}

	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1/actions")

	for _, want := range []string{`value="remove-worktree" data-key="e"`, `data-key="o" target="_blank"`} {
		if !strings.Contains(body, want) {
			t.Errorf("merged session lacks %q:\n%s", want, body)
		}
	}
}

func TestRailVerbButtonsCarryNoKey(t *testing.T) {
	t.Parallel()
	failed, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	body := renderPath(t, newServer(failed, testNow), "/rail")
	if !strings.Contains(body, `name="verb"`) || strings.Contains(body, `data-key="r"`) {
		t.Errorf("rail verb buttons carry a verb key:\n%s", body)
	}
}

func TestSessionVerbToastsAndShowsPending(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	srv := newServer(st, testNow)

	rec := do(t, srv, http.MethodPost, "/verb", req{
		Form:   url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}},
		Header: http.Header{"Hx-Request": {"true"}},
	})
	body := rec.Body
	if rec.Code != http.StatusOK || !strings.Contains(body, `role="status">Queued kill`) {
		t.Fatalf("status %d, want a Queued kill toast:\n%s", rec.Code, body)
	}
	if !strings.Contains(body, `data-glyph="pending"`) || !strings.Contains(body, " disabled") {
		t.Errorf("queued verb shows no pending mark:\n%s", body)
	}

	poll := renderPath(t, srv, "/s/acme/web/1/actions")
	if !strings.Contains(poll, `data-glyph="pending"`) || strings.Contains(poll, `class="toast"`) {
		t.Errorf("polled actions should keep the pending mark and drop the toast:\n%s", poll)
	}
}

func TestSeededBoardFeatureAndSessionKeepTheSameVerbControls(t *testing.T) {
	st, _ := sessionStore(t, sessionRunOpts{Kind: plan.RunKindAgent, Prompt: "p", KeepPrompt: true, Alive: true})
	if err := st.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: sessionTicket, Verb: plan.VerbKill, At: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	srv := newServer(st, testNow)
	confirm := `hx-confirm="kill on https://github.com/acme/web/issues/1 signals the process group of ` +
		`its live agent run, stopping it where it is. It cannot be undone. pgid at risk: 99"`
	for _, tc := range []struct{ name, path, verbPath string }{
		{name: "tickets", path: "/tickets", verbPath: "/verb?all=1"},
		{name: "feature", path: "/f/checkout", verbPath: "/verb?feature=checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := renderPath(t, srv, tc.path)
			for _, want := range []string{
				`<table id="board"`, `data-section="in-progress"`, `hx-post="` + tc.verbPath + `"`,
				confirm, `name="verb" value="kill"`, `kill queued`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s lacks %q:\n%s", tc.path, want, body)
				}
			}
		})
	}
	for _, path := range []string{"/s/acme/web/1", "/s/acme/web/1/actions"} {
		body := renderPath(t, srv, path)
		for _, want := range []string{
			`id="session-actions"`, `hx-post="/verb"`, confirm,
			`name="verb" value="kill" disabled>kill</button>`,
			`<span class="pending-mark" data-glyph="pending">queued</span>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s lacks %q:\n%s", path, want, body)
			}
		}
	}
}

func TestSessionVerbWithoutHTMXRedirectsBack(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	form := url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}}
	rec := do(t, newServer(st, testNow), http.MethodPost, "/verb", req{Form: form})
	if rec.Code != http.StatusSeeOther || rec.Location != "/s/acme/web/1" {
		t.Errorf("got %d %q, want 303 to the session", rec.Code, rec.Location)
	}
}

func TestSessionFollowUpQueuesThePrompt(t *testing.T) {
	t.Parallel()
	st, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	srv := newServer(st, testNow)
	rec := do(t, srv, http.MethodPost, "/verb", req{
		Form:   url.Values{"verb": {"follow-up"}, "ticket": {sessionTicket}, "from": {"session"}, "prompt": {"try again"}},
		Header: http.Header{"Hx-Request": {"true"}},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body, "Queued follow-up") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body)
	}
	rec = do(t, srv, http.MethodPost, "/verb", req{
		Form:   url.Values{"verb": {"follow-up"}, "ticket": {sessionTicket}, "from": {"session"}},
		Header: http.Header{"Hx-Request": {"true"}},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank follow-up status = %d, want 422", rec.Code)
	}
}

func TestSessionFeatureLinkCarriesTheFeatureKey(t *testing.T) {
	t.Parallel()
	withFeature, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	noFeature, _ := sessionStoreInFeature(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true, Outcome: plan.OutcomePush,
	})

	body := renderPath(t, newServer(withFeature, testNow), "/s/acme/web/1")
	if want := `href="/f/checkout/graph" data-key="f"`; !strings.Contains(body, want) {
		t.Errorf("session with a feature lacks %q:\n%s", want, body)
	}
	body = renderPath(t, newServer(noFeature, testNow), "/s/acme/web/1")
	if strings.Contains(body, `data-key="f"`) {
		t.Errorf("session with no feature carries data-key=\"f\":\n%s", body)
	}
}

func TestSessionComposerIsOutsideEveryPolledElement(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	if !strings.Contains(body, "<textarea") {
		t.Fatalf("no composer textarea on the page:\n%s", body)
	}
	start := strings.Index(body, `<div id="session-actions"`)
	if start < 0 {
		t.Fatalf("no polled session-actions block:\n%s", body)
	}
	polled := body[start:]
	depth := 0
	for i := 0; i < len(polled); i++ {
		switch {
		case strings.HasPrefix(polled[i:], "<div"):
			depth++
		case strings.HasPrefix(polled[i:], "</div>"):
			depth--
			if depth == 0 {
				polled = polled[:i]
			}
		}
	}
	if strings.Contains(polled, "<textarea") {
		t.Errorf("the composer textarea sits inside the polled #session-actions:\n%s", polled)
	}
}
