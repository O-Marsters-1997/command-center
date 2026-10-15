package tptest

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

type Worktrees struct {
	mu      sync.Mutex
	cut     []string
	removed []string
}

var _ tp.Worktrees = (*Worktrees)(nil)

func (w *Worktrees) New(ctx context.Context, repoPath, branch, baseRef string) error {
	path := filepath.Join(filepath.Dir(repoPath), "wt-"+branch)
	if err := command.Run(ctx, repoPath, "git", "worktree", "add", "-q", "-b", branch, path, baseRef); err != nil {
		return fmt.Errorf("create worktree %s: %w", branch, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cut = append(w.cut, branch)
	return nil
}

func (w *Worktrees) Remove(ctx context.Context, repoPath, branch string, mode tp.RemoveMode) error {
	if mode != tp.RemoveMerged && mode != tp.RemoveForced {
		return fmt.Errorf("remove worktree %s: unknown remove mode %d", branch, int(mode))
	}
	paths, err := git.WorktreePaths(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("remove worktree %s: %w", branch, err)
	}
	path, ok := paths[branch]
	if !ok {
		return fmt.Errorf("remove worktree %s: no worktree", branch)
	}
	if err := command.Run(ctx, repoPath, "git", "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("remove worktree %s: %w", branch, err)
	}
	if err := command.Run(ctx, repoPath, "git", "branch", "-q", "-D", branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed = append(w.removed, branch)
	return nil
}

func (w *Worktrees) Cut() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.cut)
}

func (w *Worktrees) Removed() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.removed)
}

func RunContract(t *testing.T, newWorktrees func(t *testing.T) tp.Worktrees) {
	t.Helper()

	hasWorktree := func(t *testing.T, repo, branch string) bool {
		t.Helper()
		paths, err := git.WorktreePaths(t.Context(), repo)
		if err != nil {
			t.Fatalf("WorktreePaths(%s): %v", repo, err)
		}
		_, ok := paths[branch]
		return ok
	}

	t.Run("New cuts a worktree on a new branch", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)

		if err := newWorktrees(t).New(t.Context(), repo, "cc-1-first", "main"); err != nil {
			t.Fatalf("New(cc-1-first) error = %v", err)
		}
		if !hasWorktree(t, repo, "cc-1-first") {
			t.Errorf("New(cc-1-first) left no worktree on cc-1-first")
		}
	})

	for _, mode := range []tp.RemoveMode{tp.RemoveMerged, tp.RemoveForced} {
		t.Run(fmt.Sprintf("Remove mode %d takes the worktree and branch away", mode), func(t *testing.T) {
			t.Parallel()
			repo := gittest.Init(t)
			worktrees := newWorktrees(t)
			if err := worktrees.New(t.Context(), repo, "cc-1-first", "main"); err != nil {
				t.Fatalf("New(cc-1-first) error = %v", err)
			}

			if err := worktrees.Remove(t.Context(), repo, "cc-1-first", mode); err != nil {
				t.Fatalf("Remove(cc-1-first) error = %v", err)
			}
			if hasWorktree(t, repo, "cc-1-first") || gittest.HasRef(t, repo, "refs/heads/cc-1-first") {
				t.Errorf("Remove(cc-1-first) left its worktree or branch behind")
			}
		})
	}

	t.Run("Remove with an unknown mode is an error", func(t *testing.T) {
		t.Parallel()
		repo := gittest.Init(t)
		worktrees := newWorktrees(t)
		if err := worktrees.New(t.Context(), repo, "cc-1-first", "main"); err != nil {
			t.Fatalf("New(cc-1-first) error = %v", err)
		}

		if err := worktrees.Remove(t.Context(), repo, "cc-1-first", tp.RemoveUnknown); err == nil {
			t.Error("Remove(RemoveUnknown) error = nil, want one")
		}
	})
}
