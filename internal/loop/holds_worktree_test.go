package loop_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func dispositionAs(t *testing.T, store *storepkg.Store, ticketURL string, outcome plan.Outcome, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticketURL, err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: outcome, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
}

func TestAutoRefreshLeavesAnAwaitingInputWorktreeUntouchedUntilItLeavesThatState(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := testAt

	f := newStackedFixture(t, repoPath, store, at)
	dispositionAs(t, store, f.child.URL, plan.OutcomeAsked, at.Add(time.Second))
	parentTip1 := advanceParent(t, repoPath, f)
	childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, cctest.Clock{At: at.Add(time.Minute)}, cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD"); got != childTip0 {
		t.Errorf("child worktree HEAD = %s, want unchanged %s while awaiting input", got, childTip0)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	if hasEvent(events, "refreshed", "") || hasEvent(events, "refresh_refused", "") {
		t.Errorf("events = %+v, want no refresh activity while awaiting input", events)
	}

	dispositionAs(t, store, f.child.URL, plan.OutcomePush, at.Add(2*time.Second))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce after the session moved on: %v", err)
	}
	events, err = store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	if !hasEvent(events, "refreshed", "") {
		t.Errorf("events = %+v, want a refreshed event once the ticket left awaiting input", events)
	}
}

func TestRetargetSkipsAnAwaitingInputTicketUntilItLeavesThatState(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}
	store := openStore(t)
	at := testAt

	f := newStackedFixture(t, repoPath, store, at)
	dispositionAs(t, store, f.child.URL, plan.OutcomeAsked, at.Add(time.Second))
	mergeParentIntoMain(t, repoPath)

	obs := mergedObservation(f, "parent")
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, cctest.Clock{At: at.Add(time.Minute)}, cfg, ws, runner.ProcessRunner{},
		loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := forge.Calls("Edit"); len(got) != 0 {
		t.Errorf("forge Edit calls = %+v, want none while awaiting input", got)
	}

	dispositionAs(t, store, f.child.URL, plan.OutcomePush, at.Add(2*time.Second))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce after the session moved on: %v", err)
	}
	want := forgeCall{Method: "Edit", Repo: repoPath, Branch: "child", Base: "main"}
	if got := forge.Calls("Edit"); len(got) != 1 || got[0] != want {
		t.Errorf("forge Edit calls = %+v, want one edit of child onto main", got)
	}
}

func TestRemoveWorktreeRefusesAnAwaitingInputTicket(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	dispositionAs(t, f.store, f.ticket.URL, plan.OutcomeAsked, f.at.Add(time.Second))
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("an awaiting-input worktree must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	if !hasEvent(events, "remove_worktree_refused", "awaiting an answer") {
		t.Errorf("events = %+v, want a remove_worktree_refused naming the awaited answer", events)
	}
}
