package demo

import (
	"context"
	"fmt"
	"path/filepath"

	ccgit "github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

type Worktrees struct{}

func (Worktrees) New(ctx context.Context, repoPath, branch, baseRef string) error {
	path := filepath.Join(filepath.Dir(repoPath), "wt-"+branch)
	_, err := git(ctx, repoPath, "worktree", "add", "-q", "-b", branch, path, baseRef)
	return err
}

func (Worktrees) Remove(ctx context.Context, repoPath, branch string, _ tp.RemoveMode) error {
	worktrees, err := ccgit.WorktreePaths(ctx, repoPath)
	if err != nil {
		return err
	}
	path, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("no worktree for branch %s", branch)
	}
	if _, err := git(ctx, repoPath, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, err = git(ctx, repoPath, "branch", "-q", "-D", branch)
	return err
}

var _ ccgit.Worktrees = Worktrees{}
