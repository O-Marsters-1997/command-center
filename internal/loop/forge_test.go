package loop_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestALoopDrivesATicketFromReadyToReviewMeWithForgeAndWorktreeFakes(t *testing.T) {
	t.Parallel()
	root, _ := repoWithOrigin(t)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	pushSettingsFile(t, config.CheckoutPath(root, "repo"), "[checks]\nsuccess = \"CI\"\n")

	issue := tracker.Ticket{URL: "https://github.com/acme/repo/issues/1", Number: 1, Title: "Add x"}
	source := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets:  map[string][]tracker.Ticket{"project:x": {issue}},
	}
	resolve := func(tracker.Kind, string) (tracker.Source, error) { return source, nil }

	store := openStore(t)
	trackRepo(t, store, "repo", "git@github.com:acme/repo.git")
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	forge := &recordingForge{}
	worktrees := &recordingWorktrees{}
	runner := runner.NewFake()
	lp := newLoop(
		store, loop.NewObserver(store, forge, cfg), fixedClock(at), cfg, ws, runner,
		loop.WithForge(forge), loop.WithWorktrees(worktrees), loop.WithTrackerSource(resolve),
	)

	if err := store.QueueVerbIntent(t.Context(), "project:x", plan.VerbImport, at); err != nil {
		t.Fatal(err)
	}
	authoriseTicket(t, store, issue.URL, composeHash(issue.URL), at)

	tick := func() {
		t.Helper()
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
	}
	state := func() string {
		t.Helper()
		return rowState(t, renderPage(t, openServer(store, fixedClock(at), "")), issue.URL)
	}

	tick()
	if len(runner.Spawns) != 1 {
		t.Fatalf("spawns = %d, want the imported ticket's implement run", len(runner.Spawns))
	}
	gittest.Commit(t, runner.Spawns[0].WorktreePath, "x.go", "package x\n")

	runner.Alive[1] = false
	runner.CanReap[1] = true
	for range 4 {
		tick()
	}
	if len(forge.Calls("Create")) != 1 {
		t.Fatalf("Create calls = %+v, want one", forge.Calls("Create"))
	}
	if len(worktrees.Cut) != 1 {
		t.Fatalf("cut worktrees = %v, want one", worktrees.Cut)
	}
	branch := worktrees.Cut[0]
	forge.Snapshot = gh.Snapshot{ByBranch: map[string]gh.PR{
		branch: {
			Number: 1, HeadRef: branch, State: gh.Open,
			HeadOid: gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/"+branch),
			Checks:  map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
		},
	}}

	if len(runner.Spawns) != 1 {
		t.Fatalf("spawns = %d, want only the implement run", len(runner.Spawns))
	}
	for range 2 {
		tick()
	}

	if got := state(); got != "review_me" {
		t.Errorf("row state = %q, want review_me", got)
	}

	obs, _, err := store.LastObservation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.LocalTips) == 0 {
		t.Error("the observation holds no local tips: observe must read them for the push step")
	}
}
