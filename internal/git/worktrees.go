package git

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/tp"
)

// Worktrees is every tp call the reconcile loop and its verbs make. CLI is the real one; a test
// substitutes its own.
type Worktrees interface {
	New(ctx context.Context, repoPath, branch, baseRef string) error
	Remove(ctx context.Context, repoPath, branch string, mode tp.RemoveMode) error
}

// CLI is the Worktrees that shells out to the tp binary.
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
