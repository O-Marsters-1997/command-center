package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func seedRun(t *testing.T, store *storepkg.Store, ticketURL string) int64 {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticketURL, err)
	}
	if err := store.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: testNow, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	return runID
}

func seededRunning(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	seedRun(t, store, "sandbox://CC-1")
	obs, _, err := store.LastObservation(t.Context())
	if err != nil {
		t.Fatalf("LastObservation: %v", err)
	}
	obs.Runs = map[plan.TicketURL]plan.RunObservation{"sandbox://CC-1": {Alive: true}}
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func seededFailed(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	runID := seedRun(t, store, "sandbox://CC-1")
	exitCode := 1
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: testNow,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
	return store
}

func seededQueued(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	now := testNow
	if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-1", At: now,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}
	if err := store.ApplyLaunchIntents(t.Context(), now); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	return store
}

func seededStore(t *testing.T, observedAt time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	storetest.Tickets(t, store, tickets...)

	obs := plan.Observation{
		ObservedAt: observedAt,
		PRs:        map[plan.BranchRef]plan.PR{},
		Worktrees:  map[plan.BranchRef]string{branchRef("cc-sandbox", "cc-1-first"): "/repos/cc-sandbox-cc-1-first"},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	tickErr := storepkg.TickError{At: observedAt.Add(15 * time.Second), Message: "observe: gh pr list: exit status 1"}
	if err := store.RecordTickError(ctx, tickErr); err != nil {
		t.Fatalf("RecordTickError: %v", err)
	}
	return store
}

func seededServer(t *testing.T) *web.Server {
	t.Helper()

	observedAt := testNow
	store := seededStore(t, observedAt)
	if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: observedAt,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}
	return newServer(store, observedAt.Add(45*time.Second))
}

func TestServerRendersTheShellAroundTheBoard(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	full := renderPath(t, server, "/tickets")
	swap := renderPath(t, server, "/board?all=1")

	if !strings.HasPrefix(swap, `<table id="board"`) {
		t.Fatalf("GET /board did not render a table: prefix %q", swap[:min(len(swap), 120)])
	}
	nested := strings.TrimSpace(swap)
	if !strings.Contains(full, nested) {
		t.Fatalf("GET /tickets does not nest the GET /board bytes verbatim; board refs = %v, page refs = %v",
			ticketRefs(webtest.BoardRows(t, swap)), ticketRefs(webtest.BoardRows(t, full)))
	}
}

func TestPageRendersTheParentsVerdictOnAStackedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	storetest.Tickets(t, store, tickets...)

	at := testNow
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	dispositionAsPushed(t, store, "sandbox://CHILD", at)
	const parentTip, childTip = "parent-tip", "child-tip"
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: "sandbox://PARENT", Tip: parentTip, Base: "main", BaseTip: "main-tip", At: at,
	})
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: "sandbox://CHILD", Tip: childTip, Base: "parent", BaseTip: parentTip, At: at,
	})

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{
			branchRef("repo", "parent"): "/repos/parent", branchRef("repo", "child"): "/repos/child",
		},
		BranchTips: map[plan.BranchRef]plan.SHA{
			branchRef("repo", "parent"): plan.SHA(parentTip), branchRef("repo", plan.DefaultBaseBranch): "main-tip",
		},
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "parent"): {
				Number: 1, State: plan.Open, HeadOid: parentTip,
				Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
			branchRef("repo", "child"): {
				Number: 2, State: plan.Open, HeadOid: childTip,
				Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			},
		},
		Settings: map[string]config.RepoSettings{"repo": {Stacking: true, Checks: verdict.Predicate{Success: "CI"}}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	server := openServer(track(t, store, repos...), cctest.Clock{At: at}, "")
	page := renderPage(t, server)

	parent := webtest.RowFor(t, page, "sandbox://PARENT")
	if state := parent.State; state != "ci_failed" {
		t.Fatalf("parent's state = %q, want ci_failed:\n%s", state, parent.Row.HTML())
	}

	child := webtest.RowFor(t, page, "sandbox://CHILD")
	if got := child.Row.HTML(); !strings.Contains(got, `title="base verdict: ci_failed"`) {
		t.Errorf("child's rendered base verdict flag = %q, want ci_failed (the parent's own verdict)", got)
	}
}

func TestCIFailedRowLinksEachRedRequiredCheck(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CI", Repo: "repo", Branch: "ci"}
	storetest.Tickets(t, store, ticket)

	at := testNow
	dispositionAsPushed(t, store, ticket.URL, at)
	const tip = "ci-tip"
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: ticket.URL, Tip: tip, Base: "main", BaseTip: "main-tip", At: at,
	})

	obs := plan.Observation{
		BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "ci"): {
				Number: 1, State: plan.Open, HeadOid: tip,
				Checks: map[string]plan.CheckState{
					"CI":     {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/1"},
					"Deploy": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/2"},
					"Lint":   {Status: "COMPLETED", Conclusion: "FAILURE"},
				},
			},
		},
		Settings: map[string]config.RepoSettings{"repo": {Checks: verdict.Predicate{AllOf: []verdict.Predicate{
			{Success: "CI"}, {Success: "Deploy"}, {Success: "Lint"},
		}}}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	server := openServer(track(t, store, repos...), cctest.Clock{At: at}, "")
	page := renderPage(t, server)

	row := webtest.RowFor(t, page, ticket.URL)
	if row.State != "ci_failed" {
		t.Fatalf("state = %q, want ci_failed:\n%s", row.State, row.Row.HTML())
	}
	reason := row.Ticket.HTML()
	links := row.Ticket.Find("a[href]")
	for _, tc := range []struct{ href, name string }{
		{"https://github.com/o/r/actions/runs/1", "CI"},
		{"https://github.com/o/r/actions/runs/2", "Deploy"},
	} {
		matches := row.Ticket.Find(`a[href="` + tc.href + `"]`)
		if len(matches) != 1 {
			t.Errorf("found %d links for %s, want one:\n%s", len(matches), tc.href, row.Row.HTML())
			continue
		}
		link := matches[0]
		if link.Text() != tc.name {
			t.Errorf("link for %s = %q, want %q:\n%s", tc.href, link.Text(), tc.name, row.Row.HTML())
		}
		if target, _ := link.Attr("target"); target != "_blank" {
			t.Errorf("link target = %q, want _blank:\n%s", target, row.Row.HTML())
		}
		if rel, _ := link.Attr("rel"); rel != "noopener" {
			t.Errorf("link rel = %q, want noopener:\n%s", rel, row.Row.HTML())
		}
	}
	if got := len(links); got != 2 {
		t.Errorf("reason has %d links, want 2 (one per red check with a DetailsURL):\n%s", got, row.Row.HTML())
	}
	if !strings.Contains(reason, "Lint") {
		t.Errorf("Lint is not named:\n%s", row.Row.HTML())
	}
	if strings.Contains(reason, `<a href=""`) {
		t.Errorf("Lint has no DetailsURL and should not render an empty anchor:\n%s", row.Row.HTML())
	}
}

func TestPageRendersWaitingOnProducerDeployWhenOnlyTheCompatCheckIsRed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	storetest.Tickets(t, store, ticket)

	at := testNow
	dispositionAsPushed(t, store, "sandbox://CC-1", at)
	const tip = "cc-1-tip"
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: "sandbox://CC-1", Tip: tip, Base: "main", BaseTip: "main-tip", At: at,
	})

	obs := plan.Observation{
		Worktrees:  map[plan.BranchRef]string{branchRef("repo", "cc-1"): "/repos/cc-1"},
		BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "cc-1"): {
				Number: 1, State: plan.Open, HeadOid: tip,
				Checks: map[string]plan.CheckState{
					"GraphQL production compatibility": {Status: "COMPLETED", Conclusion: "FAILURE"},
					"Tests":                            {Status: "COMPLETED", Conclusion: "SUCCESS"},
				},
			},
		},
		Settings: map[string]config.RepoSettings{"repo": {
			CompatCheck: "GraphQL production compatibility",
			Checks: verdict.Predicate{AllOf: []verdict.Predicate{
				{Success: "GraphQL production compatibility"}, {Success: "Tests"},
			}},
		}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	server := openServer(track(t, store, repos...), cctest.Clock{At: at}, "")
	page := renderPage(t, server)

	row := webtest.RowFor(t, page, "sandbox://CC-1")
	if state := row.State; state != "waiting_on_producer_deploy" {
		t.Fatalf("state = %q, want waiting_on_producer_deploy:\n%s", state, row.Row.HTML())
	}
	row.Row.One(t, `button[name="verb"][value="re-run"]`)
}

func TestServerRejectsUnknownPaths(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), cctest.Clock{At: testNow}, "")

	rec := do(t, server, http.MethodGet, "/nope", req{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func seededStoreAtTestNow(t *testing.T) *storepkg.Store {
	t.Helper()
	return seededStore(t, testNow)
}

func pendingMutationIntentCount(t *testing.T, store *storepkg.Store) int {
	t.Helper()
	byTicket, err := store.PendingIntentsByTicket(t.Context())
	if err != nil {
		t.Fatalf("PendingIntentsByTicket: %v", err)
	}
	count := 0
	for _, intents := range byTicket {
		count += len(intents)
	}
	edits, err := store.PendingEditTicketIntents(t.Context())
	if err != nil {
		t.Fatalf("PendingEditTicketIntents: %v", err)
	}
	return count + len(edits)
}

func TestMutationRoutesCheckOrigin(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name   string
		target string
		form   url.Values
		seed   func(*testing.T) *storepkg.Store
	}{
		{name: "launch", target: "/launch?ticket=sandbox://CC-1", seed: seededStoreAtTestNow},
		{name: "verb", target: "/verb?verb=kill&ticket=sandbox://CC-1", seed: seededRunning},
		{name: "ticket", target: "/ticket", form: url.Values{
			"ticket": {"sandbox://CC-2"}, "branch": {"cc-2-second"},
		}, seed: seededStoreAtTestNow},
		{name: "launch/open", target: "/launch/open", form: url.Values{"feature": {"x"}}, seed: openStore},
		{name: "feature import", target: "/features/project:x/import", seed: openStore},
	}
	origins := []struct {
		name, value string
		wantStatus  int
		wantIntents int
	}{
		{name: "foreign", value: "http://evil.example", wantStatus: http.StatusForbidden},
		{name: "missing", value: "-", wantStatus: http.StatusSeeOther, wantIntents: 1},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range origins {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					store := route.seed(t)
					before := pendingMutationIntentCount(t, store)
					beforeTickets, err := store.Tickets(t.Context())
					if err != nil {
						t.Fatalf("Tickets(before POST %s): %v", route.target, err)
					}
					got := do(t, newServer(store, testNow), http.MethodPost, route.target, req{
						Form: route.form, Origin: tc.value,
					})
					if got.Code != tc.wantStatus {
						t.Errorf("POST %s = %d, want %d", route.target, got.Code, tc.wantStatus)
					}
					if after := pendingMutationIntentCount(t, store); after != before+tc.wantIntents {
						t.Errorf("pending intents = %d, want %d", after, before+tc.wantIntents)
					}
					afterTickets, err := store.Tickets(t.Context())
					if err != nil {
						t.Fatalf("Tickets(after POST %s): %v", route.target, err)
					}
					if diff := cmp.Diff(beforeTickets, afterTickets); diff != "" {
						t.Errorf("tickets changed before reconciliation (-before +after):\n%s", diff)
					}
				})
			}
		})
	}
}

func TestMutationRoutesAllowSameOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, target string
		form         url.Values
		seed         func(*testing.T) *storepkg.Store
	}{
		{
			name: "launch same origin", target: "/launch?ticket=sandbox://CC-1",
			seed: seededStoreAtTestNow,
		},
		{name: "verb same origin", target: "/verb?verb=kill&ticket=sandbox://CC-1", seed: seededRunning},
		{
			name: "ticket same origin", target: "/ticket",
			form: url.Values{"ticket": {"sandbox://CC-2"}, "branch": {"cc-2-second"}},
			seed: seededStoreAtTestNow,
		},
		{name: "launch/open same origin", target: "/launch/open", form: url.Values{"feature": {"x"}}, seed: openStore},
		{name: "feature import same origin", target: "/features/project:x/import", seed: openStore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := do(t, newServer(tc.seed(t), testNow), http.MethodPost, tc.target, req{Form: tc.form})
			if got.Code != http.StatusSeeOther {
				t.Errorf("POST %s = %d, want 303", tc.target, got.Code)
			}
		})
	}
}

func TestMutationRoutesRejectWrongMethod(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	server := newServer(store, testNow)
	for _, target := range []string{
		"/launch?ticket=sandbox://CC-1", "/verb?verb=kill&ticket=sandbox://CC-1",
		"/ticket", "/launch/open", "/features/project:x/import",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			got := do(t, server, http.MethodPut, target, req{})
			if got.Code != http.StatusMethodNotAllowed {
				t.Errorf("PUT %s = %d, want 405", target, got.Code)
			}
		})
	}
}

func TestLaunchOpenRedirectsToFeature(t *testing.T) {
	t.Parallel()
	got := do(t, newServer(openStore(t), testNow), http.MethodPost, "/launch/open", req{
		Form: url.Values{"feature": {"x"}}, Origin: "-",
	})
	if got.Code != http.StatusSeeOther || got.Location != "/f/x/launch" {
		t.Errorf("POST /launch/open = %d %q, want 303 /f/x/launch", got.Code, got.Location)
	}
}

func TestLaunchAcceptsASameOriginPost(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), cctest.Clock{At: testNow}, "")
	got := do(t, server, http.MethodPost, "/launch?ticket=sandbox://CC-1", req{})
	if got.Code != http.StatusSeeOther || got.Location != "/" {
		t.Errorf("POST /launch = %d %q, want 303 /", got.Code, got.Location)
	}
}

func TestGetPreviewIsGone(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), cctest.Clock{At: testNow}, "")
	got := do(t, server, http.MethodGet, "/preview?ticket=sandbox://CC-1", req{})
	if got.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got.Code)
	}
}

func TestCandidatesAndLaunchHandleAnArbitrarilySizedSlice(t *testing.T) {
	t.Parallel()

	const fanOut = 50
	ctx := t.Context()
	store := openStore(t)

	root := storepkg.Ticket{URL: "sandbox://CC-0", Repo: "cc-sandbox", Branch: "cc-0"}
	tickets := []storepkg.Ticket{root}
	query := "ticket=" + root.URL
	for i := 1; i <= fanOut; i++ {
		ticketURL := fmt.Sprintf("sandbox://CC-%d", i)
		tickets = append(tickets, storepkg.Ticket{
			URL: ticketURL, Repo: "cc-sandbox", Branch: fmt.Sprintf("cc-%d", i),
			BlockedBy: []string{root.URL},
		})
		query += "&ticket=" + ticketURL
	}
	storetest.Tickets(t, store, tickets...)
	obs := plan.Observation{ObservedAt: testNow, PRs: map[plan.BranchRef]plan.PR{}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	server := openServer(store, cctest.Clock{At: testNow}, "")
	candidateResp := do(t, server, http.MethodGet, "/launch/candidates.json?"+query, req{})
	if candidateResp.Code != http.StatusOK {
		t.Fatalf("GET candidates = %d, want 200: %s", candidateResp.Code, candidateResp.Body)
	}
	var candidates []jsonCandidate
	if err := json.Unmarshal([]byte(candidateResp.Body), &candidates); err != nil {
		t.Fatalf("Unmarshal(candidates): %v", err)
	}
	if got := len(candidates); got != fanOut+1 {
		t.Fatalf("candidates = %d, want %d: the slice must not be truncated", got, fanOut+1)
	}

	launchResp := do(t, server, http.MethodPost, "/launch?"+query, req{})
	if launchResp.Code != http.StatusSeeOther || launchResp.Location != "/" {
		t.Fatalf("POST /launch = %d %q, want 303 /", launchResp.Code, launchResp.Location)
	}

	if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if len(memberships) != fanOut+1 {
		t.Fatalf("authorised memberships = %d, want %d: the slice must not be truncated", len(memberships), fanOut+1)
	}
}

func runningRowStore(t *testing.T, ticket storepkg.Ticket, startedAt, now time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	storetest.Tickets(t, store, ticket)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticket.URL, err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: startedAt, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: true}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func TestServerRendersARunningRowWithPgidAndElapsed(t *testing.T) {
	t.Parallel()

	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)

	server := newServer(store, now)
	rec := do(t, server, http.MethodGet, "/tickets", req{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	row := webtest.RowFor(t, rec.Body, ticket.URL)
	if got := row.State; got != "running" {
		t.Errorf("row state = %q, want running: %s", got, row.Row.HTML())
	}
	if got := row.Elapsed.Text(); got != "1m30s" {
		t.Errorf("elapsed = %q, want 1m30s: %s", got, row.Row.HTML())
	}
	row.Row.One(t, `button[name="verb"][value="kill"]`)
}

func TestLaunchStoresTheComposedHash(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, store, ticket)
	obs := plan.Observation{ObservedAt: testNow, PRs: map[plan.BranchRef]plan.PR{}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	server := openServer(store, cctest.Clock{At: testNow}, "")
	got := do(t, server, http.MethodPost, "/launch?ticket=sandbox://CC-1", req{})
	if got.Code != http.StatusSeeOther || got.Location != "/" {
		t.Fatalf("POST /launch = %d %q, want 303 /", got.Code, got.Location)
	}

	if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	want := plan.Hash(plan.Compose(plan.Ticket{URL: plan.TicketURL(ticket.URL)}))
	if got := hashes[ticket.URL]; got != want {
		t.Errorf("stored prompt_hash = %q, want %q", got, want)
	}
}

func TestPageLinksTheBuiltStylesheet(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	server := newServer(seededStore(t, observedAt), observedAt)

	rec := do(t, server, http.MethodGet, "/tickets", req{})

	head := htmltest.Parse(t, rec.Body).One(t, "head")
	head.One(t, `link[rel="stylesheet"][href="/assets/app.css"]`)
	if styles := head.Find("style"); len(styles) != 0 {
		t.Errorf("page still inlines a stylesheet: %s", styles[0].HTML())
	}

	css := do(t, server, http.MethodGet, "/page.css", req{})
	if css.Code != http.StatusNotFound {
		t.Errorf("GET /page.css = %d, want 404", css.Code)
	}
}

func TestServesTheBuiltStylesheet(t *testing.T) {
	t.Parallel()

	requireBuiltStylesheet(t)
	observedAt := testNow
	server := newServer(seededStore(t, observedAt), observedAt)

	got := do(t, server, http.MethodGet, "/assets/app.css", req{})
	if got.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.css = %d, want 200", got.Code)
	}
	if contentType := got.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", contentType)
	}
	for _, want := range []string{"--color-s-attention", ".glyph", "inter-variable.woff2"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("built stylesheet is missing %q", want)
		}
	}
}

func TestPageShowsQueuedVerbsBesideTheState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)
	server := newServer(store, now)

	if err := store.QueueVerbIntent(ctx, ticket.URL, "kill", now); err != nil {
		t.Fatalf("QueueVerbIntent(kill): %v", err)
	}
	page := renderPage(t, server)
	row := webtest.RowFor(t, page, ticket.URL)
	if got := row.State; got != "running · kill queued" {
		t.Errorf("state = %q, want running · kill queued:\n%s", got, row.Row.HTML())
	}
	row.Row.One(t, `button[name="verb"][value="kill"]`)

	if err := store.QueueVerbIntent(ctx, ticket.URL, "follow-up", now.Add(time.Second)); err != nil {
		t.Fatalf("QueueVerbIntent(follow-up): %v", err)
	}
	queued := webtest.RowFor(t, renderPage(t, server), ticket.URL)
	if got := queued.State; got != "running · kill queued · follow-up queued" {
		t.Errorf("state = %q, want both queued verbs:\n%s", got, queued.Row.HTML())
	}

	pending, err := store.PendingVerbIntents(ctx, "kill")
	if err != nil {
		t.Fatalf("PendingVerbIntents(kill): %v", err)
	}
	if err := store.ConsumeVerbIntent(ctx, pending[0].ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("ConsumeVerbIntent(%d): %v", pending[0].ID, err)
	}
	page = renderPage(t, server)
	row = webtest.RowFor(t, page, ticket.URL)
	if got := row.State; got != "running · follow-up queued" {
		t.Errorf("state = %q, want consumed kill gone:\n%s", got, row.Row.HTML())
	}
	row.Row.One(t, `button[name="verb"][value="kill"]`)
}

func TestPageShowsAQueuedLaunchBeforeTheTickAuthorisesIt(t *testing.T) {
	t.Parallel()

	now := testNow
	store := seededStore(t, now)
	if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: now,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}

	server := newServer(store, now)
	row := webtest.RowFor(t, renderPage(t, server), "sandbox://CC-1")
	if got := row.State; got != "ready · launch queued" {
		t.Errorf("state = %q, want ready · launch queued:\n%s", got, row.Row.HTML())
	}

	if err := store.ApplyLaunchIntents(t.Context(), now.Add(15*time.Second)); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	row = webtest.RowFor(t, renderPage(t, server), "sandbox://CC-1")
	if got := row.State; got != "queued" {
		t.Errorf("state = %q, want queued after tick:\n%s", got, row.Row.HTML())
	}
}

func TestLaunchRefusesASubmittedHashThatNoLongerComposes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
	}
	storetest.Tickets(t, store, tickets...)
	obs := plan.Observation{ObservedAt: testNow, PRs: map[plan.BranchRef]plan.PR{}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	server := openServer(store, cctest.Clock{At: testNow}, "")

	previewed := plan.Hash("a prompt sandbox://CC-2 no longer composes to")
	recomposed := plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-2"}))

	form := url.Values{
		"ticket": {"sandbox://CC-1", "sandbox://CC-2"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-1"})),
			"sandbox://CC-2 " + previewed,
		},
	}
	got := do(t, server, http.MethodPost, "/launch", req{Form: form})
	if got.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", got.Code)
	}
	for _, want := range []string{"sandbox://CC-2", previewed, recomposed} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("409 body does not name %q:\n%s", want, got.Body)
		}
	}

	if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("active launch hashes = %v, want nothing queued", hashes)
	}
}

func TestLaunchIgnoresTheHashOfAnUncheckedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := seededStore(t, testNow)
	server := openServer(store, cctest.Clock{At: testNow}, "")

	form := url.Values{
		"ticket": {"sandbox://CC-1"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-1"})),
			"sandbox://CC-2 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-2"})),
		},
	}
	got := do(t, server, http.MethodPost, "/launch", req{Form: form})
	if got.Code != http.StatusSeeOther || got.Location != "/" {
		t.Fatalf("POST /launch = %d %q, want 303 /", got.Code, got.Location)
	}

	if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	if _, ok := hashes["sandbox://CC-1"]; !ok || len(hashes) != 1 {
		t.Errorf("active launch hashes = %v, want only sandbox://CC-1", hashes)
	}
}

func TestLaunchRejectsAMalformedHashField(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), cctest.Clock{At: testNow}, "")

	form := url.Values{"ticket": {"sandbox://CC-1"}, "hash": {"deadbeef"}}
	got := do(t, server, http.MethodPost, "/launch", req{Form: form})
	if got.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got.Code)
	}
}

func TestTheBoardDerivesWithTheChecksTheObservationCarries(t *testing.T) {
	t.Parallel()

	rowWith := func(t *testing.T, checks verdict.Predicate) webtest.BoardRow {
		t.Helper()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CI", Repo: "repo", Branch: "ci"}
		storetest.Tickets(t, store, ticket)
		at := testNow
		dispositionAsPushed(t, store, ticket.URL, at)
		storetest.Pushed(t, store, storetest.PushedOpts{
			URL: ticket.URL, Tip: "ci-tip", Base: "main", BaseTip: "main-tip", At: at,
		})
		obs := plan.Observation{
			BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
			PRs: map[plan.BranchRef]plan.PR{
				branchRef("repo", "ci"): {
					Number: 1, State: plan.Open, HeadOid: "ci-tip",
					Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
				},
			},
			Settings: map[string]config.RepoSettings{"repo": {Checks: checks}},
		}
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		server := openServer(track(t, store, named("repo")...), cctest.Clock{At: at}, "")
		return webtest.RowFor(t, renderPage(t, server), ticket.URL)
	}

	if got := rowWith(t, verdict.Predicate{Success: "CI"}); got.State != "review_me" {
		t.Fatalf("state with CI required = %q, want review_me:\n%s", got.State, got.Row.HTML())
	}
	if got := rowWith(t, verdict.Predicate{Success: "Lint"}); got.State == "review_me" {
		t.Errorf("state with unreported check = %q, want observed verdict:\n%s", got.State, got.Row.HTML())
	}
}
