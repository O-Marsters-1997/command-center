package cc_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	server := cc.NewServer(seededStore(t, observedAt), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	got := rec.Body.Bytes()
	if *update {
		if err := os.WriteFile(goldenPage, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPage)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("page differs from %s; rerun with -update to accept\n--- got ---\n%s", goldenPage, got)
	}
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
		BranchTips: map[string]string{"parent": parentTip},
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
	server := cc.NewServer(store, fixedClock(at), repos, "")
	page := renderPage(t, server)

	if state := rowState(t, page, "sandbox://PARENT"); state != "needs_you" {
		t.Fatalf("parent's own state = %q, want needs_you (its CI check failed)", state)
	}

	if got := rowCellAt(t, page, "sandbox://CHILD", 6); got != "needs_you" {
		t.Errorf("child's rendered base verdict = %q, want needs_you (the parent's own verdict)", got)
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

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
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
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202: %s", resp.StatusCode, body)
	}
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	// CC-4 is a tracked task but deliberately left out of the slice, so CC-3's blocker sits
	// outside it with no pull request.
	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CC-1&task=sandbox://CC-2&task=sandbox://CC-3")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode preview response %s: %v", body, err)
	}
	byTicket := make(map[string]map[string]any, len(rows))
	for _, r := range rows {
		byTicket[r["TicketURL"].(string)] = r
	}

	if got := byTicket["sandbox://CC-1"]["Label"]; got != "now" {
		t.Errorf("CC-1 label = %v, want now", got)
	}
	if got := byTicket["sandbox://CC-1"]["Base"]; got != "origin/main" {
		t.Errorf("CC-1 base = %v, want origin/main", got)
	}
	if got := byTicket["sandbox://CC-2"]["Label"]; got != "on unlock" {
		t.Errorf("CC-2 label = %v, want on unlock", got)
	}
	if got := byTicket["sandbox://CC-2"]["Base"]; got != "origin/main" {
		t.Errorf("CC-2 base = %v, want origin/main", got)
	}
	if got := byTicket["sandbox://CC-3"]["Label"]; got != "refused" {
		t.Errorf("CC-3 label = %v, want refused", got)
	}
	if reason, _ := byTicket["sandbox://CC-3"]["Reason"].(string); !strings.Contains(reason, "sandbox://CC-4") {
		t.Errorf("CC-3 reason = %q, does not name its blocker sandbox://CC-4", reason)
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
		BranchTips: map[string]string{"parent": parentTip},
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
	srv := httptest.NewServer(cc.NewServer(store, fixedClock(at), repos, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CHILD")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode preview response %s: %v", body, err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	if got := rows[0]["Label"]; got != "now" {
		t.Errorf("label = %v, want now (parent's PR is open)", got)
	}
	if got := rows[0]["Base"]; got != "origin/parent" {
		t.Errorf("base = %v, want origin/parent", got)
	}
	if got := rows[0]["BaseVerdict"]; got != "needs_you" {
		t.Errorf("base verdict = %v, want needs_you (the parent's own red CI)", got)
	}
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CC-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode preview response %s: %v", body, err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	if rows[0]["Label"] != "refused" {
		t.Errorf("label = %v, want refused", rows[0]["Label"])
	}
	reason, _ := rows[0]["Reason"].(string)
	if !strings.Contains(reason, "already authorised in launch") {
		t.Errorf("reason = %q, want it to name the active launch", reason)
	}
}

func TestPreviewRejectsEmptyOrUnknownTask(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != fanOut+1 {
		t.Fatalf("preview rows = %d, want %d", len(rows), fanOut+1)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/launch?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	launchResp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = launchResp.Body.Close() }()
	if launchResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(launchResp.Body)
		t.Fatalf("launch status = %d, want 202: %s", launchResp.StatusCode, body)
	}

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

func TestServerRendersARunningRowWithPgidAndElapsed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(ctx, task.TicketURL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}

	now := startedAt.Add(90 * time.Second)
	obs := cc.Observation{
		ObservedAt: now,
		Runs:       map[string]cc.RunObservation{task.TicketURL: {Alive: true}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, fixedClock(now), nil, "")
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, root))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CC-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}

	wantPrompt := plan.Compose(plan.Task{TicketURL: task.TicketURL}, []string{"seam one content", "seam two content"})
	if got := rows[0]["Prompt"]; got != wantPrompt {
		t.Errorf("Prompt = %q, want %q", got, wantPrompt)
	}
	if got := rows[0]["Hash"]; got != plan.Hash(wantPrompt) {
		t.Errorf("Hash = %v, want %v", got, plan.Hash(wantPrompt))
	}
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, t.TempDir()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview?task=sandbox://CC-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	if got := rows[0]["Label"]; got != "refused" {
		t.Errorf("label = %v, want refused", got)
	}
	if reason, _ := rows[0]["Reason"].(string); !strings.Contains(reason, "ghost") {
		t.Errorf("reason = %q, does not name the missing seam %q", reason, "ghost")
	}
	if got, _ := rows[0]["Prompt"].(string); got != "" {
		t.Errorf("Prompt = %q, want empty for a refused row", got)
	}
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, t.TempDir()))
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

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, root))
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
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

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
