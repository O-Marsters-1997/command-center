package loop_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestARunOnceNeverSpawnsPastMaxAgentsAcrossAVerbAndAnAutomaticLaunch(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatalf("UpsertTickets(%d tickets): %v", len(tickets), err)
	}
	at := testAt
	dispositionAsPushed(t, store, tickets[0].URL, at)
	authoriseTicket(t, store, tickets[1].URL, composeHash(tickets[1].URL), at)
	if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: tickets[0].URL, Verb: "re-run", At: at,
	}); err != nil {
		t.Fatalf("QueueVerbIntent(%q, re-run): %v", tickets[0].URL, err)
	}

	obs := plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	fake := runner.NewFake()
	lp := newLoop(store, observe, cctest.Clock{At: at.Add(time.Minute)}, cfg, ws, fake,
		loop.WithForge(&recordingForge{}), loop.WithWorktrees(&recordingWorktrees{}))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the re-run takes the only slot (max_agents = 1)", len(fake.Spawns))
	}
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket(): %v", err)
	}
	if _, ran := latest[plan.TicketURL(tickets[1].URL)]; ran {
		t.Error("sandbox://CC-2 launched past max_agents")
	}
}

func TestARunDisposedInAbsorbIsPushedInTheSameTick(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	baseline := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets(%q): %v", ticket.URL, err)
	}
	at := testAt
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", BaselineSHA: baseline, PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticket.URL, err)
	}
	spawn := storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: at, LogPath: filepath.Join(t.TempDir(), "1.jsonl"),
	}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}

	obs := plan.Observation{Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	fake := runner.NewFake()
	lp := newLoop(store, observe, cctest.Clock{At: at.Add(time.Minute)}, cfg, ws, fake, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
		t.Fatal("the run disposed this tick was not pushed in the same tick")
	}
	if got := len(forge.Calls("Create")); got != 1 {
		t.Errorf("Create calls = %d, want 1", got)
	}
}

func TestATickPrunesTickErrorsPastRetention(t *testing.T) {
	t.Parallel()
	f := newLoopFixture(t)
	stale := storepkg.Event{At: testAt.Add(-31 * 24 * time.Hour), Kind: storepkg.EventTickError, Detail: "old"}
	recent := storepkg.Event{At: testAt.Add(-time.Hour), Kind: storepkg.EventTickError, Detail: "new"}
	for _, e := range []storepkg.Event{stale, recent} {
		if err := f.Store.AppendEvent(t.Context(), e); err != nil {
			t.Fatalf("AppendEvent(%s): %v", e.Kind, err)
		}
	}

	f.Tick(t)

	events, err := f.Store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	var details []string
	for _, e := range events {
		if e.Kind == storepkg.EventTickError {
			details = append(details, e.Detail)
		}
	}
	if len(details) != 1 || details[0] != "new" {
		t.Errorf("tick_error details after a tick = %v, want [new]", details)
	}
}
