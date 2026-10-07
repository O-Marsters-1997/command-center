package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")

	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", repoPath)
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("-C", repoPath, "add", "README.md")
	run("-C", repoPath, "commit", "-q", "-m", "initial")
	run("init", "-q", "-b", "main", "--bare", remote)
	run("-C", repoPath, "remote", "add", "origin", remote)
	run("-C", repoPath, "push", "-q", "-u", "origin", "main")
	run("-C", repoPath, "fetch", "-q", "origin")
	return root, repoPath
}
