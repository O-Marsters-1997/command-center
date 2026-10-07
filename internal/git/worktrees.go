package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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
	cmd := exec.CommandContext(ctx, "tp", "new", branch, "--base", baseRef)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tp new %s --base %s in %s: %w: %s",
			branch, baseRef, repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
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
	flag := mode.flag()
	cmd := exec.CommandContext(ctx, "tp", "remove", flag, branch)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tp remove %s %s in %s: %w: %s",
			flag, branch, repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}
