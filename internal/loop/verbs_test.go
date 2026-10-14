package loop_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

type removeWorktreeFixture struct {
	root, repoPath, worktreePath string
	store                        *storepkg.Store
	ws                           config.Workspace
	cfg                          config.Config
	ticket                       storepkg.Ticket
	runID                        int64
	at                           time.Time
	forge                        *recordingForge
	worktrees                    *recordingWorktrees
	forgeOption                  loop.Option
	failRemove                   bool
}

func newRemoveWorktreeFixture(t *testing.T, branch string) removeWorktreeFixture {
	t.Helper()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}
	worktrees := &recordingWorktrees{}
	worktreePath := cutWorktree(t, repoPath, branch)
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", branch)

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://" + strings.ToUpper(branch), Repo: "repo", Branch: branch}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join("runs", fmt.Sprintf("%d.jsonl", runID))
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: logPath}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatal(err)
	}
	// Disposed as failed, deliberately: remove-worktree's own gate (merged, or base_gone) never
	// consults a run's outcome, and a push-outcome run would make this same tick's own
	// pushPushable step race to reconcile it too -- exactly the interference a focused verb
	// test must not have to account for.
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	writeRunLogFiles(t, ws.RunsDir, runID)

	return removeWorktreeFixture{
		root: root, repoPath: repoPath, worktreePath: worktreePath,
		store: store, ws: ws, cfg: cfg, ticket: ticket, runID: runID, at: at,
		forge: forge, worktrees: worktrees, forgeOption: loop.WithForge(forge),
	}
}

func writeRunLogFiles(t *testing.T, runsDir string, runID int64) {
	t.Helper()
	for _, ext := range []string{"jsonl", "prompt"} {
		path := filepath.Join(runsDir, fmt.Sprintf("%d.%s", runID, ext))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runLogFilesExist(runsDir string, runID int64) bool {
	for _, ext := range []string{"jsonl", "prompt"} {
		if _, err := os.Stat(filepath.Join(runsDir, fmt.Sprintf("%d.%s", runID, ext))); err == nil {
			return true
		}
	}
	return false
}

func (f removeWorktreeFixture) requestRemoveWorktree(t *testing.T, obs plan.Observation) error {
	t.Helper()
	at := f.at.Add(time.Second)
	if err := f.store.QueueVerbIntent(t.Context(), f.ticket.URL, "remove-worktree", at); err != nil {
		t.Fatal(err)
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	worktreeOption := loop.WithWorktrees(f.worktrees)
	if f.failRemove {
		worktreeOption = loop.WithWorktrees(failingRemove{f.worktrees})
	}
	lp := newLoop(f.store, observe, fixedClock(at), f.cfg, f.ws, runner.ProcessRunner{},
		f.forgeOption, worktreeOption)
	return lp.RunOnce(t.Context())
}

func TestRemoveWorktreeSucceedsForAMergedRowAndPrunesLogs(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree still exists at %s", f.worktreePath)
	}
	if err := exec.Command("git", "-C", f.repoPath, "rev-parse", "--verify", "refs/heads/cc-1").Run(); err == nil {
		t.Error("the local branch still exists after remove-worktree")
	}
	if runLogFilesExist(f.ws.RunsDir, f.runID) {
		t.Error("run log files were not pruned")
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "worktree_removed", "") {
		t.Error("no worktree_removed event")
	}

	if calls := f.forge.Calls("CloseIssue"); len(calls) != 1 || calls[0].Issue != f.ticket.URL {
		t.Errorf("CloseIssue calls = %+v, want one close for %s", calls, f.ticket.URL)
	}

	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want the merged ticket's row dropped from the fleet", tickets)
	}
}

func TestRemoveWorktreeOnAMergedRowRepairsDependentsBlockedBy(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	dependent := storepkg.Ticket{URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2", BlockedBy: []string{f.ticket.URL}}
	if err := f.store.UpsertTickets(t.Context(), []storepkg.Ticket{dependent}); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}
	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != dependent.URL {
		t.Fatalf("tickets = %+v, want only the dependent left", tickets)
	}
	if len(tickets[0].BlockedBy) != 0 {
		t.Errorf("dependent's blocked_by = %v, want the merged blocker pruned", tickets[0].BlockedBy)
	}
}

func TestRemoveWorktreeSucceedsWhenTheWorktreeIsAlreadyGone(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "remove_worktree_refused", "no worktree for cc-1") {
		t.Error("a merged ticket with no observed worktree must not refuse")
	}
	if !hasEvent(events, "worktree_removed", "") {
		t.Error("no worktree_removed event")
	}

	if calls := f.forge.Calls("CloseIssue"); len(calls) != 1 || calls[0].Issue != f.ticket.URL {
		t.Errorf("CloseIssue calls = %+v, want one close for %s", calls, f.ticket.URL)
	}

	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want the row dropped even though the worktree was already gone", tickets)
	}
}

func TestRemoveWorktreeSucceedsForABaseGoneRow(t *testing.T) {
	t.Parallel()
	// A dependent whose blocker's PR closed unmerged after it ran: base_gone, not merged --
	// remove-worktree's other eligible state.
	f := newRemoveWorktreeFixture(t, "cc-2")
	blocker := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	dependent := storepkg.Ticket{URL: f.ticket.URL, Repo: "repo", Branch: "cc-2", BlockedBy: []string{blocker.URL}}
	if err := f.store.UpsertTickets(t.Context(), []storepkg.Ticket{blocker, dependent}); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-2"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Closed}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(f.worktreePath); !os.IsNotExist(err) {
		t.Error("a base_gone row must be removable too")
	}

	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != blocker.URL {
		t.Errorf("tickets = %+v, want only the dependent dropped, the blocker left alone", tickets)
	}
}

func TestRemoveWorktreeTearsDownBeforeClosingTheIssue(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	f.forgeOption = loop.WithForge(failingCloseIssue{f.forge})
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); !os.IsNotExist(err) {
		t.Error("the worktree should already be torn down before the issue close is even attempted")
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "forge refused") {
		t.Error("no remove_worktree_refused event naming the issue close failure")
	}
	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Errorf("tickets = %+v, want the row left in place to retry the close", tickets)
	}
}

func TestRemoveWorktreeKeepsTheRowWhenRemovalFails(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	f.failRemove = true
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}
	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Errorf("worktree after failed remove: %v, want it preserved", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "forge refused") {
		t.Errorf("events = %+v, want a refused remove", events)
	}
	if calls := f.forge.Calls("CloseIssue"); len(calls) != 0 {
		t.Errorf("CloseIssue calls = %+v, want none before removal succeeds", calls)
	}
	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != f.ticket.URL {
		t.Errorf("tickets after failed remove = %+v, want the ticket retained", tickets)
	}
}

func TestRemoveWorktreeForcesPastTpWhenTheRefIsPrunedButTheTipMatches(t *testing.T) {
	t.Parallel()
	f := newRemoveWorktreeFixture(t, "cc-1")
	tip := gittest.Run(t, f.worktreePath, "rev-parse", "HEAD")
	push := storepkg.Push{
		Ticket: plan.TicketURL(f.ticket.URL), Tip: plan.SHA(tip), BaseSHA: "basesha", BaseBranch: "main", At: f.at,
	}
	if err := f.store.RecordPush(t.Context(), push); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, f.repoPath, "update-ref", "-d", "refs/remotes/origin/cc-1")

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged}},
	}
	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); !os.IsNotExist(err) {
		t.Error("a branch at its last pushed tip, with the remote ref pruned, must still be removable")
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "worktree_removed", "forced") {
		t.Error("no worktree_removed event recording that removal was forced")
	}
	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want the row dropped", tickets)
	}
}

func TestRemoveWorktreeAtTheMergedHeadDespiteAStaleRecordedPush(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		advance   bool
		wantFound bool
	}{
		{name: "tip equals the merged head", wantFound: false},
		{name: "tip ahead of the merged head", advance: true, wantFound: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newRemoveWorktreeFixture(t, "cc-1")
			head := gittest.Run(t, f.worktreePath, "rev-parse", "HEAD")
			stale := storepkg.Push{
				Ticket: plan.TicketURL(f.ticket.URL), Tip: "stale", BaseSHA: "basesha", BaseBranch: "main", At: f.at,
			}
			if err := f.store.RecordPush(t.Context(), stale); err != nil {
				t.Fatal(err)
			}
			gittest.Run(t, f.repoPath, "update-ref", "-d", "refs/remotes/origin/cc-1")
			if tt.advance {
				gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "after the merged head")
			}

			obs := plan.Observation{
				Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
				PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: plan.Merged, HeadOid: plan.SHA(head)}},
			}
			if err := f.requestRemoveWorktree(t, obs); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}

			_, err := os.Stat(f.worktreePath)
			if gotFound := err == nil; gotFound != tt.wantFound {
				t.Errorf("worktree present = %v, want %v", gotFound, tt.wantFound)
			}
			events, err := f.store.Events(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if refused := hasEvent(events, "remove_worktree_refused", "unpushed"); refused != tt.wantFound {
				t.Errorf("refused for unpushed commits = %v, want %v", refused, tt.wantFound)
			}
		})
	}
}

func TestRemoveWorktreeRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		pr     plan.PRState
		setup  func(t *testing.T, f removeWorktreeFixture)
		reason string
	}{
		{
			name: "dirty worktree", pr: plan.Merged, reason: "dirty",
			setup: func(t *testing.T, f removeWorktreeFixture) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(f.worktreePath, "scratch.txt"), []byte("oops\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unpushed commits", pr: plan.Merged, reason: "unpushed",
			setup: func(t *testing.T, f removeWorktreeFixture) {
				t.Helper()
				gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "not pushed")
			},
		},
		{
			name: "pruned ref and a tip that moved past the last push", pr: plan.Merged, reason: "unpushed",
			setup: func(t *testing.T, f removeWorktreeFixture) {
				t.Helper()
				tip := gittest.Run(t, f.worktreePath, "rev-parse", "HEAD")
				push := storepkg.Push{
					Ticket: plan.TicketURL(f.ticket.URL), Tip: plan.SHA(tip), BaseSHA: "basesha", BaseBranch: "main",
					At: f.at,
				}
				if err := f.store.RecordPush(t.Context(), push); err != nil {
					t.Fatal(err)
				}
				gittest.Run(t, f.repoPath, "update-ref", "-d", "refs/remotes/origin/cc-1")
				gittest.Run(t, f.worktreePath, "commit", "-q", "--allow-empty", "-m", "diverged after prune")
			},
		},
		{
			name: "open row, neither merged nor base gone", pr: plan.Open, reason: "neither merged nor base gone",
			setup: func(*testing.T, removeWorktreeFixture) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newRemoveWorktreeFixture(t, "cc-1")
			tt.setup(t, f)
			obs := plan.Observation{
				Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.worktreePath},
				PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {State: tt.pr}},
			}

			if err := f.requestRemoveWorktree(t, obs); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if _, err := os.Stat(f.worktreePath); err != nil {
				t.Fatalf("a refused worktree must never be removed: %v", err)
			}
			events, err := f.store.Events(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !hasEvent(events, "remove_worktree_refused", tt.reason) {
				t.Errorf("no remove_worktree_refused event naming %q", tt.reason)
			}
		})
	}
}
