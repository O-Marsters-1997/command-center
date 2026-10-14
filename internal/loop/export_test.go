package loop

import (
	"context"
)

// MergifyHash is the observe phase's read of .mergify.yml off origin's default branch.
func MergifyHash(ctx context.Context, repoPath string) (string, error) {
	return mergifyHash(ctx, repoPath)
}
