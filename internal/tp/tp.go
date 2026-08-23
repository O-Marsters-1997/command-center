// Package tp wraps the treepad CLI commands the app needs. Phase 3 needs only `new`; `remove`
// is out of scope until teardown.
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
