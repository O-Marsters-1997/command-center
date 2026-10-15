package tp

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

const LongTimeout = 10 * time.Minute

type RemoveMode int

const (
	RemoveUnknown RemoveMode = iota
	// RemoveMerged skips tp's ancestor check after GitHub squash-merges the branch.
	RemoveMerged
	// RemoveForced skips tp's checks when GitHub's delete-branch-on-merge leaves no ref.
	RemoveForced
)

func (m RemoveMode) flag() (string, error) {
	switch m {
	case RemoveMerged:
		return "--merged", nil
	case RemoveForced:
		return "--force", nil
	case RemoveUnknown:
	}
	return "", fmt.Errorf("unknown remove mode %d", int(m))
}

type Worktrees interface {
	New(ctx context.Context, repoPath, branch, baseRef string) error
	Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error
}

type CLI struct {
	Exec command.Executor
}

func (c CLI) New(ctx context.Context, repoPath, branch, baseRef string) error {
	if _, err := command.OrExec(c.Exec).Output(
		ctx, LongTimeout, repoPath, "tp", "new", branch, "--base", baseRef,
	); err != nil {
		return fmt.Errorf("create worktree %s: %w", branch, err)
	}
	return nil
}

func (c CLI) Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error {
	flag, err := mode.flag()
	if err != nil {
		return fmt.Errorf("resolve remove mode for %s: %w", branch, err)
	}
	if _, err := command.OrExec(c.Exec).Output(
		ctx, command.DefaultTimeout, repoPath, "tp", "remove", flag, branch,
	); err != nil {
		return fmt.Errorf("remove worktree %s: %w", branch, err)
	}
	return nil
}
