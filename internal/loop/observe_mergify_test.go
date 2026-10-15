package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

func TestMergifyHashReadsTheRefNotTheWorkingTree(t *testing.T) {
	t.Parallel()
	_, repoPath := repoWithOrigin(t)

	mergify := filepath.Join(repoPath, ".mergify.yml")
	if err := os.WriteFile(mergify, []byte("pull_request_rules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, repoPath, "add", ".mergify.yml")
	gittest.Run(t, repoPath, "commit", "-q", "-m", "add mergify config")
	gittest.Run(t, repoPath, "push", "-q", "origin", "main")
	gittest.Run(t, repoPath, "fetch", "-q", "origin")

	clean, err := loop.MergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(mergify, []byte("pull_request_rules: [tampered]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty, err := loop.MergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if dirty != clean {
		t.Errorf("hash changed with a dirty working tree: %q then %q", clean, dirty)
	}

	gittest.Run(t, repoPath, "checkout", "-q", "-b", "elsewhere")
	gittest.Run(t, repoPath, "checkout", "-q", "--", ".mergify.yml")
	if err := os.Remove(mergify); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, repoPath, "commit", "-q", "-am", "drop mergify config on another branch")

	elsewhere, err := loop.MergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("hashing off another branch failed, so it is still reading the working tree: %v", err)
	}
	if elsewhere != clean {
		t.Errorf("hash changed on another branch: %q then %q", clean, elsewhere)
	}
}
