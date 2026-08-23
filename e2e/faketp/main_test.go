package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func initRepo(t *testing.T) string {
	t.Helper()
	// EvalSymlinks so the path matches what `git rev-parse --show-toplevel` reports on macOS.
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	repo := filepath.Join(parent, "sandbox")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("sandbox\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-q", "-m", "initial commit")
	t.Chdir(repo)
	return repo
}

// squashMergedBranch creates a worktree on branch, commits in it, and squash-merges into main —
// the shape `git branch -d` refuses to delete.
func squashMergedBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"new", branch, "--base", "main"}, env(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("faketp new: exit %d (stderr: %q)", code, stderr.String())
	}
	worktree := siblingPath(repo, branch)
	if err := os.WriteFile(filepath.Join(worktree, "feature.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("writing feature file: %v", err)
	}
	git(t, worktree, "add", "feature.txt")
	git(t, worktree, "commit", "-q", "-m", "feature")
	git(t, repo, "merge", "--squash", branch)
	git(t, repo, "commit", "-q", "-m", "squashed feature")
	return worktree
}

func TestRemoveRefusesSquashMergedBranchWithoutForce(t *testing.T) {
	repo := initRepo(t)
	worktree := squashMergedBranch(t, repo, "feat/squashed")

	var stdout, stderr bytes.Buffer
	code := run([]string{"remove", "feat/squashed"}, env(nil), &stdout, &stderr)

	if code == 0 {
		t.Errorf("exit code = 0, want non-zero (stderr: %q)", stderr.String())
	}
	if branches := git(t, repo, "branch", "--list", "feat/squashed"); !strings.Contains(branches, "feat/squashed") {
		t.Errorf("branch list = %q, want the refused branch to survive", branches)
	}
	// Deliberate: real tp removes the worktree before deleting the branch, so a refusal leaves
	// the worktree gone and the branch behind. The fake reproduces that ordering.
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want the worktree already removed", worktree, err)
	}
}

func TestRemoveForceDeletesSquashMergedBranch(t *testing.T) {
	repo := initRepo(t)
	worktree := squashMergedBranch(t, repo, "feat/squashed")

	var stdout, stderr bytes.Buffer
	code := run([]string{"remove", "--force", "feat/squashed"}, env(nil), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if branches := git(t, repo, "branch", "--list", "feat/squashed"); strings.TrimSpace(branches) != "" {
		t.Errorf("branch list = %q, want the branch deleted", branches)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want the worktree removed", worktree, err)
	}
}

func TestFailEnvCreatesNothing(t *testing.T) {
	repo := initRepo(t)

	var stdout, stderr bytes.Buffer
	getenv := env(map[string]string{"CC_TP_FAIL": "1"})
	code := run([]string{"new", "feat/issue-1", "--base", "main"}, getenv, &stdout, &stderr)

	if code == 0 {
		t.Error("exit code = 0, want non-zero when CC_TP_FAIL is set")
	}
	sibling := filepath.Join(filepath.Dir(repo), "sandbox-feat-issue-1")
	if _, err := os.Stat(sibling); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want it not to exist", sibling, err)
	}
	if listed := git(t, repo, "worktree", "list", "--porcelain"); strings.Contains(listed, "feat/issue-1") {
		t.Errorf("worktree list = %q, want no feat/issue-1 entry", listed)
	}
}

func TestCCTPLogRecordsEveryAttemptEvenAFailingOne(t *testing.T) {
	initRepo(t)
	logPath := filepath.Join(t.TempDir(), "tp.log")

	var stdout, stderr bytes.Buffer
	getenv := env(map[string]string{"CC_TP_FAIL": "1", "CC_TP_LOG": logPath})
	if code := run([]string{"new", "feat/issue-1", "--base", "main"}, getenv, &stdout, &stderr); code == 0 {
		t.Fatal("exit code = 0, want non-zero when CC_TP_FAIL is set")
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tp log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(logged), "\n"), "\n")
	if len(lines) != 1 || lines[0] != "new feat/issue-1 --base main" {
		t.Errorf("tp log = %q, want exactly one line for the attempted call", logged)
	}
}

func TestNewAddsSiblingWorktree(t *testing.T) {
	repo := initRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"new", "feat/issue-1", "--base", "main"}, env(nil), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	want := filepath.Join(filepath.Dir(repo), "sandbox-feat-issue-1")
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("worktree %s: stat err %v", want, err)
	}
	listed := git(t, repo, "worktree", "list", "--porcelain")
	if !strings.Contains(listed, "worktree "+want) {
		t.Errorf("worktree list = %q, want it to contain %q", listed, want)
	}
	if !strings.Contains(listed, "branch refs/heads/feat/issue-1") {
		t.Errorf("worktree list = %q, want branch refs/heads/feat/issue-1", listed)
	}
}
