package loop

import (
	"context"
)

// MergifyHash is the observe phase's read of .mergify.yml off origin's default branch.
func MergifyHash(ctx context.Context, repoPath string) (string, error) {
	return mergifyHash(ctx, repoPath)
}

// BranchKey names one branch in every branch-keyed map on Observation, letting cc_test build
// fixtures against the same key production code reads.
func BranchKey(repo, branch string) string { return branchKey(repo, branch) }

// MainTipKey names defaultBaseBranch's own tip in Observation.BranchTips.
func MainTipKey(repo string) string { return mainTipKey(repo) }
