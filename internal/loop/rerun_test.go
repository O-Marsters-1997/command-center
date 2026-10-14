package loop_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestReRunSpawnsASecondRunInTheSameWorktreeWithoutCutting(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	at := testAt
	firstRunID, err := f.Store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: f.Ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(firstRunID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := f.Store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	exitCode := 1
	if err := f.Store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(firstRunID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
	if err := f.Store.QueueVerbIntent(t.Context(), f.Ticket.URL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	fake := runner.NewFake()
	f.reset(at.Add(time.Second), fake)
	f.tick(t)

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	if fake.Spawns[0].WorktreePath != f.Worktree {
		t.Errorf("re-run spawned in %q, want the existing worktree %q", fake.Spawns[0].WorktreePath, f.Worktree)
	}
	if fake.Spawns[0].SystemPromptPath != f.ws.SystemPromptPath {
		t.Errorf("system prompt path = %q, want %q", fake.Spawns[0].SystemPromptPath, f.ws.SystemPromptPath)
	}

	latest, err := f.Store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	summary, ok := latest[plan.TicketURL(f.Ticket.URL)]
	if !ok {
		t.Fatal("no run recorded after re-run")
	}
	if int64(summary.ID) == firstRunID {
		t.Error("re-run must create a second runs row, not reuse the first")
	}
	if summary.Outcome.IsSome() {
		t.Error("the freshly spawned re-run must not already have an outcome")
	}

	pending, err := f.Store.PendingVerbIntents(t.Context(), "re-run")
	if err != nil {
		t.Fatalf("PendingVerbIntents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-run intents = %+v, want none: consumed", pending)
	}
}

func TestReRunOnAGoneWorktreeCutsAFreshOneAndSpawns(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t, loop.WithWorktrees(&recordingWorktrees{}))
	gittest.Run(t, f.Repo, "worktree", "remove", "--force", f.Worktree)
	at := testAt
	firstRunID, err := f.Store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: f.Ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(firstRunID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := f.Store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	exitCode := 1
	if err := f.Store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(firstRunID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}
	if err := f.Store.QueueVerbIntent(t.Context(), f.Ticket.URL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	delete(f.Obs.Worktrees, branchRef("repo", "cc-1"))
	fake := runner.NewFake()
	f.reset(at.Add(time.Second), fake, loop.WithWorktrees(&recordingWorktrees{}))
	f.tick(t)

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	if !strings.HasSuffix(fake.Spawns[0].WorktreePath, "wt-cc-1") {
		t.Errorf("re-run spawned in %q, want a freshly cut worktree", fake.Spawns[0].WorktreePath)
	}

	latest, err := f.Store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	summary, ok := latest[plan.TicketURL(f.Ticket.URL)]
	if !ok {
		t.Fatal("no run recorded after re-run")
	}
	if int64(summary.ID) == firstRunID {
		t.Error("re-run must create a second runs row, not reuse the first")
	}

	events := f.events(t, "re_run_refused")
	if len(events) != 0 {
		t.Error("re-run must not refuse when the worktree is gone")
	}

	pending, err := f.Store.PendingVerbIntents(t.Context(), "re-run")
	if err != nil {
		t.Fatalf("PendingVerbIntents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending re-run intents = %+v, want none: consumed", pending)
	}
}
