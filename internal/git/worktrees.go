package git

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/tp"
)

type Worktrees interface {
	New(ctx context.Context, repoPath, branch, baseRef string) error
	Remove(ctx context.Context, repoPath, branch string, mode tp.RemoveMode) error
}

type CLI struct{}

func (CLI) New(ctx context.Context, repoPath, branch, baseRef string) error {
	if err := tp.New(ctx, repoPath, branch, baseRef); err != nil {
		return fmt.Errorf("create worktree %s: %w", branch, err)
	}
	return nil
}

func (CLI) Remove(ctx context.Context, repoPath, branch string, mode tp.RemoveMode) error {
	if err := tp.Remove(ctx, repoPath, branch, mode); err != nil {
		return fmt.Errorf("remove worktree %s: %w", branch, err)
	}
	return nil
}
