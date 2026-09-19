package cc_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// removeWorktreeFixture is one ticket fully run, pushed and recorded, its worktree cut for real --
// the shape every remove-worktree test starts from before diverging (dirty, unpushed, or left
// alone for a clean removal).
type removeWorktreeFixture struct {
	root, repoPath, worktreePath string
	store                        *cc.Store
	ws                           cc.Workspace
	cfg                          cc.Config
	ticket                       cc.Ticket
	runID                        int64
	at                           time.Time
	ghLog                        string
}

// installFakeTpRemove puts a script named tp on PATH supporting `remove [--merged|--force]
// <branch>`: it looks the branch's worktree path up via `git worktree list --porcelain` (run in
// repoPath, matching tp.Remove's cmd.Dir) and delegates to real git, refusing a dirty worktree or
// unpushed commits the same way real tp remove --merged does -- and skipping both checks the same
// way real tp remove --force does -- so these tests never depend on, or risk touching, a real
// treepad installation on the machine running them.
func installFakeTpRemove(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"[ \"$1\" = remove ] || { echo 'fake tp: only remove is supported' >&2; exit 1; }\n" +
		"shift\n" +
		"forced=\n" +
		"[ \"$1\" = --force ] && forced=1\n" +
		"[ \"$1\" = --merged ] || [ \"$1\" = --force ] && shift\n" +
		"branch=\"$1\"\n" +
		"path=$(git worktree list --porcelain | awk -v b=\"branch refs/heads/$branch\" " +
		"'/^worktree /{p=$2} $0==b{print p}')\n" +
		"[ -n \"$path\" ] || { echo \"fake tp: no worktree for $branch\" >&2; exit 1; }\n" +
		"if [ -z \"$forced\" ]; then\n" +
		"  dirty=$(git -C \"$path\" status --porcelain)\n" +
		"  [ -z \"$dirty\" ] || { echo 'fake tp: worktree is dirty' >&2; exit 1; }\n" +
		"  if git -C \"$path\" rev-parse --verify -q \"refs/remotes/origin/$branch\" >/dev/null 2>&1; then\n" +
		"    local_tip=$(git -C \"$path\" rev-parse \"refs/heads/$branch\")\n" +
		"    remote_tip=$(git -C \"$path\" rev-parse \"refs/remotes/origin/$branch\")\n" +
		"    [ \"$local_tip\" = \"$remote_tip\" ] || " +
		"{ echo 'fake tp: branch has unpushed commits' >&2; exit 1; }\n" +
		"  fi\n" +
		"fi\n" +
		"git worktree remove --force \"$path\"\n" +
		"git branch -D \"$branch\"\n"
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newRemoveWorktreeFixture(t *testing.T, branch string) removeWorktreeFixture {
	t.Helper()
	// Not t.Parallel(): repoWithOrigin, installFakeTpRemove and installFakeGh all use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeTpRemove(t)
	ghLog := installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, branch)
	commitFile(t, worktreePath, "agent.txt", "agent was here\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", branch)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://" + strings.ToUpper(branch), Repo: "repo", Branch: branch}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	runID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join("runs", fmt.Sprintf("%d.jsonl", runID))
	if err := store.RecordSpawn(t.Context(), runID, 111, at, logPath); err != nil {
		t.Fatal(err)
	}
	// Disposed as failed, deliberately: remove-worktree's own gate (merged, or base_gone) never
	// consults a run's outcome, and a push-outcome run would make this same tick's own
	// pushPushable step race to reconcile it too -- exactly the interference a focused verb
	// test must not have to account for.
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), runID, plan.OutcomeFailed, &exitCode, at); err != nil {
		t.Fatal(err)
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	writeRunLogFiles(t, ws.RunsDir, runID)

	return removeWorktreeFixture{
		root: root, repoPath: repoPath, worktreePath: worktreePath,
		store: store, ws: ws, cfg: cfg, ticket: ticket, runID: runID, at: at, ghLog: ghLog,
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

func (f removeWorktreeFixture) requestRemoveWorktree(t *testing.T, obs cc.Observation) error {
	t.Helper()
	at := f.at.Add(time.Second)
	if err := f.store.QueueVerbIntent(t.Context(), f.ticket.URL, "remove-worktree", at); err != nil {
		t.Fatal(err)
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	loop := cc.NewLoop(f.store, observe, fixedClock(at), f.cfg, f.ws, cc.ProcessRunner{})
	return loop.RunOnce(t.Context())
}

func TestRemoveWorktreeSucceedsForAMergedRowAndPrunesLogs(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree still exists at %s", f.worktreePath)
	}
	// tp remove --merged deletes the *local* branch; the remote copy is GitHub's own
	// deleteBranchOnMerge, unrelated to this call, so it is deliberately not asserted here.
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

	ghLog, err := os.ReadFile(f.ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ghLog), "issue close "+f.ticket.URL) {
		t.Errorf("gh.log = %q, want an issue close call for %s", ghLog, f.ticket.URL)
	}

	tickets, err := f.store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want the merged ticket's row dropped from the fleet", tickets)
	}
}

// TestRemoveWorktreeSucceedsWhenTheWorktreeIsAlreadyGone covers issue #196: a worktree removed by
// something other than this verb must not leave a merged row stuck refusing forever.
func TestRemoveWorktreeSucceedsWhenTheWorktreeIsAlreadyGone(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	obs := cc.Observation{
		Worktrees: map[string]string{},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
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

	ghLog, err := os.ReadFile(f.ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ghLog), "issue close "+f.ticket.URL) {
		t.Errorf("gh.log = %q, want an issue close call for %s", ghLog, f.ticket.URL)
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
	// A dependent whose blocker's PR closed unmerged after it ran: base_gone, not merged --
	// remove-worktree's other eligible state.
	f := newRemoveWorktreeFixture(t, "cc-2")
	blocker := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	dependent := cc.Ticket{URL: f.ticket.URL, Repo: "repo", Branch: "cc-2", BlockedBy: []string{blocker.URL}}
	if err := f.store.UpsertTickets(t.Context(), []cc.Ticket{blocker, dependent}); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-2"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Closed}},
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

// installFakeGhFailingIssueClose overrides the fixture's fake gh with one that fails only
// `issue close`, so a test can exercise that failure without touching any other gh call the
// verb might make.
func installFakeGhFailingIssueClose(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = \"issue close\" ]; then\n" +
		"  echo 'fake gh: issue close failed' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRemoveWorktreeTearsDownBeforeClosingTheIssue covers the reordering
// (docs/adr/0012-cc-proves-what-tp-cannot.md): tp remove runs before gh issue close, so a close
// failure leaves the worktree already gone, and the row stays to retry -- issue #196's own guard
// (a missing worktree is the state this verb is trying to reach) means the retry skips straight
// to the close instead of refusing on a worktree that no longer needs tearing down.
func TestRemoveWorktreeTearsDownBeforeClosingTheIssue(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	installFakeGhFailingIssueClose(t)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
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
	if !hasEvent(events, "remove_worktree_refused", "issue close failed") {
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

func TestRemoveWorktreeRefusesADirtyWorktree(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	if err := os.WriteFile(filepath.Join(f.worktreePath, "scratch.txt"), []byte("oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("a dirty worktree must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "dirty") {
		t.Error("no refusal event naming the worktree as dirty")
	}
}

func TestRemoveWorktreeRefusesUnpushedCommits(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	runGit(t, "-C", f.worktreePath, "commit", "-q", "--allow-empty", "-m", "not pushed")
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("a worktree with unpushed commits must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "unpushed") {
		t.Error("no refusal event naming unpushed commits")
	}
}

// TestRemoveWorktreeForcesPastTpWhenTheRefIsPrunedButTheTipMatches covers the fix
// (docs/adr/0012-cc-proves-what-tp-cannot.md): GitHub's delete-branch-on-merge, followed by this
// app's own fetch --prune, can leave tp with no remote ref left to check unpushed commits
// against -- at exactly the tip this app itself last pushed. cc proves that itself and forces
// past tp's own check, rather than refusing forever on a fact tp can no longer verify.
func TestRemoveWorktreeForcesPastTpWhenTheRefIsPrunedButTheTipMatches(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	tip := strings.TrimSpace(runGitOutput(t, "-C", f.worktreePath, "rev-parse", "HEAD"))
	if err := f.store.RecordPush(t.Context(), f.ticket.URL, tip, "main", "basesha", f.at); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", f.repoPath, "update-ref", "-d", "refs/remotes/origin/cc-1")

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
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

// TestRemoveWorktreeRefusesWhenTheRefIsPrunedAndTheTipHasDiverged covers the other half of the
// same gap: once the ref is gone, cc is the only thing left that can tell a safe force apart from
// a branch that has genuinely moved past what this app last pushed.
func TestRemoveWorktreeRefusesWhenTheRefIsPrunedAndTheTipHasDiverged(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	tip := strings.TrimSpace(runGitOutput(t, "-C", f.worktreePath, "rev-parse", "HEAD"))
	if err := f.store.RecordPush(t.Context(), f.ticket.URL, tip, "main", "basesha", f.at); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", f.repoPath, "update-ref", "-d", "refs/remotes/origin/cc-1")
	runGit(t, "-C", f.worktreePath, "commit", "-q", "--allow-empty", "-m", "diverged after prune")

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Merged}},
	}
	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("a branch that moved past its last pushed tip must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "unpushed") {
		t.Error("no refusal event naming unpushed commits")
	}
}

func TestRemoveWorktreeRefusesANonEligibleRow(t *testing.T) {
	f := newRemoveWorktreeFixture(t, "cc-1")
	// Open, not merged and not base_gone: still under review, nothing about it says it may
	// be torn down.
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): f.worktreePath},
		PRs:       map[string]gh.PR{cc.BranchKey("repo", "cc-1"): {State: gh.Open}},
	}

	if err := f.requestRemoveWorktree(t, obs); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(f.worktreePath); err != nil {
		t.Fatalf("an open, unmerged row must never be removed: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "remove_worktree_refused", "neither merged nor base gone") {
		t.Error("no refusal event explaining why")
	}
}
