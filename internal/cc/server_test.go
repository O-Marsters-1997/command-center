package cc_test

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

var update = flag.Bool("update", false, "regenerate golden files")

const goldenShell = "testdata/shell.golden.html"
const goldenBoard = "testdata/board.golden.html"
const goldenPrompt = "testdata/prompt.golden.txt"

// assertGolden compares got against the golden file at path, rewriting it under -update.
func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("render differs from %s; rerun with -update to accept\n--- got ---\n%s", path, got)
	}
}

// noRedirect defeats http.Client's default of following a 303.
func noRedirect(srv *httptest.Server) *http.Client {
	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func assertSeeOtherHome(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 303: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want %q", got, "/")
	}
}

func seededStore(t *testing.T, observedAt time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		ObservedAt: observedAt,
		PRs:        map[string]gh.PR{},
		Worktrees:  map[string]string{cc.BranchKey("cc-sandbox", "cc-1-first"): "/repos/cc-sandbox-cc-1-first"},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	tickErr := cc.TickError{At: observedAt.Add(15 * time.Second), Message: "observe: gh pr list: exit status 1"}
	if err := store.RecordTickError(ctx, tickErr); err != nil {
		t.Fatal(err)
	}
	return store
}

func seededServer(t *testing.T) *cc.Server {
	t.Helper()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	if err := store.QueueLaunchIntent(t.Context(), "sandbox://CC-1", "hash-1", "group-a", observedAt); err != nil {
		t.Fatal(err)
	}
	return cc.NewServer(store, fixedClock(observedAt.Add(45*time.Second)), nil, "")
}

// TestServerRendersTheShellAroundTheBoard goldens the two fragments separately and pins the join
// between them: GET / must nest the exact bytes GET /board serves, or the poll's swap would
// redraw the board differently from the first paint.
func TestServerRendersTheShellAroundTheBoard(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	full := renderPage(t, server)
	swap := renderBoard(t, server)

	if !strings.HasPrefix(swap, `<table id="board"`) {
		t.Fatalf("GET /board did not render the table, so the join below proves nothing:\n%s", swap)
	}
	rest := full
	for _, part := range splitBoardSwap(t, swap) {
		if !strings.Contains(rest, part.html) {
			t.Errorf("GET / does not nest the GET /board %s bytes verbatim\n--- %s ---\n%s\n--- page ---\n%s",
				part.name, part.name, part.html, full)
			continue
		}
		rest = strings.Replace(rest, part.html, "", 1)
	}
	assertGolden(t, goldenBoard, []byte(swap))
	assertGolden(t, goldenShell, []byte(rest))
}

type swapPart struct{ name, html string }

// splitBoardSwap carves GET /board into the table htmx swaps into its target and the two
// out-of-band fragments riding along with it, so each can be pinned against the first paint.
func splitBoardSwap(t *testing.T, swap string) []swapPart {
	t.Helper()

	masthead := strings.Index(swap, `<div id="masthead"`)
	band := strings.Index(swap, `<section id="band"`)
	if masthead < 0 || band < masthead {
		t.Fatalf("GET /board is not the table, then the masthead, then the band:\n%s", swap)
	}
	return []swapPart{
		{"board", strings.TrimSpace(swap[:masthead])},
		{"masthead", strings.TrimSpace(swap[masthead:band])},
		{"band", strings.TrimSpace(swap[band:])},
	}
}

// TestPageRendersTheParentsVerdictOnAStackedRow covers the last of issue #32's "what to build":
// a red check on a descendant whose base moved may not be its own fault, so the row also renders
// the base's own CI verdict alongside its own.
func TestPageRendersTheParentsVerdictOnAStackedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	dispositionAsPushed(t, store, "sandbox://CHILD", at)
	const parentTip, childTip = "parent-tip", "child-tip"
	if err := store.RecordPush(ctx, "sandbox://PARENT", parentTip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, "sandbox://CHILD", childTip, "parent", parentTip, at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees: map[string]string{
			cc.BranchKey("repo", "parent"): "/repos/parent", cc.BranchKey("repo", "child"): "/repos/child",
		},
		BranchTips: map[string]string{cc.BranchKey("repo", "parent"): parentTip, cc.MainTipKey("repo"): "main-tip"},
		PRs: map[string]gh.PR{
			cc.BranchKey("repo", "parent"): {
				Number: 1, State: gh.Open, HeadOid: parentTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
			cc.BranchKey("repo", "child"): {
				Number: 2, State: gh.Open, HeadOid: childTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if state := rowState(t, page, "sandbox://PARENT"); state != "ci_failed" {
		t.Fatalf("parent's own state = %q, want ci_failed (its CI check failed)", state)
	}

	if got := rowCellAt(t, page, "sandbox://CHILD", 1); !strings.Contains(got, `title="base verdict: ci_failed"`) {
		t.Errorf("child's rendered base verdict flag = %q, want ci_failed (the parent's own verdict)", got)
	}
}

func TestCIFailedRowLinksEachRedRequiredCheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CI", Repo: "repo", Branch: "ci"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)
	const tip = "ci-tip"
	if err := store.RecordPush(ctx, ticket.URL, tip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		BranchTips: map[string]string{cc.MainTipKey("repo"): "main-tip"},
		PRs: map[string]gh.PR{
			cc.BranchKey("repo", "ci"): {
				Number: 1, State: gh.Open, HeadOid: tip,
				Checks: map[string]gh.CheckState{
					"CI":     {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/1"},
					"Deploy": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: "https://github.com/o/r/actions/runs/2"},
					"Lint":   {Status: "COMPLETED", Conclusion: "FAILURE"},
				},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Checks: verdict.Predicate{AllOf: []verdict.Predicate{
		{Success: "CI"}, {Success: "Deploy"}, {Success: "Lint"},
	}}}}
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if state := rowState(t, page, ticket.URL); state != "ci_failed" {
		t.Fatalf("state = %q, want ci_failed", state)
	}

	reason := rowCellAt(t, page, ticket.URL, 3)
	ciLink := `<a href="https://github.com/o/r/actions/runs/1" target="_blank" rel="noopener">CI</a>`
	if !strings.Contains(reason, ciLink) {
		t.Errorf("CI is not linked to its DetailsURL:\n%s", reason)
	}
	deployLink := `<a href="https://github.com/o/r/actions/runs/2" target="_blank" rel="noopener">Deploy</a>`
	if !strings.Contains(reason, deployLink) {
		t.Errorf("Deploy is not linked to its DetailsURL:\n%s", reason)
	}
	if got := strings.Count(reason, "<a href="); got != 2 {
		t.Errorf("reason has %d links, want 2 (one per red check with a DetailsURL):\n%s", got, reason)
	}
	if !strings.Contains(reason, "Lint") {
		t.Errorf("Lint is not named:\n%s", reason)
	}
	if strings.Contains(reason, `<a href=""`) {
		t.Errorf("Lint has no DetailsURL and should not render an empty anchor:\n%s", reason)
	}
}

// TestPageRendersWaitingOnProducerDeployWhenOnlyTheCompatCheckIsRed covers inv. 12 wired end to
// end through the repo's configured compat_check: a red compat check with every other required
// check green renders the row as waiting_on_producer_deploy, not needs_you.
func TestPageRendersWaitingOnProducerDeployWhenOnlyTheCompatCheckIsRed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://CC-1", at)
	const tip = "cc-1-tip"
	if err := store.RecordPush(ctx, "sandbox://CC-1", tip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees:  map[string]string{cc.BranchKey("repo", "cc-1"): "/repos/cc-1"},
		BranchTips: map[string]string{cc.MainTipKey("repo"): "main-tip"},
		PRs: map[string]gh.PR{
			cc.BranchKey("repo", "cc-1"): {
				Number: 1, State: gh.Open, HeadOid: tip,
				Checks: map[string]gh.CheckState{
					"GraphQL production compatibility": {Status: "COMPLETED", Conclusion: "FAILURE"},
					"Tests":                            {Status: "COMPLETED", Conclusion: "SUCCESS"},
				},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{
		Name: "repo", CompatCheck: "GraphQL production compatibility",
		Checks: verdict.Predicate{AllOf: []verdict.Predicate{
			{Success: "GraphQL production compatibility"}, {Success: "Tests"},
		}},
	}}
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if state := rowState(t, page, "sandbox://CC-1"); state != "waiting_on_producer_deploy" {
		t.Fatalf("state = %q, want waiting_on_producer_deploy (only the compat check is red)", state)
	}
	if !strings.Contains(page, `value="re-check"`) {
		t.Error("page has no re-check button for the waiting_on_producer_deploy row")
	}
	if !strings.Contains(page, `value="re-run"`) {
		t.Error("page has no re-run button for the waiting_on_producer_deploy row")
	}
}

func TestServerRejectsUnknownPaths(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLaunchRejectsBadOriginAndMethod(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
	}{
		{
			name:   "GET is rejected before origin is even checked",
			method: http.MethodGet, origin: srv.URL, wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "a missing Origin is rejected",
			method: http.MethodPost, origin: "", wantStatus: http.StatusForbidden,
		},
		{
			name:   "a foreign Origin is rejected",
			method: http.MethodPost, origin: "http://evil.example", wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+"/launch?ticket=sandbox://CC-1", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestLaunchAcceptsASameOriginPost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?ticket=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)
}

// TestGetPreviewIsGone covers phase 5 of plans/feature-launch.md: the per-ticket preview page is
// deleted along with handlePreview, so the route itself is unregistered rather than refusing a
// bad request.
func TestGetPreviewIsGone(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?ticket=sandbox://CC-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestCandidatesAndLaunchHandleAnArbitrarilySizedSlice proves issue #33's "no size limit anywhere
// in the path": a fan-out is one root plus as many dependents as the plan calls for, and neither
// /launch/candidates nor /launch may special-case a small slice.
func TestCandidatesAndLaunchHandleAnArbitrarilySizedSlice(t *testing.T) {
	t.Parallel()

	const fanOut = 50
	ctx := t.Context()
	store := openStore(t)

	root := cc.Ticket{URL: "sandbox://CC-0", Repo: "cc-sandbox", Branch: "cc-0"}
	tickets := []cc.Ticket{root}
	query := "ticket=" + root.URL
	for i := 1; i <= fanOut; i++ {
		ticketURL := fmt.Sprintf("sandbox://CC-%d", i)
		tickets = append(tickets, cc.Ticket{
			URL: ticketURL, Repo: "cc-sandbox", Branch: fmt.Sprintf("cc-%d", i),
			BlockedBy: []string{root.URL},
		})
		query += "&ticket=" + ticketURL
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, query)
	if got := len(candidates); got != fanOut+1 {
		t.Fatalf("candidates = %d, want %d: the slice must not be truncated", got, fanOut+1)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	launchResp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = launchResp.Body.Close() }()
	assertSeeOtherHome(t, launchResp)

	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberships) != fanOut+1 {
		t.Fatalf("authorised memberships = %d, want %d: the slice must not be truncated", len(memberships), fanOut+1)
	}
}

// runningRowStore seeds one ticket with a live agent run, the state both a pgid/elapsed row and a
// queued-verb row are read against.
func runningRowStore(t *testing.T, ticket cc.Ticket, startedAt, now time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{ObservedAt: now, Runs: map[string]cc.RunObservation{ticket.URL: {Alive: true}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestServerRendersARunningRowWithPgidAndElapsed(t *testing.T) {
	t.Parallel()

	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)

	server := cc.NewServer(store, fixedClock(now), nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	wants := []string{"running", "1m30s",
		`<button type="submit" name="verb" value="kill">kill</button>`}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}

// TestLaunchStoresTheComposedHash covers issue #52's AC1 at the authorisation route:
// launch_members.prompt_hash stores plan.Hash of the composed prompt.
func TestLaunchStoresTheComposedHash(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?ticket=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)

	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := plan.Hash(plan.Compose(plan.Ticket{URL: ticket.URL}))
	if got := hashes[ticket.URL]; got != want {
		t.Errorf("stored prompt_hash = %q, want %q", got, want)
	}
}

func TestPageLinksTheBuiltStylesheet(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, observedAt), fixedClock(observedAt), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if want := `<link rel="stylesheet" href="/assets/app.css">`; !strings.Contains(body, want) {
		t.Errorf("page is missing %q", want)
	}
	if strings.Contains(body, "<style>") {
		t.Error("page still inlines a stylesheet")
	}

	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.css = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
	for _, want := range []string{"--color-s-live", ".pill", "data-theme=dark"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("built stylesheet is missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /page.css = %d, want 404", rec.Code)
	}
}

// TestPageShowsQueuedVerbsBesideTheState covers issue #71.
func TestPageShowsQueuedVerbsBesideTheState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)
	server := cc.NewServer(store, fixedClock(now), nil, "")

	if err := store.QueueVerbIntent(ctx, ticket.URL, "kill", now); err != nil {
		t.Fatal(err)
	}
	page := renderPage(t, server)
	if got := rowState(t, page, ticket.URL); got != "running · kill queued" {
		t.Errorf("state = %q, want %q", got, "running · kill queued")
	}
	const killButton = `<button type="submit" name="verb" value="kill">kill</button>`
	if !strings.Contains(page, killButton) {
		t.Errorf("kill button missing while kill is queued; a queued intent is not a promise:\n%s", page)
	}

	if err := store.QueueVerbIntent(ctx, ticket.URL, "close-pr", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := rowState(t, renderPage(t, server), ticket.URL); got != "running · kill queued · close-pr queued" {
		t.Errorf("state = %q, want both queued verbs", got)
	}

	pending, err := store.PendingVerbIntents(ctx, "kill")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeVerbIntent(ctx, pending[0].ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	page = renderPage(t, server)
	if got := rowState(t, page, ticket.URL); got != "running · close-pr queued" {
		t.Errorf("state = %q, want the consumed kill gone", got)
	}
	if !strings.Contains(page, killButton) {
		t.Errorf("kill button missing after the tick consumed the intent:\n%s", page)
	}
}

// TestPageShowsAQueuedLaunchBeforeTheTickAuthorisesIt covers issue #71's launch window.
func TestPageShowsAQueuedLaunchBeforeTheTickAuthorisesIt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, now)
	if err := store.QueueLaunchIntent(t.Context(), "sandbox://CC-1", "hash-1", "group-a", now); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, fixedClock(now), nil, "")
	if got := rowState(t, renderPage(t, server), "sandbox://CC-1"); got != "ready · launch queued" {
		t.Errorf("state = %q, want %q", got, "ready · launch queued")
	}

	if err := store.ApplyLaunchIntents(t.Context(), now.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := rowState(t, renderPage(t, server), "sandbox://CC-1"); got != "queued" {
		t.Errorf("state = %q, want a bare %q once the tick consumed the intent", got, "queued")
	}
}

// TestLaunchRefusesASubmittedHashThatNoLongerComposes covers issue #73's AC2: a submitted hash
// that is not what the ticket composes to now is caught at /launch, and the whole slice is refused
// — including the ticket whose hash still matched.
func TestLaunchRefusesASubmittedHashThatNoLongerComposes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	previewed := plan.Hash("a prompt sandbox://CC-2 no longer composes to")
	recomposed := plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-2"}))

	form := url.Values{
		"ticket": {"sandbox://CC-1", "sandbox://CC-2"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-1"})),
			"sandbox://CC-2 " + previewed,
		},
	}
	resp, body := postLaunchForm(t, srv, form)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"sandbox://CC-2", previewed, recomposed} {
		if !strings.Contains(body, want) {
			t.Errorf("409 body does not name %q:\n%s", want, body)
		}
	}

	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 0 {
		t.Errorf("active launch hashes = %v, want nothing queued", hashes)
	}
}

// TestLaunchIgnoresTheHashOfAnUncheckedRow covers what a browser actually posts when the operator
// unchecks a row: the hidden hash still travels, its checkbox does not, and the tickets that are
// checked still launch on their own hashes.
func TestLaunchIgnoresTheHashOfAnUncheckedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	form := url.Values{
		"ticket": {"sandbox://CC-1"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-1"})),
			"sandbox://CC-2 " + plan.Hash(plan.Compose(plan.Ticket{URL: "sandbox://CC-2"})),
		},
	}
	resp, _ := postLaunchForm(t, srv, form)
	assertSeeOtherHome(t, resp)

	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hashes["sandbox://CC-1"]; !ok || len(hashes) != 1 {
		t.Errorf("active launch hashes = %v, want only sandbox://CC-1", hashes)
	}
}

// TestLaunchRejectsAMalformedHashField covers a hash field that names no ticket: it is refused
// rather than read as a hash that matches nothing.
func TestLaunchRejectsAMalformedHashField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	form := url.Values{"ticket": {"sandbox://CC-1"}, "hash": {"deadbeef"}}
	resp, body := postLaunchForm(t, srv, form)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func postLaunchForm(t *testing.T, srv *httptest.Server, form url.Values) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}
