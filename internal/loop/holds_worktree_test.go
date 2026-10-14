package loop_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func dispositionAs(t *testing.T, store *storepkg.Store, ticketURL string, outcome plan.Outcome, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), ticketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := store.RecordDisposition(
		t.Context(), runID, outcome, option.Some(exitCode), at, option.None[float64](),
	); err != nil {
		t.Fatal(err)
	}
}

func TestAutoRefreshLeavesAnAwaitingInputWorktreeUntouchedUntilItLeavesThatState(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	dispositionAs(t, store, f.child.URL, plan.OutcomeAsked, at.Add(time.Second))
	parentTip1 := advanceParent(t, repoPath, f)
	childTip0 := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD")); got != childTip0 {
		t.Errorf("child worktree HEAD = %s, want unchanged %s while awaiting input", got, childTip0)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	if !hasEvent(events, "refreshed", "") {
		t.Errorf("events = %+v, want a refreshed event once the ticket left awaiting input", events)
	}
}

func TestRetargetSkipsAnAwaitingInputTicketUntilItLeavesThatState(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	dispositionAs(t, store, f.child.URL, plan.OutcomeAsked, at.Add(time.Second))
	mergeParentIntoMain(t, repoPath)

	obs := mergedObservation(f, "parent")
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := ghLogLines(t, ghLog, "pr edit"); len(got) != 0 {
		t.Errorf("gh pr edit calls = %q, want none while awaiting input", got)
	}

	dispositionAs(t, store, f.child.URL, plan.OutcomePush, at.Add(2*time.Second))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce after the session moved on: %v", err)
	}
	if got := ghLogLines(t, ghLog, "pr edit"); len(got) != 1 || got[0] != "pr edit child --base main" {
		t.Errorf("gh pr edit calls = %q, want exactly [pr edit child --base main]", got)
	}
}

func TestRemoveWorktreeRefusesAnAwaitingInputTicket(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	dispositionAs(t, f.store, f.ticket.URL, plan.OutcomeAsked, f.at.Add(time.Second))
	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]plan.PR{plan.BranchKey("repo", "cc-1"): {State: plan.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("an awaiting-input worktree must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "awaiting an answer") {
		t.Errorf("events = %+v, want a remove_worktree_refused naming the awaited answer", events)
	}
}
