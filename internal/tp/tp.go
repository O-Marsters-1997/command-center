// Package tp wraps the treepad CLI commands the app needs: `new` (Phase 3) and `remove`
// (Phase 6 teardown).
package tp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// New cuts a worktree for branch off baseRef via `tp new <branch> --base <baseRef>`, run inside
// repoPath. A failure here is the caller's "cut failed", not a crash.
func New(ctx context.Context, repoPath, branch, baseRef string) error {
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
	// RemoveMerged skips only the ancestor check a squash merge always fails; tp still refuses
	// a dirty worktree or unpushed commits itself (issue #147).
	RemoveMerged RemoveMode = iota
	// RemoveForced skips every check tp performs, for a caller that has already proven them
	// itself once GitHub's delete-branch-on-merge leaves tp with no ref left to check against
	// (docs/adr/0012-cc-proves-what-tp-cannot.md).
	RemoveForced
)

func (m RemoveMode) flag() string {
	if m == RemoveForced {
		return "--force"
	}
	return "--merged"
}

// Remove tears down branch's worktree and deletes the branch via `tp remove <flag> <branch>`,
// run inside repoPath, where mode picks the flag (see RemoveMode).
func Remove(ctx context.Context, repoPath, branch string, mode RemoveMode) error {
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
