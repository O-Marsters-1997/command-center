package cc_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestARunOnceNeverSpawnsPastMaxAgentsAcrossAVerbAndAnAutomaticLaunch(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, tickets[0].URL, at)
	authoriseTicket(t, store, tickets[1].URL, plan.Hash(plan.Compose(plan.Ticket{URL: tickets[1].URL})), at)
	if err := store.QueueVerbIntent(t.Context(), tickets[0].URL, "re-run", at); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	fake := runner.NewFake()
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the re-run takes the only slot (max_agents = 1)", len(fake.Spawns))
	}
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ran := latest[tickets[1].URL]; ran {
		t.Error("sandbox://CC-2 launched past max_agents")
	}
}

func TestARunDisposedInAbsorbIsPushedInTheSameTick(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID, 4242, at, filepath.Join(t.TempDir(), "1.jsonl")); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	fake := runner.NewFake()
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !remoteHasBranch(t, root, "cc-1") {
		t.Fatal("the run disposed this tick was not pushed in the same tick")
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Errorf("gh invocations = %d, want 1 (pr create)", got)
	}
}
