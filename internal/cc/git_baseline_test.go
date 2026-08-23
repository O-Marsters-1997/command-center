package cc

import (
	"os"
	"os/exec"
	"testing"
)

var osEnviron = os.Environ

func initRepoForGitTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(osEnviron(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "initial")
	return dir
}

func commitEmpty(t *testing.T, dir, msg string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "commit", "-q", "--allow-empty", "-m", msg)
	cmd.Env = append(osEnviron(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
}

func TestBranchTipReadsTheCurrentTip(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	want, err := exec.Command("git", "-C", dir, "rev-parse", "refs/heads/main").Output()
	if err != nil {
		t.Fatal(err)
	}

	got, err := BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatalf("BranchTip: %v", err)
	}
	if got == "" || got != trimNewline(string(want)) {
		t.Errorf("BranchTip = %q, want %q", got, want)
	}
}

func TestCommitsSinceCountsCommitsAfterBaseline(t *testing.T) {
	t.Parallel()

	dir := initRepoForGitTest(t)
	baseline, err := BranchTip(t.Context(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	n, err := CommitsSince(t.Context(), dir, baseline)
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 0 {
		t.Errorf("commits since own tip = %d, want 0", n)
	}

	commitEmpty(t, dir, "agent commit")
	n, err = CommitsSince(t.Context(), dir, baseline)
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 1 {
		t.Errorf("commits since baseline = %d, want 1", n)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
