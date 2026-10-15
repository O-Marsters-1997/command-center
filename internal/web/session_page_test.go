package web_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/htmltest"
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
		t.Fatalf("WriteFile(%q): %v", logPath, err)
	}
	if opts.KeepPrompt {
		if err := os.WriteFile(filepath.Join(dir, "1.prompt"), []byte(opts.Prompt), 0o600); err != nil {
			t.Fatalf("WriteFile(1.prompt): %v", err)
		}
	}
	runID, err := st.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: sessionTicket, Kind: opts.Kind, PromptHash: "hash",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", sessionTicket, err)
	}
	if err := st.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 99, StartedAt: testNow, LogPath: logPath,
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	if !opts.Alive {
		exitCode := 0
		if err := st.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: opts.Outcome, ExitCode: option.Some(exitCode),
			Cost: option.None[float64](), EndedAt: testNow,
		}); err != nil {
			t.Fatalf("RecordDisposition(%d): %v", runID, err)
		}
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: opts.Alive}},
	}
	if err := st.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st, line
}

func TestSessionPageShowsHeaderAndPromptByteForByte(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		kind plan.RunKind
	}{
		{name: "agent", kind: plan.RunKindAgent},
		{name: "follow-up", kind: plan.RunKindFollowUp},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prompt := "Implement <b>#1</b>\n  & keep   spacing\n"
			st, _ := sessionStore(t, sessionRunOpts{Kind: tt.kind, Prompt: prompt, KeepPrompt: true, Alive: true})
			body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

			main := htmltest.Parse(t, body).One(t, "main#main")
			main.One(t, `header.session-head [data-glyph="running"]`)
			main.One(t, "a.back")
			main.One(t, "#session-actions")
			text := main.Text()
			if !strings.Contains(text, "#1") || !strings.Contains(text, "acme/web") ||
				!strings.Contains(text, "checkout") {
				t.Errorf("%s session heading missing #1, acme/web, or checkout", tt.kind)
			}
			pre := main.One(t, "pre.prompt-text")
			if got := pre.HTML(); !strings.Contains(got, "Implement &lt;b&gt;#1&lt;/b&gt;\n  &amp; keep   spacing\n") {
				t.Errorf("%s prompt bytes = %s", tt.kind, got)
			}
			if streams := main.Find("[sse-connect]"); len(streams) == 0 {
				t.Errorf("%s session has no event stream", tt.kind)
			}
		})
	}
}

func TestSessionPageSaysWhenPromptWasPruned(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent"})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")
	main := htmltest.Parse(t, body).One(t, "main#main")
	if got := main.Text(); !strings.Contains(got, "The prompt was not kept.") {
		t.Error("pruned prompt status missing")
	}
	if pre := main.Find("pre.prompt-text"); len(pre) != 0 {
		t.Errorf("pruned prompt still shown: %s", pre[0].HTML())
	}
}

func TestSessionRawTabServesTheJSONL(t *testing.T) {
	t.Parallel()
	st, line := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1?log=raw")
	pre := htmltest.Parse(t, body).One(t, "pre.raw-log")
	if got := pre.Text(); got != strings.TrimSpace(line) {
		t.Errorf("raw log = %q, want %q", got, strings.TrimSpace(line))
	}
}

func TestSessionPageShowsTheWorktreeToShellInto(t *testing.T) {
	t.Parallel()
	now := testNow
	body := renderPath(t, newServer(detailStore(t, writeLog(t, 1), now, now), now), "/s/o/r/76")
	main := htmltest.Parse(t, body).One(t, "main#main")
	if codes := main.Find("code"); len(codes) == 0 {
		t.Error("session has no worktree path")
	} else {
		found := false
		for _, code := range codes {
			found = found || code.Text() == "/repos/repo-cc-76"
		}
		if !found {
			t.Errorf("worktree path absent from %d code elements", len(codes))
		}
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
		t.Fatalf("ParseSessionRef(%q): %v", sessionTicket, err)
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
		t.Fatalf("LatestRunLog(%q): %v", sessionTicket, err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatalf("Remove(%q): %v", logPath, err)
	}
	merged := storepkg.Event{At: testNow, TicketURL: sessionTicket, Kind: "pr_merged", Detail: "PR #1 merged"}
	if err := st.AppendEvent(ctx, merged); err != nil {
		t.Fatalf("AppendEvent(pr_merged): %v", err)
	}

	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	main := htmltest.Parse(t, body).One(t, "main#main")
	if got := main.Text(); !strings.Contains(got, "merged PR #1 merged") {
		t.Error("session merge line missing merged PR #1 merged")
	}
	if strings.Contains(main.Text(), "Nothing to show yet") {
		t.Error("session falsely claims empty log")
	}
}

func TestSessionComposerFollowsTheState(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	body := renderPath(t, newServer(running, testNow), "/s/acme/web/1")
	main := htmltest.Parse(t, body).One(t, "main#main")
	runningStatus := main.One(t, ".composer-running")
	if got := runningStatus.Text(); !strings.Contains(got, "Agent is running") {
		t.Errorf("running status = %q", got)
	}
	main.One(t, `button[value="kill"]`)
	main.One(t, `nav[aria-label="log view"] a[href="/s/acme/web/1?log=raw"]`)
	composer := main.One(t, ".composer")
	composer.One(t, `input[name="verb"][value="follow-up"]`)
	composer.One(t, `textarea[aria-label="follow-up prompt"]`)
	if got := main.One(t, ".composer-chip").Text(); !strings.Contains(got, config.AgentModel) {
		t.Errorf("composer model = %q", got)
	}

	idle, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	body = renderPath(t, newServer(idle, testNow), "/s/acme/web/1")
	main = htmltest.Parse(t, body).One(t, "main#main")
	main.One(t, ".composer").One(t, `input[name="verb"][value="follow-up"]`)
	main.One(t, `button[value="re-run"]`)
	if status := main.Find(".composer-running"); len(status) != 0 {
		t.Errorf("finished session claims to be running: %s", status[0].HTML())
	}
}

func TestRunningSessionShowsAQueuedMessageUntilTheLoopConsumesIt(t *testing.T) {
	t.Parallel()
	running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	if err := running.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: sessionTicket, Verb: plan.VerbFollowUp, Payload: "use sqlite instead", At: testNow,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload(follow-up): %v", err)
	}

	body := renderPath(t, newServer(running, testNow), "/s/acme/web/1")
	main := htmltest.Parse(t, body).One(t, "main#main")
	pending := main.One(t, ".pending-message")
	if got := pending.Text(); !strings.Contains(got, "use sqlite instead") {
		t.Errorf("queued message = %q: %s", got, pending.HTML())
	}
	main.One(t, ".composer")
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
			present: []string{`button[value="re-run"][data-key="r"]`, `textarea[name="prompt"][rows="2"][data-key="enter"]`},
			absent:  []string{`[data-key="o"]`, `[data-key="e"]`},
		},
		{
			name: "running", store: running,
			present: []string{`textarea[name="prompt"][rows="2"][data-key="enter"]`},
			absent:  []string{`[data-key="r"]`, `[data-key="o"]`, `[data-key="e"]`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			main := htmltest.Parse(t, renderPath(t, newServer(tt.store, testNow), "/s/acme/web/1")).One(t, "main#main")
			for _, selector := range tt.present {
				main.One(t, selector)
			}
			for _, selector := range tt.absent {
				if unwanted := main.Find(selector); len(unwanted) != 0 {
					t.Errorf("actions carry %s: %s", selector, unwanted[0].HTML())
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
		t.Fatalf("SaveObservation: %v", err)
	}

	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1/actions")

	root := htmltest.Parse(t, body)
	root.One(t, `button[value="remove-worktree"][data-key="e"]`)
	root.One(t, `a[data-key="o"][target="_blank"]`)
}

func TestRailVerbButtonsCarryNoKey(t *testing.T) {
	t.Parallel()
	failed, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	body := renderPath(t, newServer(failed, testNow), "/rail")
	rail := htmltest.Parse(t, body).One(t, "#rail-body")
	if buttons := rail.Find(`button[name="verb"]`); len(buttons) == 0 {
		t.Error("rail has no verb buttons")
	} else {
		for _, button := range buttons {
			if key, ok := button.Attr("data-key"); ok {
				t.Errorf("rail verb has key %q: %s", key, button.HTML())
			}
		}
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
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	root := htmltest.Parse(t, rec.Body)
	toast := root.One(t, `[role="status"]`)
	if got := toast.Text(); !strings.Contains(got, "Queued kill") {
		t.Errorf("kill toast = %q", got)
	}
	root.One(t, `[data-glyph="pending"]`)
	root.One(t, `button[value="kill"][disabled]`)

	poll := htmltest.Parse(t, renderPath(t, srv, "/s/acme/web/1/actions"))
	poll.One(t, `[data-glyph="pending"]`)
	if toasts := poll.Find(".toast"); len(toasts) != 0 {
		t.Errorf("polled actions include toast: %s", toasts[0].HTML())
	}
}

func TestSeededBoardFeatureAndSessionKeepTheSameVerbControls(t *testing.T) {
	t.Parallel()

	st, _ := sessionStore(t, sessionRunOpts{Kind: plan.RunKindAgent, Prompt: "p", KeepPrompt: true, Alive: true})
	if err := st.QueueVerbIntentWithPayload(t.Context(), storepkg.VerbRequest{
		Ticket: sessionTicket, Verb: plan.VerbKill, At: testNow,
	}); err != nil {
		t.Fatalf("QueueVerbIntentWithPayload(kill): %v", err)
	}
	srv := newServer(st, testNow)
	confirm := `kill on https://github.com/acme/web/issues/1 signals the process group of ` +
		`its live agent run, stopping it where it is. It cannot be undone. pgid at risk: 99`
	for _, tc := range []struct{ name, path, verbPath string }{
		{name: "tickets", path: "/tickets", verbPath: "/verb?all=1"},
		{name: "feature", path: "/f/checkout", verbPath: "/verb?feature=checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			board := htmltest.Parse(t, renderPath(t, srv, tc.path)).One(t, "table#board")
			board.One(t, `[data-section="in-progress"]`)
			form := board.One(t, `form[hx-post="`+tc.verbPath+`"]`)
			if got, _ := form.Attr("hx-confirm"); got != confirm {
				t.Errorf("%s kill confirmation = %q, want %q: %s", tc.path, got, confirm, form.HTML())
			}
			form.One(t, `button[name="verb"][value="kill"]`)
			if got := board.Text(); !strings.Contains(got, "kill queued") {
				t.Errorf("%s pending board state missing kill queued", tc.path)
			}
		})
	}
	for _, path := range []string{"/s/acme/web/1", "/s/acme/web/1/actions"} {
		actions := htmltest.Parse(t, renderPath(t, srv, path)).One(t, "#session-actions")
		form := actions.One(t, `form[hx-post="/verb"]`)
		if got, _ := form.Attr("hx-confirm"); got != confirm {
			t.Errorf("%s kill confirmation = %q, want %q: %s", path, got, confirm, form.HTML())
		}
		form.One(t, `button[name="verb"][value="kill"][disabled]`)
		if got := actions.One(t, `.pending-mark[data-glyph="pending"]`).Text(); got != "queued" {
			t.Errorf("%s pending mark = %q", path, got)
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
	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d, want 200", rec.Code)
	}
	if got := htmltest.Parse(t, rec.Body).One(t, `[role="status"]`).Text(); !strings.Contains(got, "Queued follow-up") {
		t.Errorf("follow-up toast = %q", got)
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
	htmltest.Parse(t, body).One(t, `a[href="/f/checkout/graph"][data-key="f"]`)
	body = renderPath(t, newServer(noFeature, testNow), "/s/acme/web/1")
	main := htmltest.Parse(t, body).One(t, "main#main")
	if keys := main.Find(`[data-key="f"]`); len(keys) != 0 {
		t.Errorf("session with no feature carries feature key: %s", keys[0].HTML())
	}
}

func TestSessionComposerIsOutsideEveryPolledElement(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	main := htmltest.Parse(t, body).One(t, "main#main")
	main.One(t, `textarea[name="prompt"]`)
	actions := main.One(t, "#session-actions")
	if composer := actions.Find(`textarea[name="prompt"]`); len(composer) != 0 {
		t.Errorf("composer is inside polled actions: %s", composer[0].HTML())
	}
}
