package plan

import "slices"

// ReadyToMergeLabel is the label both repos wire to a "Merge when ready" rule with no base
// condition (docs/designs/command-centre-design.md § 4a inv. 2). The app never applies it.
const ReadyToMergeLabel = "ready-to-merge"

// StackedReadyToMergeWarning reports invariant 2's hazard: a PR based on anything other than
// main carrying ready-to-merge would squash-merge into its parent branch, checks unseen.
func StackedReadyToMergeWarning(baseRef string, labels []string) bool {
	if baseRef == "" || baseRef == defaultBranch {
		return false
	}
	return slices.Contains(labels, ReadyToMergeLabel)
}
