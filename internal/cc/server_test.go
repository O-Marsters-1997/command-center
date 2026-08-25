package cc_test

import (
	"context"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

var update = flag.Bool("update", false, "regenerate golden files")

const goldenPage = "testdata/page.golden.html"
const goldenPreview = "testdata/preview.golden.html"

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

func fetchPreview(t *testing.T, srv *httptest.Server, query string) string {
	t.Helper()

	resp, err := http.Get(srv.URL + "/preview?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	return string(body)
}

// previewRowFor returns the one <tr> block naming ticketURL, so a test asserts on a single row's
// cells without pulling in an HTML parser.
func previewRowFor(t *testing.T, body, ticketURL string) string {
	t.Helper()

	for _, tr := range strings.Split(body, "<tr>") {
		if strings.Contains(tr, "<td>"+ticketURL+"</td>") {
			return tr
		}
	}
	t.Fatalf("no preview row for %s in:\n%s", ticketURL, body)
	return ""
}

func assertCells(t *testing.T, row string, cells ...string) {
	t.Helper()

	for _, cell := range cells {
		if !strings.Contains(row, cell) {
			t.Errorf("row does not contain %q:\n%s", cell, row)
		}
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
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		ObservedAt: observedAt,
		PRs:        map[string]gh.PR{},
		Worktrees:  map[string]string{"cc-1-first": "/repos/cc-sandbox-cc-1-first"},
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

func TestServerRendersThePage(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := observedAt.Add(45 * time.Second)
	store := seededStore(t, observedAt)
	if err := store.QueueLaunchIntent(t.Context(), "sandbox://CC-1", "hash-1", "group-a", observedAt); err != nil {
		t.Fatal(err)
	}
	server := cc.NewServer(store, fixedClock(now), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	assertGolden(t, goldenPage, rec.Body.Bytes())
}

// TestPageRendersTheParentsVerdictOnAStackedRow covers the last of issue #32's "what to build":
// a red check on a descendant whose base moved may not be its own fault, so the row also renders
// the base's own CI verdict alongside its own.
func TestPageRendersTheParentsVerdictOnAStackedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
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
		Worktrees:  map[string]string{"parent": "/repos/parent", "child": "/repos/child"},
		BranchTips: map[string]string{"parent": parentTip, "repo//main": "main-tip"},
		PRs: map[string]gh.PR{
			"parent": {
				Number: 1, State: gh.Open, HeadOid: parentTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
			"child": {
				Number: 2, State: gh.Open, HeadOid: childTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}
	server := cc.NewServer(store, fixedClock(at), repos, nil, "")
	page := renderPage(t, server)

	if state := rowState(t, page, "sandbox://PARENT"); state != "needs_you" {
		t.Fatalf("parent's own state = %q, want needs_you (its CI check failed)", state)
	}

	if got := rowCellAt(t, page, "sandbox://CHILD", 7); got != "needs_you" {
		t.Errorf("child's rendered base verdict = %q, want needs_you (the parent's own verdict)", got)
	}
}

// TestPageRendersWaitingOnProducerDeployWhenOnlyTheCompatCheckIsRed covers inv. 12 wired end to
// end through the repo's configured compat_check: a red compat check with every other required
// check green renders the row as waiting_on_producer_deploy, not needs_you.
func TestPageRendersWaitingOnProducerDeployWhenOnlyTheCompatCheckIsRed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://CC-1", at)
	const tip = "cc-1-tip"
	if err := store.RecordPush(ctx, "sandbox://CC-1", tip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees:  map[string]string{"cc-1": "/repos/cc-1"},
		BranchTips: map[string]string{"repo//main": "main-tip"},
		PRs: map[string]gh.PR{
			"cc-1": {
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
	server := cc.NewServer(store, fixedClock(at), repos, nil, "")
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

	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLaunchRejectsBadOriginAndMethod(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, ""))
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
			req, err := http.NewRequest(tt.method, srv.URL+"/launch?task=sandbox://CC-1", nil)
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

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?task=sandbox://CC-1", nil)
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

func TestPreviewRendersNowOnUnlockAndRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"}},
		{TicketURL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	// CC-4 is a tracked task but deliberately left out of the slice, so CC-3's blocker sits
	// outside it with no pull request.
	body := fetchPreview(t, srv, "task=sandbox://CC-1&task=sandbox://CC-2&task=sandbox://CC-3")

	assertCells(t, previewRowFor(t, body, "sandbox://CC-1"), "<td>now</td>", "<td>origin/main</td>")
	assertCells(t, previewRowFor(t, body, "sandbox://CC-2"), "<td>on unlock</td>", "<td>origin/main</td>")

	refused := previewRowFor(t, body, "sandbox://CC-3")
	assertCells(t, refused, "<td>refused</td>", "sandbox://CC-4")
	if strings.Contains(refused, `<input type="checkbox"`) {
		t.Errorf("refused row carries a checkbox and would be submitted:\n%s", refused)
	}
}

// TestPreviewShowsTheBasesVerdictForAStackedRow covers issue #34's "you are about to build on a
// red parent": the preview is read before authorising, so a row whose base is a blocker's own
// branch carries that blocker's own CI verdict, not just its base branch name.
func TestPreviewShowsTheBasesVerdictForAStackedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	const parentTip = "parent-tip"
	if err := store.RecordPush(ctx, "sandbox://PARENT", parentTip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		BranchTips: map[string]string{"parent": parentTip, "repo//main": "main-tip"},
		PRs: map[string]gh.PR{
			"parent": {
				Number: 1, State: gh.Open, HeadOid: parentTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}
	srv := httptest.NewServer(cc.NewServer(store, fixedClock(at), repos, nil, ""))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CHILD")

	// now because the parent's PR is open, origin/parent because stacking is on, and needs_you
	// because that parent's own CI is red.
	assertCells(t, previewRowFor(t, body, "sandbox://CHILD"),
		"<td>now</td>", "<td>origin/parent</td>", "<td>needs_you</td>")
}

func TestPreviewRefusesATaskAlreadyInAnActiveLaunch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, task.TicketURL, "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CC-1")

	assertCells(t, previewRowFor(t, body, "sandbox://CC-1"), "<td>refused</td>", "already authorised in launch 1")
}

func TestPreviewRejectsEmptyOrUnknownTask(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		path string
	}{
		{name: "no task at all", path: "/preview"},
		{name: "an empty task value", path: "/preview?task="},
		{name: "an unknown ticket", path: "/preview?task=sandbox://GHOST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestPreviewAndLaunchHandleAnArbitrarilySizedSlice proves issue #33's "no size limit anywhere
// in the path": a fan-out is one root plus as many dependents as the plan calls for, and neither
// /preview nor /launch may special-case a small slice.
func TestPreviewAndLaunchHandleAnArbitrarilySizedSlice(t *testing.T) {
	t.Parallel()

	const fanOut = 50
	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	root := cc.Task{TicketURL: "sandbox://CC-0", Repo: "cc-sandbox", Branch: "cc-0"}
	tasks := []cc.Task{root}
	query := "task=" + root.TicketURL
	for i := 1; i <= fanOut; i++ {
		ticketURL := fmt.Sprintf("sandbox://CC-%d", i)
		tasks = append(tasks, cc.Task{
			TicketURL: ticketURL, Repo: "cc-sandbox", Branch: fmt.Sprintf("cc-%d", i),
			BlockedBy: []string{root.TicketURL},
		})
		query += "&task=" + ticketURL
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, query)
	if got := strings.Count(body, `<input type="checkbox"`); got != fanOut+1 {
		t.Fatalf("preview checkboxes = %d, want %d: the slice must not be truncated", got, fanOut+1)
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

// runningRowStore seeds one task with a live agent run, the state both a pgid/elapsed row and a
// queued-verb row are read against.
func runningRowStore(t *testing.T, task cc.Task, startedAt, now time.Time) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, task.TicketURL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{ObservedAt: now, Runs: map[string]cc.RunObservation{task.TicketURL: {Alive: true}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestServerRendersARunningRowWithPgidAndElapsed(t *testing.T) {
	t.Parallel()

	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, task, startedAt, now)

	server := cc.NewServer(store, fixedClock(now), nil, nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	wants := []string{"running", "4242", "1m30s", "/state/runs/1.jsonl",
		`<button type="submit" name="verb" value="kill">kill</button>`}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}

// TestPreviewComposesSeamsIntoThePromptAndHash covers issue #52's AC1: a task with two seams
// composes to the implement instruction plus both files' contents, in config order, and the
// preview's hash is plan.Hash of that same composed prompt.
func TestPreviewComposesSeamsIntoThePromptAndHash(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam one content")
	writeSeamFile(t, root, "two", "seam two content")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"one", "two"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, root))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CC-1")

	wantPrompt := plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam one content", "seam two content"})
	assertCells(t, previewRowFor(t, body, "sandbox://CC-1"),
		"<details><summary>prompt "+plan.Hash(wantPrompt)+"</summary><pre>"+html.EscapeString(wantPrompt)+"</pre></details>")
}

// TestPreviewRefusesATaskNamingAMissingSeam covers issue #52's AC2: a task naming a seam with no
// file is refused, naming that seam, rather than composing around it with an empty paste.
func TestPreviewRefusesATaskNamingAMissingSeam(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"ghost"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, t.TempDir()))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CC-1")

	row := previewRowFor(t, body, "sandbox://CC-1")
	assertCells(t, row, "<td>refused</td>", "ghost")
	if strings.Contains(row, "<details>") {
		t.Errorf("refused row renders a prompt it never composed:\n%s", row)
	}
}

// TestPreviewKeepsAnExistingRefusalReasonOverAMissingSeam covers the case where a row is refused
// for two independent reasons at once: a task already authorised in an active launch that also
// names a missing seam must keep naming the active launch, not have that reason overwritten by
// the seam refusal.
func TestPreviewKeepsAnExistingRefusalReasonOverAMissingSeam(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"ghost"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, task.TicketURL, "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, fixedClock(at), nil, nil, t.TempDir()))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CC-1")

	assertCells(t, previewRowFor(t, body, "sandbox://CC-1"), "<td>refused</td>", "already authorised in launch 1")
}

// TestLaunchRefusesATaskNamingAMissingSeam covers issue #52's AC2 at the authorisation route: a
// refused task is never queued to launch.
func TestLaunchRefusesATaskNamingAMissingSeam(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"ghost"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, t.TempDir()))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?task=sandbox://CC-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	if err := store.ApplyLaunchIntents(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, queued := hashes[task.TicketURL]; queued {
		t.Error("a task naming a missing seam must never be queued to launch")
	}
}

// TestLaunchStoresTheComposedHashForATaskWithSeams covers issue #52's AC1 at the authorisation
// route: launch_members.prompt_hash stores plan.Hash of the seam-composed prompt.
func TestLaunchStoresTheComposedHashForATaskWithSeams(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam one content")
	writeSeamFile(t, root, "two", "seam two content")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"one", "two"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, root))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?task=sandbox://CC-1", nil)
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
	want := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam one content", "seam two content"}))
	if got := hashes[task.TicketURL]; got != want {
		t.Errorf("stored prompt_hash = %q, want %q", got, want)
	}
}

// TestPageFlagsSeamChangedOnAQueuedRow covers issue #55's AC1 at the page: a member sits queued
// on its authorised hash until a seam file it names is edited, at which point the row stays
// queued but now also carries the seam-changed flag.
func TestPageFlagsSeamChangedOnAQueuedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam content, version A")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"one"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam content, version A"}))
	if err := store.QueueLaunchIntent(ctx, task.TicketURL, hash, "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, fixedClock(at), nil, nil, root)
	page := renderPage(t, server)
	if state := rowState(t, page, task.TicketURL); state != "queued" {
		t.Fatalf("state before edit = %q, want queued", state)
	}
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "" {
		t.Errorf("seam-changed cell before edit = %q, want empty", got)
	}

	writeSeamFile(t, root, "one", "seam content, version B")

	page = renderPage(t, server)
	if state := rowState(t, page, task.TicketURL); state != "queued" {
		t.Errorf("state after edit = %q, want still queued", state)
	}
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "seam changed" {
		t.Errorf("seam-changed cell after edit = %q, want %q", got, "seam changed")
	}

	latest, err := store.LatestRunsByTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ran := latest[task.TicketURL]; ran {
		t.Error("no run should exist: this test never ticks the loop")
	}
}

// TestPageClearsSeamChangedAfterReauthorisation covers issue #55's AC2: cancelling a launch
// whose seam changed and re-authorising against the current composition clears the flag, since
// the new member's hash is taken from what the seam files say now.
func TestPageClearsSeamChangedAfterReauthorisation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam content, version A")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"one"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	staleHash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam content, version A"}))
	if err := store.QueueLaunchIntent(ctx, task.TicketURL, staleHash, "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatal(err)
	}
	writeSeamFile(t, root, "one", "seam content, version B")

	server := cc.NewServer(store, fixedClock(at), nil, nil, root)
	page := renderPage(t, server)
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "seam changed" {
		t.Fatalf("seam-changed cell before re-authorising = %q, want %q", got, "seam changed")
	}

	if _, err := store.CancelLaunchesFor(ctx, task.TicketURL); err != nil {
		t.Fatal(err)
	}
	freshHash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam content, version B"}))
	if err := store.QueueLaunchIntent(ctx, task.TicketURL, freshHash, "group-b", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	page = renderPage(t, server)
	if state := rowState(t, page, task.TicketURL); state != "queued" {
		t.Errorf("state after re-authorising = %q, want queued", state)
	}
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "" {
		t.Errorf("seam-changed cell after re-authorising = %q, want empty", got)
	}
}

// TestPageComposesSeamChangedWithReviewMe covers issue #55's AC3: a row that has already run and
// reads review_me still flags seam changed once its seam is edited afterwards -- the flag
// composes with the derived state rather than plan.Status growing a case for it.
func TestPageComposesSeamChangedWithReviewMe(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam content, version A")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1", Seams: []string{"one"}}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runHash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam content, version A"}))
	runID, err := store.InsertRunSkeleton(ctx, task.TicketURL, "agent", "", runHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := store.RecordDisposition(ctx, runID, plan.OutcomePush, &exitCode, at); err != nil {
		t.Fatal(err)
	}
	const pushedTip = "cc-1-tip"
	if err := store.RecordPush(ctx, task.TicketURL, pushedTip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		PRs: map[string]gh.PR{
			"cc-1": {
				Number: 1, State: gh.Open, HeadOid: pushedTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			},
		},
		BranchTips: map[string]string{"repo//main": "main-tip"},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Checks: verdict.Predicate{Success: "CI"}}}
	server := cc.NewServer(store, fixedClock(at), repos, nil, root)
	page := renderPage(t, server)
	if state := rowState(t, page, task.TicketURL); state != "review_me" {
		t.Fatalf("state before edit = %q, want review_me", state)
	}
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "" {
		t.Errorf("seam-changed cell before edit = %q, want empty", got)
	}

	writeSeamFile(t, root, "one", "seam content, version B")

	page = renderPage(t, server)
	if state := rowState(t, page, task.TicketURL); state != "review_me" {
		t.Errorf("state after edit = %q, want still review_me", state)
	}
	if got := rowCellAt(t, page, task.TicketURL, 3); got != "seam changed" {
		t.Errorf("seam-changed cell after edit = %q, want %q", got, "seam changed")
	}
}

func TestPageInlinesTheStylesheetAndRefreshesItself(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(seededStore(t, observedAt), fixedClock(observedAt), nil, nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	for _, want := range []string{
		`<meta http-equiv="refresh" content="5">`,
		"<style>",
		"border-collapse",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /page.css = %d, want 404: the stylesheet is inlined, not served", rec.Code)
	}
}

// TestPageShowsQueuedVerbsBesideTheState covers issue #71.
func TestPageShowsQueuedVerbsBesideTheState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, task, startedAt, now)
	server := cc.NewServer(store, fixedClock(now), nil, nil, "")

	if err := store.QueueVerbIntent(ctx, task.TicketURL, "kill", now); err != nil {
		t.Fatal(err)
	}
	page := renderPage(t, server)
	if got := rowState(t, page, task.TicketURL); got != "running · kill queued" {
		t.Errorf("state = %q, want %q", got, "running · kill queued")
	}
	const killButton = `<button type="submit" name="verb" value="kill">kill</button>`
	if !strings.Contains(page, killButton) {
		t.Errorf("kill button missing while kill is queued; a queued intent is not a promise:\n%s", page)
	}

	if err := store.QueueVerbIntent(ctx, task.TicketURL, "close-pr", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := rowState(t, renderPage(t, server), task.TicketURL); got != "running · kill queued · close-pr queued" {
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
	if got := rowState(t, page, task.TicketURL); got != "running · close-pr queued" {
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

	server := cc.NewServer(store, fixedClock(now), nil, nil, "")
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

// TestPreviewRendersItsPage goldens the whole preview render: a now row, an on-unlock row and a
// refused row rendering with no checkbox.
func TestPreviewRendersItsPage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam one content")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Seams: []string{"one"}},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"}},
		{TicketURL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(store, fixedClock(at), nil, nil, root)
	rec := httptest.NewRecorder()
	target := "/preview?task=sandbox://CC-1&task=sandbox://CC-2&task=sandbox://CC-3"
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	assertGolden(t, goldenPreview, rec.Body.Bytes())
}

// TestPreviewRefusesEveryDependentOfAMidStackBlockerOutsideTheSlice covers issue #72's slice of
// five: CC-2 is left out of a slice sitting on top of it, so both of its direct dependents are
// refused and the three rows above them still read on unlock.
func TestPreviewRefusesEveryDependentOfAMidStackBlockerOutsideTheSlice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-1"}},
		{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", BlockedBy: []string{"sandbox://CC-2"}},
		{TicketURL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4", BlockedBy: []string{"sandbox://CC-2"}},
		{TicketURL: "sandbox://CC-5", Repo: "cc-sandbox", Branch: "cc-5", BlockedBy: []string{"sandbox://CC-3"}},
		{TicketURL: "sandbox://CC-6", Repo: "cc-sandbox", Branch: "cc-6", BlockedBy: []string{"sandbox://CC-4"}},
		{TicketURL: "sandbox://CC-7", Repo: "cc-sandbox", Branch: "cc-7", BlockedBy: []string{"sandbox://CC-5"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv,
		"task=sandbox://CC-3&task=sandbox://CC-4&task=sandbox://CC-5&task=sandbox://CC-6&task=sandbox://CC-7")

	for _, ticketURL := range []string{"sandbox://CC-3", "sandbox://CC-4"} {
		assertCells(t, previewRowFor(t, body, ticketURL), "<td>refused</td>", "sandbox://CC-2")
	}
	for _, ticketURL := range []string{"sandbox://CC-5", "sandbox://CC-6", "sandbox://CC-7"} {
		assertCells(t, previewRowFor(t, body, ticketURL), "<td>on unlock</td>")
	}
	if got := strings.Count(body, "<td>on unlock</td>"); got != 3 {
		t.Errorf("on unlock rows = %d, want 3", got)
	}
	if got := strings.Count(body, "<td>refused</td>"); got != 2 {
		t.Errorf("refused rows = %d, want 2", got)
	}
}

// TestPreviewCarriesTheHashOnEveryLaunchableRow covers issue #73's AC1: the authorise form posts
// the hash the operator read alongside the task, and a refused row posts neither.
func TestPreviewCarriesTheHashOnEveryLaunchableRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam one content")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Seams: []string{"one"}},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-3"}},
		{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, root))
	t.Cleanup(srv.Close)

	body := fetchPreview(t, srv, "task=sandbox://CC-1&task=sandbox://CC-2")

	want := plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-1"}, []string{"seam one content"}))
	assertCells(t, previewRowFor(t, body, "sandbox://CC-1"),
		`<input type="hidden" name="hash" value="sandbox://CC-1 `+want+`">`)
	if refused := previewRowFor(t, body, "sandbox://CC-2"); strings.Contains(refused, `name="hash"`) {
		t.Errorf("refused row carries a hash and would be submitted:\n%s", refused)
	}
}

// TestLaunchRefusesASubmittedHashThatNoLongerComposes covers issue #73's AC2: a seam edited
// between reading the preview and pressing authorise is caught at /launch, and the whole slice is
// refused — including the task whose hash still matched.
func TestLaunchRefusesASubmittedHashThatNoLongerComposes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	writeSeamFile(t, root, "one", "seam one content")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", Seams: []string{"one"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, root))
	t.Cleanup(srv.Close)

	previewed := plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-2"}, []string{"seam one content"}))
	writeSeamFile(t, root, "one", "seam one edited")
	recomposed := plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-2"}, []string{"seam one edited"}))

	form := url.Values{
		"task": {"sandbox://CC-1", "sandbox://CC-2"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-1"}, nil)),
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
// unchecks a row: the hidden hash still travels, its checkbox does not, and the tasks that are
// checked still launch on their own hashes.
func TestLaunchIgnoresTheHashOfAnUncheckedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := seededStore(t, time.Now())
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	form := url.Values{
		"task": {"sandbox://CC-1"},
		"hash": {
			"sandbox://CC-1 " + plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-1"}, nil)),
			"sandbox://CC-2 " + plan.Hash(plan.Compose(plan.Task{TicketURL: "sandbox://CC-2"}, nil)),
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

// TestLaunchRejectsAMalformedHashField covers a hash field that names no task: it is refused
// rather than read as a hash that matches nothing.
func TestLaunchRejectsAMalformedHashField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, nil, ""))
	t.Cleanup(srv.Close)

	form := url.Values{"task": {"sandbox://CC-1"}, "hash": {"deadbeef"}}
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
