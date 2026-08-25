package cc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepoWithOriginForGitTest builds a repo with a pushed origin/main, the shape
// HasUnpushedCommits needs to resolve a remote-tracking ref against.
func initRepoWithOriginForGitTest(t *testing.T) (repoPath string) {
	t.Helper()
	root := t.TempDir()
	repoPath = filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(osEnviron(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", repoPath)
	run("-C", repoPath, "commit", "-q", "--allow-empty", "-m", "initial")
	run("init", "-q", "--bare", remote)
	run("-C", repoPath, "remote", "add", "origin", remote)
	run("-C", repoPath, "push", "-q", "-u", "origin", "main")
	return repoPath
}

func TestIsDirtyReportsAClean(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	dirty, err := IsDirty(t.Context(), dir)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("a freshly committed worktree reads dirty")
	}
}

func TestIsDirtyReportsAnUntrackedFile(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dirty, err := IsDirty(t.Context(), dir)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if !dirty {
		t.Error("an untracked file did not read dirty")
	}
}

func TestHasUnpushedCommitsIsFalseRightAfterAPush(t *testing.T) {
	t.Parallel()

	dir := initRepoWithOriginForGitTest(t)
	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main", "")
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if unpushed {
		t.Error("a branch at its just-pushed tip reads as having unpushed commits")
	}
}

func TestHasUnpushedCommitsIsTrueAfterALocalCommit(t *testing.T) {
	t.Parallel()

	dir := initRepoWithOriginForGitTest(t)
	commitEmpty(t, dir, "not yet pushed")

	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main", "")
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !unpushed {
		t.Error("a commit made after the last push did not read as unpushed")
	}
}

func TestHasUnpushedCommitsIsTrueWithNoRemoteTrackingRefAtAll(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t) // no origin remote configured at all
	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main", "")
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !unpushed {
		t.Error("a branch with no remote-tracking ref at all should read as unpushed (conservative default)")
	}
}

// The merged-and-deleted case: GitHub deletes the branch on merge, then the app's own
// `git fetch origin --prune` drops the remote-tracking ref. The recorded push is the only
// surviving evidence the branch ever reached the remote.
func TestHasUnpushedCommitsIsFalseWhenTheRefIsGoneButTheTipWasRecordedAsPushed(t *testing.T) {
	t.Parallel()

	dir := initRepoWithOriginForGitTest(t)
	tip, err := RevParse(t.Context(), dir, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	pruneRemoteTrackingRef(t, dir, "main")

	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main", tip)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if unpushed {
		t.Error("a branch at its recorded pushed tip read as unpushed once the pruned ref was gone")
	}
}

func TestHasUnpushedCommitsIsTrueWhenTheRefIsGoneAndTheTipMovedPastTheRecordedPush(t *testing.T) {
	t.Parallel()

	dir := initRepoWithOriginForGitTest(t)
	tip, err := RevParse(t.Context(), dir, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	pruneRemoteTrackingRef(t, dir, "main")
	commitEmpty(t, dir, "committed after the merge, never pushed")

	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main", tip)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !unpushed {
		t.Error("a commit made after the recorded push did not read as unpushed")
	}
}

// pruneRemoteTrackingRef deletes refs/remotes/origin/<branch> the way `fetch --prune` does once
// the remote branch is gone.
func pruneRemoteTrackingRef(t *testing.T, repoPath, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "update-ref", "-d", "refs/remotes/origin/"+branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update-ref -d: %v: %s", err, out)
	}
}

// TestWorktreesKeepsAMidRebaseWorktreeUnderItsBranch covers issue #91: git calls a rebasing
// worktree "detached", which dropped it from Observation.Worktrees and Observation.MidMerge
// alike, so the row read review_me while its worktree sat on an unresolved conflict.
func TestWorktreesKeepsAMidRebaseWorktreeUnderItsBranch(t *testing.T) {
	t.Parallel()

	repo, worktree := repoWithAConflictedRebase(t)

	worktrees, err := Worktrees(t.Context(), repo)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if got := worktrees["feature"]; got != worktree {
		t.Errorf("Worktrees()[%q] = %q, want %q", "feature", got, worktree)
	}

	mid, err := MidMerge(t.Context(), worktree)
	if err != nil {
		t.Fatalf("MidMerge: %v", err)
	}
	if !mid {
		t.Error("MidMerge reported a worktree stopped on a rebase conflict as resolved")
	}
}

// repoWithAConflictedRebase leaves worktree stopped on a rebase conflict, the state a restack
// onto a squash-merged base lands in when both branches touched the same lines (issue #85).
func repoWithAConflictedRebase(t *testing.T) (repo, worktree string) {
	t.Helper()
	repo = initRepoForGitTest(t)
	// git worktree list reports the symlink-resolved path, which on macOS is /private/var/...
	// for a /var/... TempDir, so the expectation has to be resolved the same way.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	worktree = filepath.Join(tmp, "feature")

	run := func(dir string, args ...string) ([]byte, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(osEnviron(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		return cmd.CombinedOutput()
	}
	mustRun := func(dir string, args ...string) {
		t.Helper()
		if out, err := run(dir, args...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(dir, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(repo, "base\n")
	mustRun(repo, "add", "shared.txt")
	mustRun(repo, "commit", "-q", "-m", "add shared")
	base, err := RevParse(t.Context(), repo, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}

	write(repo, "main side\n")
	mustRun(repo, "commit", "-q", "-am", "main rewrites shared")

	mustRun(repo, "worktree", "add", "-q", "-b", "feature", worktree, base)
	write(worktree, "feature side\n")
	mustRun(worktree, "commit", "-q", "-am", "feature rewrites shared")

	if out, err := run(worktree, "rebase", "--onto", "main", base); err == nil {
		t.Fatalf("rebase --onto main %s succeeded, want a conflict: %s", base, out)
	}
	return repo, worktree
}

// TestMidMergeClearsOnceAResolvedRebaseFinishes covers issue #91's other half. git leaves
// REBASE_HEAD behind after rebase --continue, so reading that ref pinned the row to
// refresh_conflicted for good, and the abort it then offered failed with "no rebase in progress".
func TestMidMergeClearsOnceAResolvedRebaseFinishes(t *testing.T) {
	t.Parallel()

	_, worktree := repoWithAConflictedRebase(t)
	resolveRebase(t, worktree)

	mid, err := MidMerge(t.Context(), worktree)
	if err != nil {
		t.Fatalf("MidMerge: %v", err)
	}
	if mid {
		t.Error("MidMerge still reports mid-rebase after the rebase finished, so the row can never " +
			"leave refresh_conflicted and refreshOne's own guard never lets it advance again")
	}
}

// resolveRebase does what a human does at the conflict and nothing else: write the file, add it,
// continue.
func resolveRebase(t *testing.T, worktree string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, "shared.txt"), []byte("both sides\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", worktree, "-c", "core.editor=true", "add", "shared.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", worktree, "-c", "core.editor=true", "rebase", "--continue")
	cmd.Env = append(osEnviron(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rebase --continue: %v: %s", err, out)
	}
}
