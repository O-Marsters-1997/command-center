package loop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestReRunSpawnsASecondRunInTheSameWorktreeWithoutCutting(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	firstRunID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), firstRunID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(t.Context(), firstRunID, plan.OutcomeFailed, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	if fake.Spawns[0].WorktreePath != worktreePath {
		t.Errorf("re-run spawned in %q, want the existing worktree %q", fake.Spawns[0].WorktreePath, worktreePath)
	}
	if fake.Spawns[0].SystemPromptPath != ws.SystemPromptPath {
		t.Errorf("system prompt path = %q, want %q", fake.Spawns[0].SystemPromptPath, ws.SystemPromptPath)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[ticket.URL]
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

func TestReRunOnAGoneWorktreeCutsAFreshOneAndSpawns(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	runGit(t, "-C", repoPath, "worktree", "remove", "--force", worktreePath)

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	firstRunID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), firstRunID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(t.Context(), firstRunID, plan.OutcomeFailed, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{}, PRs: map[string]plan.PR{}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	if !strings.HasSuffix(fake.Spawns[0].WorktreePath, "wt-cc-1") {
		t.Errorf("re-run spawned in %q, want a freshly cut worktree", fake.Spawns[0].WorktreePath)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[ticket.URL]
	if !ok {
		t.Fatal("no run recorded after re-run")
	}
	if summary.ID == firstRunID {
		t.Error("re-run must create a second runs row, not reuse the first")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "re_run_refused", "") {
		t.Error("re-run must not refuse when the worktree is gone")
	}

	pending, err := store.PendingVerbIntents(t.Context(), "re-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-run intents = %+v, want none: consumed", pending)
	}
}
