package web_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const sessionTicket = "https://github.com/acme/web/issues/1"

type sessionRunOpts struct {
	Feature    string
	Kind       plan.RunKind
	Prompt     string
	KeepPrompt bool
	Alive      bool
	Pruned     bool
	Outcome    plan.Outcome

	LastMessage option.Option[string]
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
	runID, _, err := st.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: sessionTicket, Kind: opts.Kind, PromptHash: "hash", Step: "implement",
		Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", sessionTicket, err)
	}
	if err := st.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 99, StartedAt: testNow, LogPath: logPath,
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	if opts.Pruned {
		if err := st.RecordPrompt(ctx, runID, opts.Prompt); err != nil {
			t.Fatalf("RecordPrompt(%d): %v", runID, err)
		}
	}
	if !opts.Alive {
		metrics := option.None[agentlog.RunMetrics]()
		if opts.Pruned {
			metrics = option.Some(agentlog.RunMetrics{Wall: option.Some(90 * time.Second), Turns: 3})
		}
		if err := st.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: opts.Outcome, ExitCode: option.Some(0),
			Cost: option.None[float64](), EndedAt: testNow, LastMessage: opts.LastMessage, Metrics: metrics,
		}); err != nil {
			t.Fatalf("RecordDisposition(%d): %v", runID, err)
		}
	}
	if opts.Pruned {
		if err := os.Remove(logPath); err != nil {
			t.Fatalf("Remove(%q): %v", logPath, err)
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

func detailStore(t *testing.T, logPath string, startedAt, now time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "https://github.com/o/r/issues/76", Repo: "repo", Branch: "cc-76"}
	storetest.Tickets(t, store, ticket)
	storetest.Run(t, store, storetest.RunOpts{
		TicketURL: ticket.URL, Kind: "agent", BaselineSHA: "basesha1234", PromptHash: "hash-1",
		Pgid: 4242, StartedAt: startedAt, LogPath: logPath,
	})
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: true}},
		Worktrees:  map[plan.BranchRef]string{branchRef("repo", "cc-76"): "/repos/repo-cc-76"},
		PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "cc-76"): {
			Number: 76, State: plan.Open, HeadRef: "cc-76",
			Checks: map[string]plan.CheckState{
				"unit":  {Status: "COMPLETED", Conclusion: "SUCCESS"},
				"build": {Status: "IN_PROGRESS", DetailsURL: "https://github.com/o/r/actions/runs/1"},
			},
		}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func writeLog(t *testing.T, n int) string {
	t.Helper()

	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-08-20T12:00:%02dZ",`+
			`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"step %d"}}]}}`+"\n",
			i%60, i)
	}
	return writeRunLog(t, b.String())
}

func sessionPagePath(ticketURL string) string { return view.SessionPath(ticketURL) }

func protectedPushStore(t *testing.T, matches []plan.PathMatch) *storepkg.Store {
	t.Helper()
	st, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", Outcome: plan.OutcomeFailed})
	if err := st.RecordPush(t.Context(), storepkg.Push{
		Ticket: sessionTicket, Tip: "tip", BaseSHA: "base", BaseBranch: "main", At: testNow, Protected: matches,
	}); err != nil {
		t.Fatalf("RecordPush(%q): %v", sessionTicket, err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		PRs:        map[plan.BranchRef]plan.PR{branchRef("acme/web", "cc-1"): {Number: 7, State: plan.Open}},
		Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {}},
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st
}

func TestProtectedChanges(t *testing.T) {
	t.Parallel()

	matches := []plan.PathMatch{{
		Path: ".github/ci.yml", Rule: ".github/**", Tier: plan.TierProtected, Added: 3, Deleted: 1,
	}}

	t.Run("flags the board and rail and lists paths on the session page", func(t *testing.T) {
		t.Parallel()
		st := protectedPushStore(t, matches)
		server := newServer(st, testNow)

		board := htmltest.Parse(t, renderPath(t, server, "/tickets"))
		if got := board.One(t, "a[data-ref] .flag-warning").Text(); got != "protected" {
			t.Errorf("GET /tickets flag = %q, want %q", got, "protected")
		}
		rail := htmltest.Parse(t, renderPath(t, server, "/rail"))
		if got := rail.One(t, ".rail-link .flag-warning").Text(); got != "protected" {
			t.Errorf("GET /rail flag = %q, want %q", got, "protected")
		}

		item := sessionMain(t, st, "/s/acme/web/1").One(t, ".session-protected li")
		sum := sha256.Sum256([]byte(".github/ci.yml"))
		wantHref := fmt.Sprintf("https://github.com/acme/web/pull/7/files#diff-%x", sum)
		if got, _ := item.One(t, "a").Attr("href"); got != wantHref {
			t.Errorf("GET /s/acme/web/1 protected link = %q, want %q", got, wantHref)
		}
		for _, want := range []string{".github/ci.yml", ".github/**", "+3", "−1"} {
			if !strings.Contains(item.Text(), want) {
				t.Errorf("GET /s/acme/web/1 protected item = %q, want it to contain %q", item.Text(), want)
			}
		}
	})

	t.Run("shows nothing when the push touched no protected path", func(t *testing.T) {
		t.Parallel()
		st := protectedPushStore(t, nil)
		server := newServer(st, testNow)

		if got := htmltest.Parse(t, renderPath(t, server, "/tickets")).Find(".flag-warning"); len(got) != 0 {
			t.Errorf("GET /tickets shows %d flags, want none", len(got))
		}
		if got := htmltest.Parse(t, renderPath(t, server, "/rail")).Find(".flag-warning"); len(got) != 0 {
			t.Errorf("GET /rail shows %d flags, want none", len(got))
		}
		if got := sessionMain(t, st, "/s/acme/web/1").Find(".session-protected"); len(got) != 0 {
			t.Errorf("GET /s/acme/web/1 lists protected changes, want none")
		}
	})
}

func sessionMain(t *testing.T, st *storepkg.Store, path string) htmltest.Node {
	t.Helper()
	return htmltest.Parse(t, renderPath(t, newServer(st, testNow), path)).One(t, "main#main")
}

func TestSessionPage(t *testing.T) {
	t.Parallel()

	t.Run("transcript", testSessionTranscript)
	t.Run("stack panel", testSessionStackPanel)

	t.Run("a live v0 ticket shows one polling implement cell that swaps main", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: plan.RunKindAgent, Alive: true})
		main := sessionMain(t, st, "/s/acme/web/1")

		strip := main.One(t, "#step-strip")
		if _, ok := strip.Attr("hx-trigger"); !ok {
			t.Error("GET /s/acme/web/1 step strip has no hx-trigger, want it to poll while a step is live")
		}
		cells := strip.Find("a.step-cell")
		if len(cells) != 1 {
			t.Fatalf("GET /s/acme/web/1 step cells = %d, want 1", len(cells))
		}
		cells[0].One(t, `[data-glyph="running"]`)
		if got := cells[0].Text(); !strings.Contains(got, "implement") {
			t.Errorf("GET /s/acme/web/1 step cell = %q, want it to name implement", got)
		}
		if got, _ := cells[0].Attr("hx-select"); got != "#main" {
			t.Errorf("GET /s/acme/web/1 step cell hx-select = %q, want #main", got)
		}
		if got, _ := cells[0].Attr("href"); !strings.HasPrefix(got, "/s/acme/web/1?run=") {
			t.Errorf("GET /s/acme/web/1 step cell href = %q, want the session route with ?run=", got)
		}
	})

	for _, kind := range []plan.RunKind{plan.RunKindAgent, plan.RunKindFollowUp} {
		t.Run(string(kind)+" shows the header and the prompt as given", func(t *testing.T) {
			t.Parallel()
			prompt := "Implement <b>#1</b>\n  & keep   spacing\n"
			st, _ := sessionStore(t, sessionRunOpts{Kind: kind, Prompt: prompt, KeepPrompt: true, Alive: true})
			main := sessionMain(t, st, "/s/acme/web/1")

			main.One(t, `header.session-head [data-glyph="running"]`)
			main.One(t, "a.back")
			main.One(t, "#session-actions")
			for _, want := range []string{"#1", "acme/web", "checkout"} {
				if !strings.Contains(main.Text(), want) {
					t.Errorf("GET /s/acme/web/1 heading lacks %q", want)
				}
			}
			if got := main.One(t, "details.prompt summary").Text(); !strings.Contains(got, "Implement <b>#1</b>") {
				t.Errorf("GET /s/acme/web/1 closed prompt bar = %q, want the prompt's lead", got)
			}
			if got := main.One(t, "p.session-meta").Text(); !strings.Contains(got, "acme/web #1") {
				t.Errorf("GET /s/acme/web/1 head meta = %q, want repo and ref", got)
			}
			if got := main.One(t, "pre.prompt-text").Text(); !strings.Contains(got, "Implement <b>#1</b>") {
				t.Errorf("GET /s/acme/web/1 prompt = %q, want the literal text %q", got, "Implement <b>#1</b>")
			}
			if scripts := main.Find("pre.prompt-text script"); len(scripts) != 0 {
				t.Errorf("GET /s/acme/web/1 prompt carries a live script element")
			}
		})
	}

	t.Run("a failed session shows its reason and a running one does not", func(t *testing.T) {
		t.Parallel()
		failed, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
		})
		if got := sessionMain(t, failed, "/s/acme/web/1").One(t, ".session-reason").Text(); strings.TrimSpace(got) == "" {
			t.Errorf("failed GET /s/acme/web/1 reason is empty, want the row's reason")
		}

		running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		if reason := sessionMain(t, running, "/s/acme/web/1").Find(".session-reason"); len(reason) != 0 {
			t.Errorf("running GET /s/acme/web/1 shows a reason: %s", reason[0].HTML())
		}
	})

	t.Run("says when the prompt was not kept", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent"})
		main := sessionMain(t, st, "/s/acme/web/1")

		if got := main.Text(); !strings.Contains(got, "not kept") {
			t.Errorf("GET /s/acme/web/1 text = %q, want it to say the prompt was not kept", got)
		}
		if pre := main.Find("pre.prompt-text"); len(pre) != 0 {
			t.Errorf("GET /s/acme/web/1 still shows a pruned prompt: %s", pre[0].HTML())
		}
	})

	t.Run("keeps the prompt and worked-for line after the log is pruned", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "Implement the thing", Pruned: true})
		main := sessionMain(t, st, "/s/acme/web/1")

		if got := main.One(t, "pre.prompt-text").Text(); !strings.Contains(got, "Implement the thing") {
			t.Errorf("GET /s/acme/web/1 prompt = %q, want the stored prompt", got)
		}
		got := main.One(t, ".session-meta .session-worked").Text()
		if !strings.Contains(got, "Worked for 1m 30s") || !strings.Contains(got, "3 turns") {
			t.Errorf("GET /s/acme/web/1 worked line = %q, want stored wall time and turns", got)
		}
	})

	t.Run("the raw tab tables the log and filters it by kind and text", func(t *testing.T) {
		t.Parallel()
		logPath := writeRunLog(t, `{"type":"system","subtype":"init"}`+"\n"+
			`{"type":"assistant","timestamp":"2026-08-20T12:00:01Z","message":{"content":[`+
			`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go vet"}}]}}`+"\n"+
			`{"type":"user","message":{"content":[`+
			`{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"vet: bad"}]}}`+"\n")
		st := detailStore(t, logPath, testNow, testNow)

		tests := []struct {
			name      string
			path      string
			summaries []string
		}{
			{name: "unfiltered", path: "/s/o/r/76?log=raw", summaries: []string{"init", "go vet", "vet: bad"}},
			{name: "by kind", path: "/s/o/r/76?log=raw&kind=assistant", summaries: []string{"go vet"}},
			{name: "by text, any case", path: "/s/o/r/76?log=raw&q=VET", summaries: []string{"go vet", "vet: bad"}},
			{name: "kind and text with no match", path: "/s/o/r/76?log=raw&kind=system&q=vet"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				main := sessionMain(t, st, tt.path)
				var got []string
				for _, summary := range main.Find("table.raw-table summary") {
					got = append(got, summary.Text())
				}
				if diff := cmp.Diff(tt.summaries, got); diff != "" {
					t.Errorf("GET %s summaries mismatch (-want +got):\n%s", tt.path, diff)
				}
			})
		}

		main := sessionMain(t, st, "/s/o/r/76?log=raw")
		if got := len(main.Find(`tr#raw-3 [data-glyph="failed"]`)); got != 1 {
			t.Errorf("GET /s/o/r/76?log=raw failed glyphs on the error row = %d, want 1", got)
		}
		if got := main.One(t, "tr#raw-1 pre").Text(); !strings.Contains(got, `"subtype": "init"`) {
			t.Errorf("GET /s/o/r/76?log=raw row 1 JSON = %q, want the line's JSON", got)
		}
		form := main.One(t, `form[role="search"]`)
		if got, _ := form.Attr("hx-get"); got != "/s/o/r/76" {
			t.Errorf("GET /s/o/r/76?log=raw filter hx-get = %q, want /s/o/r/76", got)
		}
		if got, _ := form.Attr("hx-select"); got != "#main" {
			t.Errorf("GET /s/o/r/76?log=raw filter hx-select = %q, want #main", got)
		}
		form.One(t, `input[type="hidden"][name="log"][value="raw"]`)
	})

	t.Run("shows the worktree to shell into on the branch", func(t *testing.T) {
		t.Parallel()
		main := sessionMain(t, detailStore(t, writeLog(t, 1), testNow, testNow), "/s/o/r/76")
		if got, _ := main.One(t, ".session-meta [title]").Attr("title"); !strings.Contains(got, "/repos/repo-cc-76") {
			t.Errorf("GET /s/o/r/76 branch title = %q, want the worktree path", got)
		}
	})

	t.Run("sets a finished run's answer under a Result head carrying the state glyph", func(t *testing.T) {
		t.Parallel()
		logPath := writeRunLog(t, `{"type":"assistant","timestamp":"2026-08-20T12:00:00Z",`+
			`"message":{"content":[{"type":"text","text":"Shipped the rail."}]}}`+"\n"+
			`{"type":"result","subtype":"success","duration_ms":500,"num_turns":1}`+"\n")
		main := sessionMain(t, detailStore(t, logPath, testNow, testNow), "/s/o/r/76")

		result := main.One(t, `section[aria-label="Result"]`)
		if got := result.Text(); !strings.Contains(got, "Result") || !strings.Contains(got, "Shipped the rail.") {
			t.Errorf("GET /s/o/r/76 result = %q, want the Result head over the final answer", got)
		}
		result.One(t, "[data-glyph]")
	})

	t.Run("dropped log kinds never reach the render", func(t *testing.T) {
		t.Parallel()
		logPath := writeRunLog(t, mustReadTestdata(t, "dropped_kinds.jsonl"))
		main := sessionMain(t, detailStore(t, logPath, testNow, testNow), sessionPagePath("https://github.com/o/r/issues/76"))

		for _, dropped := range []string{"deadbeef-session", "pondering-deeply"} {
			if strings.Contains(main.Text(), dropped) {
				t.Errorf("GET /s/o/r/76 text contains dropped kind %q", dropped)
			}
		}
		if !strings.Contains(main.Text(), "echo hi") {
			t.Errorf("GET /s/o/r/76 text lacks %q", "echo hi")
		}
	})

	t.Run("a tool group holding a failure renders closed with its counts", func(t *testing.T) {
		t.Parallel()
		const log = `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":[` +
			`{"type":"tool_use","id":"a","name":"Read","input":{"file_path":"a.go"}},` +
			`{"type":"tool_use","id":"b","name":"Read","input":{"file_path":"b.go"}}]}}` + "\n" +
			`{"type":"user","timestamp":"2026-01-01T00:00:01Z","message":{"content":[` +
			`{"type":"tool_result","tool_use_id":"a","content":"ok"},` +
			`{"type":"tool_result","tool_use_id":"b","is_error":true,"content":"no such file"}]}}` + "\n"
		path := sessionPagePath("https://github.com/o/r/issues/76")
		main := sessionMain(t, detailStore(t, writeRunLog(t, log), testNow, testNow), path)

		if open := main.Find("details[open]"); len(open) != 0 {
			t.Errorf("GET %s open details = %d, want every fold closed despite the failure", path, len(open))
		}
		if text := strings.Join(strings.Fields(main.Text()), " "); !strings.Contains(text, "2 tool calls · 1 failed") {
			t.Errorf("GET %s text = %q, want the group to read 2 tool calls · 1 failed", path, text)
		}
		if got := len(main.Find("details details[data-glyph=failed]")); got != 1 {
			t.Errorf("GET %s failed calls inside the group = %d, want 1", path, got)
		}
	})

	t.Run("connects the transcript to the log stream", func(t *testing.T) {
		t.Parallel()
		logPath := writeLog(t, 3)
		info, err := os.Stat(logPath)
		if err != nil {
			t.Fatalf("Stat(%s): %v", logPath, err)
		}
		ticket := "https://github.com/o/r/issues/76"
		main := sessionMain(t, detailStore(t, logPath, testNow, testNow), sessionPagePath(ticket))

		stream := fmt.Sprintf("/ticket/%s/log?from=%d&records=0", url.PathEscape(ticket), info.Size())
		main.One(t, `[hx-ext="sse"][sse-connect="`+stream+`"][sse-swap="message"][sse-close="end"][hx-swap="beforeend"]`)
		var verbs []string
		for _, call := range main.Find(".call-head") {
			if call.One(t, ".call-arg").Text() == "step 3" {
				verbs = append(verbs, call.One(t, ".call-verb").Text())
			}
		}
		if len(verbs) != 1 || verbs[0] != "Ran" {
			t.Errorf("GET %s step 3 call verbs = %q, want [Ran]", sessionPagePath(ticket), verbs)
		}
	})

	t.Run("renders the edit form prefilled", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		storetest.Tickets(t, st,
			storepkg.Ticket{URL: "https://github.com/o/r/issues/1", Repo: "o/r", Branch: "cc-1-first"},
			storepkg.Ticket{
				URL: "https://github.com/o/r/issues/2", Repo: "o/r", Branch: "cc-2-second",
				BlockedBy: []string{"https://github.com/o/r/issues/1"},
			},
		)

		form := htmltest.Parse(t, renderPath(t, newServer(st, testNow), "/s/o/r/2")).One(t, `form[action="/ticket"]`)
		for name, value := range map[string]string{
			"ticket": "https://github.com/o/r/issues/2", "branch": "cc-2-second",
			"blocked_by": "https://github.com/o/r/issues/1",
		} {
			form.One(t, `input[name="`+name+`"][value="`+value+`"]`)
		}
	})

	t.Run("an unknown or non-numeric ticket is 404", func(t *testing.T) {
		t.Parallel()
		server := newServer(openStore(t), testNow)
		for _, path := range []string{"/s/acme/web/9", "/s/acme/web/nine"} {
			if rec := do(t, server, http.MethodGet, path, req{}); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", path, rec.Code)
			}
		}
	})

	t.Run("shows stored events, one checks grid and the worked summary", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent"})
		started := testNow.Add(-5 * time.Minute)
		obs := plan.Observation{
			ObservedAt: testNow,
			Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: false}},
			PRs: map[plan.BranchRef]plan.PR{branchRef("acme/web", "cc-1"): {
				Number: 7, State: plan.Open, HeadRef: "cc-1",
				Checks: map[string]plan.CheckState{
					"unit": {
						Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: started, CompletedAt: started.Add(48 * time.Second),
					},
					"lint": {
						Status: "COMPLETED", Conclusion: "FAILURE", StartedAt: started, CompletedAt: started.Add(72 * time.Second),
						DetailsURL: "https://github.com/acme/web/actions/runs/9",
					},
				},
			}},
		}
		if err := st.SaveObservation(ctx, obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		for _, e := range []storepkg.Event{
			{Kind: "pushed", Detail: "pushed abc123 to origin/main"},
			{Kind: "checks_landed", Detail: "unit passed"},
			{Kind: "checks_landed", Detail: "lint failed"},
		} {
			e.At, e.TicketURL = testNow, sessionTicket
			if err := st.AppendEvent(ctx, e); err != nil {
				t.Fatalf("AppendEvent(%s): %v", e.Kind, err)
			}
		}

		got := sessionMain(t, st, "/s/acme/web/1").Text()

		for _, want := range []string{"abc123 to origin/main", "1m 12s", "48s", "output", "Worked"} {
			if !strings.Contains(got, want) {
				t.Errorf("GET /s/acme/web/1 text = %q, want it to contain %q", got, want)
			}
		}
		if strings.Contains(got, "passed unit") {
			t.Errorf("GET /s/acme/web/1 text = %q, want the per-check records folded into the grid", got)
		}
	})

	t.Run("shows the merge line of a run whose log was pruned", func(t *testing.T) {
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

		main := sessionMain(t, st, "/s/acme/web/1")
		if got := main.Text(); !strings.Contains(got, "PR #1 merged") || strings.Contains(got, "Nothing to show yet") {
			t.Errorf("GET /s/acme/web/1 text = %q, want the merge line and no empty-log claim", got)
		}
	})

	t.Run("the composer follows the state", func(t *testing.T) {
		t.Parallel()
		running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		main := sessionMain(t, running, "/s/acme/web/1")
		if got := main.One(t, ".composer-running").Text(); !strings.Contains(got, "running") {
			t.Errorf("running GET /s/acme/web/1 status = %q, want it to say running", got)
		}
		main.One(t, `button[value="kill"]`)
		main.One(t, `nav[aria-label="log view"] a[href="/s/acme/web/1?log=raw"]`)
		composer := main.One(t, ".composer")
		composer.One(t, `input[name="verb"][value="follow-up"]`)
		composer.One(t, `textarea[aria-label="follow-up prompt"]`)
		composer.One(t, `select[name="effort"]`)
		if got := composer.One(t, `select[name="model"] option[selected]`).Text(); got != config.AgentModel {
			t.Errorf("running GET /s/acme/web/1 selected model = %q, want %q", got, config.AgentModel)
		}

		idle, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
		})
		main = sessionMain(t, idle, "/s/acme/web/1")
		main.One(t, ".composer").One(t, `input[name="verb"][value="follow-up"]`)
		main.One(t, `button[value="re-run"]`)
		if status := main.Find(".composer-running"); len(status) != 0 {
			t.Errorf("failed GET /s/acme/web/1 claims to be running: %s", status[0].HTML())
		}
	})

	t.Run("a running session shows a queued message until the loop consumes it", func(t *testing.T) {
		t.Parallel()
		running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		if _, err := running.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: sessionTicket, Verb: plan.VerbFollowUp, Payload: "use sqlite instead", At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent(follow-up): %v", err)
		}

		main := sessionMain(t, running, "/s/acme/web/1")
		if got := main.One(t, ".pending-message").Text(); !strings.Contains(got, "use sqlite instead") {
			t.Errorf("GET /s/acme/web/1 queued message = %q, want use sqlite instead", got)
		}
		main.One(t, ".composer")
	})

	t.Run("verb keys anchor only where the state offers them", func(t *testing.T) {
		t.Parallel()
		running, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		failed, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
		})
		for _, tt := range []struct {
			name            string
			store           *storepkg.Store
			present, absent []string
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
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				main := sessionMain(t, tt.store, "/s/acme/web/1")
				for _, selector := range tt.present {
					main.One(t, selector)
				}
				for _, selector := range tt.absent {
					if unwanted := main.Find(selector); len(unwanted) != 0 {
						t.Errorf("%s GET /s/acme/web/1 carries %s", tt.name, selector)
					}
				}
			})
		}
	})

	t.Run("a merged session anchors remove-worktree and open PR", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomePush,
		})
		obs := plan.Observation{
			ObservedAt: testNow,
			Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: false}},
			PRs: map[plan.BranchRef]plan.PR{
				branchRef("acme/web", "cc-1"): {Number: 7, HeadRef: "cc-1", State: plan.Merged, MergedAt: testNow},
			},
		}
		if err := st.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		root := htmltest.Parse(t, renderPath(t, newServer(st, testNow), "/s/acme/web/1/actions"))
		root.One(t, `button[value="remove-worktree"][data-key="e"]`)
		root.One(t, `a[data-key="o"][target="_blank"]`)
	})

	t.Run("a session asks before kill, on both of its pages", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: plan.RunKindAgent, Prompt: "p", KeepPrompt: true, Alive: true})
		if _, err := st.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: sessionTicket, Verb: plan.VerbKill, At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent(kill): %v", err)
		}
		srv := newServer(st, testNow)
		for _, path := range []string{"/s/acme/web/1", "/s/acme/web/1/actions"} {
			actions := htmltest.Parse(t, renderPath(t, srv, path)).One(t, "#session-actions")
			form := actions.One(t, `form[hx-post="/verb"]`)
			form.One(t, `button[name="verb"][value="kill"][disabled]`)
			actions.One(t, `.pending-mark[data-glyph="pending"]`)
			if confirm, _ := form.Attr("hx-confirm"); !strings.Contains(confirm, "kill on "+sessionTicket) {
				t.Errorf("GET %s kill confirmation = %q, want it to name kill on %s", path, confirm, sessionTicket)
			}
		}
	})

	t.Run("the feature link carries the feature key", func(t *testing.T) {
		t.Parallel()
		withFeature, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		noFeature, _ := sessionStoreInFeature(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true, Outcome: plan.OutcomePush,
		})

		sessionMain(t, withFeature, "/s/acme/web/1").One(t, `a[href="/f/checkout/graph"][data-key="f"]`)
		if keys := sessionMain(t, noFeature, "/s/acme/web/1").Find(`[data-key="f"]`); len(keys) != 0 {
			t.Errorf("GET /s/acme/web/1 with no feature carries the feature key: %s", keys[0].HTML())
		}
	})

	t.Run("the breadcrumb links to the feature with the nav attributes", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})

		sessionMain(t, st, "/s/acme/web/1").One(t, `ol.crumbs a[href="/f/checkout"][hx-select="#main"][hx-push-url="true"]`)
	})

	t.Run("the composer is outside every polled element", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		main := sessionMain(t, st, "/s/acme/web/1")

		main.One(t, `textarea[name="prompt"]`)
		if composer := main.One(t, "#session-actions").Find(`textarea[name="prompt"]`); len(composer) != 0 {
			t.Errorf("GET /s/acme/web/1 polls the composer inside #session-actions")
		}
	})
}

func testVerbFromSession(t *testing.T) {
	t.Parallel()

	t.Run("an htmx verb toasts and shows pending", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		srv := newServer(st, testNow)

		rec := do(t, srv, http.MethodPost, "/verb", req{
			Form:   url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}},
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("htmx POST /verb from session status = %d, want 200", rec.Code)
		}
		root := htmltest.Parse(t, rec.Body)
		if got := root.One(t, `[role="status"]`).Text(); !strings.Contains(got, "kill") {
			t.Errorf("htmx POST /verb from session toast = %q, want it to name kill", got)
		}
		root.One(t, `[data-glyph="pending"]`)
		root.One(t, `button[value="kill"][disabled]`)

		poll := htmltest.Parse(t, renderPath(t, srv, "/s/acme/web/1/actions"))
		poll.One(t, `[data-glyph="pending"]`)
		if toasts := poll.Find(".toast"); len(toasts) != 0 {
			t.Errorf("GET /s/acme/web/1/actions repeats the toast: %s", toasts[0].HTML())
		}
	})

	t.Run("undo on the toast withdraws the queued verb", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		srv := newServer(st, testNow)
		hxHeader := http.Header{"Hx-Request": {"true"}}

		queued := htmltest.Parse(t, do(t, srv, http.MethodPost, "/verb", req{
			Form:   url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}},
			Header: hxHeader,
		}).Body)
		undo := queued.One(t, `.toast form[hx-post="/verb/undo"]`)
		form := url.Values{}
		for _, name := range []string{"ticket", "intent", "verb"} {
			value, _ := undo.One(t, `input[name="`+name+`"]`).Attr("value")
			form.Set(name, value)
		}

		rec := do(t, srv, http.MethodPost, "/verb/undo", req{Form: form, Header: hxHeader})
		if got := htmltest.Parse(t, rec.Body).One(t, `[role="status"]`).Text(); !strings.Contains(got, "Undid kill") {
			t.Errorf("POST /verb/undo toast = %q, want it to say Undid kill", got)
		}
		if got := pendingVerbs(t, st, "kill"); len(got) != 0 {
			t.Errorf("pending kill intents after undo = %v, want none", got)
		}

		again := do(t, srv, http.MethodPost, "/verb/undo", req{Form: form, Header: hxHeader})
		if got := htmltest.Parse(t, again.Body).One(t, `[role="status"]`).Text(); !strings.Contains(got, "Too late") {
			t.Errorf("second POST /verb/undo toast = %q, want Too late", got)
		}
		if plain := do(t, srv, http.MethodPost, "/verb/undo", req{Form: form}); plain.Code != http.StatusConflict {
			t.Errorf("plain POST /verb/undo of a withdrawn intent = %d, want 409", plain.Code)
		}
	})

	t.Run("remove-worktree on a merged session offers no undo", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomePush,
		})
		obs := plan.Observation{
			ObservedAt: testNow,
			Runs:       map[plan.TicketURL]plan.RunObservation{sessionTicket: {Alive: false}},
			PRs: map[plan.BranchRef]plan.PR{
				branchRef("acme/web", "cc-1"): {Number: 7, HeadRef: "cc-1", State: plan.Merged, MergedAt: testNow},
			},
		}
		if err := st.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		queued := htmltest.Parse(t, do(t, newServer(st, testNow), http.MethodPost, "/verb", req{
			Form:   url.Values{"verb": {"remove-worktree"}, "ticket": {sessionTicket}, "from": {"session"}},
			Header: http.Header{"Hx-Request": {"true"}},
		}).Body)
		if undo := queued.Find(`.toast form[hx-post="/verb/undo"]`); len(undo) != 0 {
			t.Errorf("htmx POST /verb remove-worktree on a merged session offers undo: %s", undo[0].HTML())
		}
		pending, err := st.PendingVerbIntents(t.Context(), plan.VerbRemoveWorktree)
		if err != nil {
			t.Fatalf("PendingVerbIntents(remove-worktree): %v", err)
		}
		if len(pending) != 1 || !pending[0].HoldUntil.IsZero() {
			t.Errorf("pending remove-worktree intents = %+v, want one with no hold", pending)
		}
	})

	t.Run("a plain verb redirects back to the session", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
		form := url.Values{"verb": {"kill"}, "ticket": {sessionTicket}, "from": {"session"}}

		rec := do(t, newServer(st, testNow), http.MethodPost, "/verb", req{Form: form})
		if rec.Code != http.StatusSeeOther || rec.Location != "/s/acme/web/1" {
			t.Errorf("POST /verb from session = %d %q, want 303 /s/acme/web/1", rec.Code, rec.Location)
		}
	})

	t.Run("a follow-up queues the prompt and refuses a blank one", func(t *testing.T) {
		t.Parallel()
		st, _ := sessionStoreEnding(t, sessionRunOpts{
			Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
		})
		srv := newServer(st, testNow)

		rec := do(t, srv, http.MethodPost, "/verb", req{
			Form: url.Values{
				"verb": {"follow-up"}, "ticket": {sessionTicket}, "from": {"session"}, "prompt": {"try again"},
				"model": {"opus"}, "effort": {"high"},
			},
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("htmx POST /verb follow-up status = %d, want 200", rec.Code)
		}
		if got := htmltest.Parse(t, rec.Body).One(t, `[role="status"]`).Text(); !strings.Contains(got, "follow-up") {
			t.Errorf("htmx POST /verb follow-up toast = %q, want it to name follow-up", got)
		}
		rec = do(t, srv, http.MethodPost, "/verb", req{
			Form:   url.Values{"verb": {"follow-up"}, "ticket": {sessionTicket}, "from": {"session"}},
			Header: http.Header{"Hx-Request": {"true"}},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("htmx POST /verb blank follow-up status = %d, want 422", rec.Code)
		}
	})
}
