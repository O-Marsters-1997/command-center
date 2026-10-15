package plan

import "slices"

// ReadyToMergeLabel is the label repos wire to a "Merge when ready" rule with no base condition.
const ReadyToMergeLabel = "ready-to-merge"

// StackedReadyToMergeWarning reports a ready-to-merge label on a stacked PR.
// The repo's rule would squash-merge it into the parent with checks unseen.
func StackedReadyToMergeWarning(baseRef string, labels []string) bool {
	if baseRef == "" || baseRef == DefaultBaseBranch {
		return false
	}
	return slices.Contains(labels, ReadyToMergeLabel)
}
