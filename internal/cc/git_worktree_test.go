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
	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main")
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

	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main")
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
	unpushed, err := HasUnpushedCommits(t.Context(), dir, "main")
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !unpushed {
		t.Error("a branch with no remote-tracking ref at all should read as unpushed (conservative default)")
	}
}
