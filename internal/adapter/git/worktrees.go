package git

import (
	"context"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/treepad"
)

type Library struct{}

func (Library) New(ctx context.Context, repoPath, branch, baseRef string) error {
	if _, err := treepad.New(ctx, treepad.NewOptions{Branch: branch, Base: baseRef, RepoDir: repoPath}); err != nil {
		return fmt.Errorf("create worktree %s: %w", branch, err)
	}
	return nil
}

func (Library) Remove(ctx context.Context, repoPath, branch string) error {
	err := treepad.Remove(ctx, treepad.RemoveOptions{Branch: branch, RepoDir: repoPath, Force: true})
	if err != nil && !errors.Is(err, treepad.ErrNotFound) {
		return fmt.Errorf("remove worktree %s: %w", branch, err)
	}
	return nil
}
