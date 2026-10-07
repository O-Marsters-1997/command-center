package git

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

// Worktrees is every tp call the reconcile loop and its verbs make. CLI is the real one; a test
// substitutes its own.
type Worktrees interface {
	New(ctx context.Context, repoPath, branch, baseRef string) error
	Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error
}

// CLI is the Worktrees that shells out to the tp binary.
type CLI struct{}

// New cuts a worktree for branch off baseRef via `tp new`, run inside repoPath.
func (CLI) New(ctx context.Context, repoPath, branch, baseRef string) error {
	_, err := command.Output(ctx, repoPath, "tp", "new", branch, "--base", baseRef)
	return err
}

// RemoveMode selects which of tp's own removal checks the caller is asking it to run.
type RemoveMode int

const (
	// RemoveMerged skips only the ancestor check a squash merge always fails.
	RemoveMerged RemoveMode = iota
	// RemoveForced skips every check tp performs, for a caller that has already proven them
	// itself once GitHub's delete-branch-on-merge leaves tp with no ref left to check against
	// (docs/adr/0008-cc-proves-what-tp-cannot.md).
	RemoveForced
)

func (m RemoveMode) flag() string {
	if m == RemoveForced {
		return "--force"
	}
	return "--merged"
}

// Remove tears down branch's worktree and deletes the branch via `tp remove`, run inside
// repoPath.
func (CLI) Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error {
	_, err := command.Output(ctx, repoPath, "tp", "remove", mode.flag(), branch)
	return err
}
