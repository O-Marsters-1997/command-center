package cc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Fetch updates a repo's remote-tracking refs. --prune because both repos delete branches on
// merge, and a stale ref would make a deleted base look cuttable.
func Fetch(ctx context.Context, repoPath string) error {
	if _, err := git(ctx, repoPath, "fetch", "origin", "--prune"); err != nil {
		return err
	}
	return nil
}

// Worktrees reads the authoritative branch -> path map (inv. 5: never a reimplemented
// derivation of where treepad puts things).
func Worktrees(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := git(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out []byte) map[string]string {
	worktrees := map[string]string{}
	var path string
	for line := range strings.Lines(string(out)) {
		field, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch field {
		case "worktree":
			path = value
		case "branch":
			worktrees[strings.TrimPrefix(value, "refs/heads/")] = path
		}
	}
	return worktrees
}

func git(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %w: %s",
			strings.Join(args, " "), repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}
