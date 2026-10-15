package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

func repoForMergify(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := gittest.Init(t)
	remote := filepath.Join(root, "remote.git")
	gittest.Run(t, ".", "init", "-q", "-b", "main", "--bare", remote)
	gittest.Run(t, repo, "remote", "add", "origin", remote)
	gittest.Run(t, repo, "push", "-q", "-u", "origin", "main")
	gittest.Run(t, repo, "fetch", "-q", "origin")
	return repo
}

func TestMergifyHashReadsTheRefNotTheWorkingTree(t *testing.T) {
	t.Parallel()
	repoPath := repoForMergify(t)

	mergify := filepath.Join(repoPath, ".mergify.yml")
	if err := os.WriteFile(mergify, []byte("pull_request_rules: []\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", mergify, err)
	}
	gittest.Run(t, repoPath, "add", ".mergify.yml")
	gittest.Run(t, repoPath, "commit", "-q", "-m", "add mergify config")
	gittest.Run(t, repoPath, "push", "-q", "origin", "main")
	gittest.Run(t, repoPath, "fetch", "-q", "origin")

	clean, err := mergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("MergifyHash(%q): %v", repoPath, err)
	}

	if err := os.WriteFile(mergify, []byte("pull_request_rules: [tampered]\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", mergify, err)
	}
	dirty, err := mergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("MergifyHash(%q): %v", repoPath, err)
	}
	if dirty != clean {
		t.Errorf("hash changed with a dirty working tree: %q then %q", clean, dirty)
	}

	gittest.Run(t, repoPath, "checkout", "-q", "-b", "elsewhere")
	gittest.Run(t, repoPath, "checkout", "-q", "--", ".mergify.yml")
	if err := os.Remove(mergify); err != nil {
		t.Fatalf("Remove(%q): %v", mergify, err)
	}
	gittest.Run(t, repoPath, "commit", "-q", "-am", "drop mergify config on another branch")

	elsewhere, err := mergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("hashing off another branch failed, so it is still reading the working tree: %v", err)
	}
	if elsewhere != clean {
		t.Errorf("hash changed on another branch: %q then %q", clean, elsewhere)
	}
}
