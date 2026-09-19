package cc_test

import (
	"context"
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

func conflictedWorktree(t *testing.T, repoPath, worktreePath, relPath string) {
	t.Helper()
	commitFile(t, worktreePath, relPath, "child version\n")
	commitFile(t, repoPath, relPath, "main version\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	runGit(t, "-C", worktreePath, "fetch", "-q", "origin")
	cmd := exec.Command("git", "-C", worktreePath, "merge", "origin/main")
	if err := cmd.Run(); err == nil {
		t.Fatal("merge unexpectedly succeeded, want a conflict")
	}
}

func resolveAndStage(t *testing.T, worktreePath, relPath, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktreePath, relPath), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", worktreePath, "add", relPath)
}

func commitResolutionFixture(t *testing.T, root, repoPath string) (*cc.Store, cc.Ticket, time.Time) {
	t.Helper()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbCommitResolution, at); err != nil {
		t.Fatal(err)
	}
	return store, ticket, at
}

func TestCommitResolutionCommitsTheStagedMergeAndPushesInTheSameTick(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin and installFakeGh both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, "shared.txt")
	resolveAndStage(t, worktreePath, "shared.txt", "resolved version\n")

	store, _, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if mid, err := cc.MidMerge(t.Context(), worktreePath); err != nil {
		t.Fatal(err)
	} else if mid {
		t.Error("worktree is still mid-merge: MERGE_HEAD must be cleared by the commit")
	}

	head := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))
	parents := strings.Fields(runGitOutput(t, "-C", worktreePath, "log", "-1", "--format=%P", head))
	if len(parents) != 2 {
		t.Fatalf("HEAD has %d parents, want 2 (a merge commit): %v", len(parents), parents)
	}
	if !remoteHasBranch(t, root, "cc-1") {
		t.Fatal("branch was not pushed to origin")
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Fatalf("gh invocations = %d, want 1 (pr create)", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "commit_resolution_refused", "") {
		t.Errorf("events = %+v, want no commit_resolution_refused", events)
	}
	if !hasEvent(events, "pushed", "") {
		t.Errorf("events = %+v, want a pushed event", events)
	}

	pending, err := store.PendingVerbIntents(t.Context(), plan.VerbCommitResolution)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending commit-resolution intents = %+v, want none: consumed", pending)
	}
}

func TestCommitResolutionRefusesAnUnmergedPath(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, "shared.txt")

	store, _, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if mid, err := cc.MidMerge(t.Context(), worktreePath); err != nil {
		t.Fatal(err)
	} else if !mid {
		t.Error("worktree left the merge: an unmerged path must refuse the commit")
	}
	if remoteHasBranch(t, root, "cc-1") {
		t.Error("branch must not be pushed: nothing was committed")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "commit_resolution_refused", "unmerged") {
		t.Errorf("events = %+v, want a commit_resolution_refused naming the unmerged path", events)
	}
}

func TestCommitResolutionRefusesWhenNothingIsStaged(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, "shared.txt")
	// git stages "ours" unchanged from HEAD, so git diff --cached shows nothing.
	runGit(t, "-C", worktreePath, "checkout", "--ours", "--", "shared.txt")
	runGit(t, "-C", worktreePath, "add", "shared.txt")

	store, _, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if mid, err := cc.MidMerge(t.Context(), worktreePath); err != nil {
		t.Fatal(err)
	} else if !mid {
		t.Error("worktree left the merge: nothing staged must refuse the commit")
	}
	if remoteHasBranch(t, root, "cc-1") {
		t.Error("branch must not be pushed: nothing was committed")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "commit_resolution_refused", "nothing staged") {
		t.Errorf("events = %+v, want a commit_resolution_refused naming nothing staged", events)
	}
}

func TestCommitResolutionRefusesALiveRun(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store, ticket, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath},
		PRs:       map[string]gh.PR{},
		Runs:      map[string]cc.RunObservation{ticket.URL: {Alive: true}},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if remoteHasBranch(t, root, "cc-1") {
		t.Error("branch must not be pushed: a live run refuses the verb")
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "commit_resolution_refused", "a run is alive") {
		t.Errorf("events = %+v, want a commit_resolution_refused naming the live run", events)
	}
}

func TestCommitResolutionTouchingADeniedPathIsRefusedByThePushPolicy(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, ".github/workflows/ci.yml")
	resolveAndStage(t, worktreePath, ".github/workflows/ci.yml", "resolved\n")

	store, _, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if mid, err := cc.MidMerge(t.Context(), worktreePath); err != nil {
		t.Fatal(err)
	} else if mid {
		t.Error("worktree is still mid-merge: the commit itself is never gated by the push policy")
	}
	if remoteHasBranch(t, root, "cc-1") {
		t.Error("branch must not be pushed: .github/** is denied")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "commit_resolution_refused", "") {
		t.Errorf("events = %+v, want no commit_resolution_refused: the refusal is pushOne's own push_refused", events)
	}
	if !hasEvent(events, "push_refused", ".github") {
		t.Errorf("events = %+v, want a push_refused naming the denied path", events)
	}
}

func TestCommitResolutionSkipsTheCommitWhenAHumanAlreadyCommittedByHand(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin and installFakeGh both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, "shared.txt")
	resolveAndStage(t, worktreePath, "shared.txt", "resolved by hand\n")
	runGit(t, "-C", worktreePath, "commit", "--no-edit")
	headBefore := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))

	store, _, at := commitResolutionFixture(t, root, repoPath)
	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	headAfter := strings.TrimSpace(runGitOutput(t, "-C", worktreePath, "rev-parse", "HEAD"))
	if headAfter != headBefore {
		t.Errorf("HEAD moved from %s to %s: commit-resolution must not commit a second time", headBefore, headAfter)
	}
	if !remoteHasBranch(t, root, "cc-1") {
		t.Fatal("branch was not pushed to origin")
	}
	if got := countLines(t, ghLog); got != 1 {
		t.Fatalf("gh invocations = %d, want 1 (pr create)", got)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "commit_resolution_refused", "") {
		t.Errorf("events = %+v, want no commit_resolution_refused", events)
	}
}
