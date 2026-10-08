package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// MergifyHash is the observe phase's read of .mergify.yml off origin's default branch.
func MergifyHash(ctx context.Context, repoPath string) (string, error) {
	return mergifyHash(ctx, repoPath)
}

// MainTipKey names the default branch's own tip in Observation.BranchTips.
func MainTipKey(repo string) string { return plan.BranchKey(repo, plan.DefaultBaseBranch) }

// RecordMergeState is the observe phase's read of a mid-merge worktree's unmerged and staged paths.
func RecordMergeState(ctx context.Context, obs *plan.Observation, key, worktreePath string) error {
	return recordMergeState(ctx, obs, key, worktreePath)
}
