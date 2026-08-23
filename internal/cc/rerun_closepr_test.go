package cc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// TestReRunSpawnsASecondRunInTheSameWorktreeWithoutCutting covers re-run's own contract
// (docs/prds/prd-command-centre.md § Phase 6): relaunch in the same worktree, incrementally -- a
// second `runs` row against the same task, with no `tp new` call at all (the fake runner here
// never touches tp; nothing about re-run does).
func TestReRunSpawnsASecondRunInTheSameWorktreeWithoutCutting(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	firstRunID, err := store.InsertRunSkeleton(t.Context(), task.TicketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), firstRunID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(t.Context(), firstRunID, plan.OutcomeFailed, &exitCode, at); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	if fake.spawns[0].WorktreePath != worktreePath {
		t.Errorf("re-run spawned in %q, want the existing worktree %q", fake.spawns[0].WorktreePath, worktreePath)
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[task.TicketURL]
	if !ok {
		t.Fatal("no run recorded after re-run")
	}
	if summary.ID == firstRunID {
		t.Error("re-run must create a second runs row, not reuse the first")
	}
	if summary.HasOutcome {
		t.Error("the freshly spawned re-run must not already have an outcome")
	}

	pending, err := store.PendingVerbIntents(t.Context(), "re-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-run intents = %+v, want none: consumed", pending)
	}
}

// TestClosePRCallsGhPrCloseAndLogsTheEvent covers close-pr: the app can open a pull request, so
// it needs a sanctioned way to unopen one too.
func TestClosePRCallsGhPrCloseAndLogsTheEvent(t *testing.T) {
	// Not t.Parallel(): installFakeGh uses t.Setenv.
	root, _ := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "close-pr", at); err != nil {
		t.Fatal(err)
	}

	observe := func(context.Context) (cc.Observation, error) { return cc.Observation{}, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := countLines(t, ghLog); got != 1 {
		t.Fatalf("gh invocations = %d, want 1", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "close_pr_requested", "") {
		t.Error("no close_pr_requested event")
	}

	pending, err := store.PendingVerbIntents(t.Context(), "close-pr")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending close-pr intents = %+v, want none: consumed", pending)
	}
}
