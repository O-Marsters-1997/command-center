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

// Remove tears down branch's worktree and deletes the branch via `tp remove --merged <branch>`,
// run inside repoPath. --merged skips only the ancestor check a squash merge always fails; tp
// still refuses a dirty worktree or unpushed commits itself (issue #147).
func Remove(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "tp", "remove", "--merged", branch)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tp remove --merged %s in %s: %w: %s",
			branch, repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}
