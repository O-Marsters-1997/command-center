package loop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestMergifyHashReadsTheRefNotTheWorkingTree(t *testing.T) {
	_, repoPath := repoWithOrigin(t)

	mergify := filepath.Join(repoPath, ".mergify.yml")
	if err := os.WriteFile(mergify, []byte("pull_request_rules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "add", ".mergify.yml")
	runGit(t, "-C", repoPath, "commit", "-q", "-m", "add mergify config")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin")

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

	runGit(t, "-C", repoPath, "checkout", "-q", "-b", "elsewhere")
	runGit(t, "-C", repoPath, "checkout", "-q", "--", ".mergify.yml")
	if err := os.Remove(mergify); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "commit", "-q", "-am", "drop mergify config on another branch")

	elsewhere, err := loop.MergifyHash(t.Context(), repoPath)
	if err != nil {
		t.Fatalf("hashing off another branch failed, so it is still reading the working tree: %v", err)
	}
	if elsewhere != clean {
		t.Errorf("hash changed on another branch: %q then %q", clean, elsewhere)
	}
}

func TestRecordMergeStateReadsUnmergedAndStagedPaths(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	conflictedWorktree(t, repoPath, worktreePath, "shared.txt")
	const key = "repo//cc-1"

	obs := plan.Observation{UnmergedPaths: map[string][]string{}, HasStaged: map[string]bool{}}
	if err := loop.RecordMergeState(t.Context(), &obs, key, worktreePath); err != nil {
		t.Fatal(err)
	}
	if got := obs.UnmergedPaths[key]; len(got) != 1 || got[0] != "shared.txt" {
		t.Errorf("unmerged = %v, want [shared.txt]", got)
	}

	resolveAndStage(t, worktreePath, "shared.txt", "resolved\n")
	if err := loop.RecordMergeState(t.Context(), &obs, key, worktreePath); err != nil {
		t.Fatal(err)
	}
	if got := obs.UnmergedPaths[key]; len(got) != 0 {
		t.Errorf("unmerged = %v, want none once staged", got)
	}
	if !obs.HasStaged[key] {
		t.Error("HasStaged = false, want true once the resolution is staged")
	}
}
