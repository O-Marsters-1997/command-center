package tp

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

const LongTimeout = 10 * time.Minute

// RemoveMode selects which of tp's own removal checks the caller is asking it to run.
type RemoveMode int

const (
	RemoveUnknown RemoveMode = iota
	// RemoveMerged skips only the ancestor check a squash merge always fails.
	RemoveMerged
	// RemoveForced skips every check tp performs, for a caller that has already proven them
	// itself once GitHub's delete-branch-on-merge leaves tp with no ref left to check against.
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

// New cuts a worktree for branch off baseRef via `tp new`, run inside repoPath.
func New(ctx context.Context, repoPath, branch, baseRef string) error {
	if err := command.RunWithin(ctx, LongTimeout, repoPath, "tp", "new", branch, "--base", baseRef); err != nil {
		return fmt.Errorf("tp new %s: %w", branch, err)
	}
	return nil
}

// Remove tears down branch's worktree and deletes the branch via `tp remove`, run inside
// repoPath.
func Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error {
	flag, err := mode.flag()
	if err != nil {
		return fmt.Errorf("tp remove %s: %w", branch, err)
	}
	if err := command.Run(ctx, repoPath, "tp", "remove", flag, branch); err != nil {
		return fmt.Errorf("tp remove %s: %w", branch, err)
	}
	return nil
}
