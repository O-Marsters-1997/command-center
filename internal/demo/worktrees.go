package demo

import (
	"context"
	"fmt"
	"path/filepath"

	ccgit "github.com/O-Marsters-1997/command-center/internal/git"
)

type Worktrees struct{}

func (Worktrees) New(_ context.Context, repoPath, branch, baseRef string) error {
	path := filepath.Join(filepath.Dir(repoPath), "wt-"+branch)
	_, err := git(repoPath, "worktree", "add", "-q", "-b", branch, path, baseRef)
	return err
}

func (Worktrees) Remove(ctx context.Context, repoPath, branch string, _ ccgit.RemoveMode) error {
	worktrees, err := ccgit.WorktreePaths(ctx, repoPath)
	if err != nil {
		return err
	}
	path, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("no worktree for branch %s", branch)
	}
	if _, err := git(repoPath, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, err = git(repoPath, "branch", "-q", "-D", branch)
	return err
}

var _ ccgit.Worktrees = Worktrees{}
