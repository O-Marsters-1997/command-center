package plan

import "slices"

// ReadyToMergeLabel is the label both repos wire to a "Merge when ready" rule with no base
// condition. The app never applies it.
const ReadyToMergeLabel = "ready-to-merge"

// StackedReadyToMergeWarning reports whether a PR based on anything other than main carries
// ready-to-merge, which would squash-merge into its parent branch with checks unseen.
func StackedReadyToMergeWarning(baseRef string, labels []string) bool {
	if baseRef == "" || baseRef == defaultBranch {
		return false
	}
	return slices.Contains(labels, ReadyToMergeLabel)
}
