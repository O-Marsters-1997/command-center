package plan

// The verbs a row can offer. These are the wire values the page's forms post and internal/cc's
// handler accepts, so they are spelled exactly as the route's `verb` field, not prettified.
const (
	VerbLaunch         = "launch"
	VerbKill           = "kill"
	VerbReRun          = "re-run"
	VerbRetryPush      = "retry-push"
	VerbClosePR        = "close-pr"
	VerbRemoveWorktree = "remove-worktree"
	VerbCancel         = "cancel"
	VerbRefresh        = "refresh"
)

// Verbs is the verbs a row in this state offers, in the order the page renders them
// (docs/prds/prd-command-centre.md § The states).
func Verbs(s State) []string {
	switch s {
	case Ready, Blocked, Cancelled:
		return []string{VerbLaunch}
	case Queued:
		return []string{VerbCancel}
	case Running:
		return []string{VerbKill}
	case Failed, CutFailed:
		return []string{VerbReRun}
	case Checking:
		return []string{VerbReRun, VerbClosePR}
	case NeedsYou:
		return []string{VerbReRun, VerbKill, VerbClosePR}
	case PushFailed:
		return []string{VerbRetryPush, VerbReRun}
	case ReviewMe:
		return []string{VerbClosePR}
	case PRMerged:
		return []string{VerbRemoveWorktree}
	case PRClosedUnmerged, BaseGone:
		return []string{VerbReRun, VerbRemoveWorktree}
	case BaseMoved:
		return []string{VerbRefresh, VerbReRun}
	default:
		return nil
	}
}
