package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/treepad"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
)

func cutBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	if err := (git.Library{}).New(t.Context(), repo, branch, "main"); err != nil {
		t.Fatalf("New(%s) error = %v", branch, err)
	}
	paths, err := git.WorktreePaths(t.Context(), repo)
	if err != nil {
		t.Fatalf("WorktreePaths(%s): %v", repo, err)
	}
	path, ok := paths[branch]
	if !ok {
		t.Fatalf("New(%s) left no worktree on the branch", branch)
	}
	return path
}

func TestLibrary_Remove(t *testing.T) {
	t.Parallel()

	t.Run("takes the worktree and branch away", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)
		cutBranch(t, repo, "cc-1-first")

		if err := (git.Library{}).Remove(t.Context(), repo, "cc-1-first"); err != nil {
			t.Fatalf("Remove(cc-1-first) error = %v", err)
		}
		paths, err := git.WorktreePaths(t.Context(), repo)
		if err != nil {
			t.Fatalf("WorktreePaths(%s): %v", repo, err)
		}
		if _, ok := paths["cc-1-first"]; ok || gittest.HasRef(t, repo, "refs/heads/cc-1-first") {
			t.Errorf("Remove(cc-1-first) left its worktree or branch behind")
		}
	})

	t.Run("removes a branch that is not merged", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)
		path := cutBranch(t, repo, "cc-1-first")
		gittest.Commit(t, path, "agent.txt", "squash-merged elsewhere\n")

		if err := (git.Library{}).Remove(t.Context(), repo, "cc-1-first"); err != nil {
			t.Fatalf("Remove(unmerged cc-1-first) error = %v", err)
		}
	})

	t.Run("a missing worktree counts as removed", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)

		if err := (git.Library{}).Remove(t.Context(), repo, "cc-9-never-cut"); err != nil {
			t.Errorf("Remove(cc-9-never-cut) error = %v, want nil", err)
		}
	})

	t.Run("removes a branch cut from origin and pushed with Push", func(t *testing.T) {
		t.Parallel()
		repo, _ := gittest.WithOrigin(t)
		if err := (git.Library{}).New(t.Context(), repo, "cc-1-first", "origin/main"); err != nil {
			t.Fatalf("New(cc-1-first) error = %v", err)
		}
		paths, err := git.WorktreePaths(t.Context(), repo)
		if err != nil {
			t.Fatalf("WorktreePaths(%s): %v", repo, err)
		}
		gittest.Commit(t, paths["cc-1-first"], "agent.txt", "work\n")
		if err := git.Push(t.Context(), repo, "cc-1-first"); err != nil {
			t.Fatalf("Push(cc-1-first) error = %v", err)
		}

		if err := (git.Library{}).Remove(t.Context(), repo, "cc-1-first"); err != nil {
			t.Errorf("Remove(pushed cc-1-first) error = %v, want nil", err)
		}
	})

	t.Run("a dirty worktree is refused and kept", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)
		path := cutBranch(t, repo, "cc-1-first")
		if err := os.WriteFile(filepath.Join(path, "scratch.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(scratch.txt): %v", err)
		}

		err := (git.Library{}).Remove(t.Context(), repo, "cc-1-first")
		if !errors.Is(err, treepad.ErrDirty) {
			t.Errorf("Remove(dirty) error = %v, want treepad.ErrDirty", err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("Remove(dirty) took the worktree away: %v", statErr)
		}
	})
}
