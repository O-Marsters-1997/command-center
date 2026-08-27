package plan

// The verbs a row can offer. These are the wire values the page's forms post and internal/cc's
// handler accepts, so they are spelled exactly as the route's `verb` field, not prettified.
const (
	VerbLaunch         = "launch"
	VerbKill           = "kill"
	VerbReRun          = "re-run"
	VerbReCheck        = "re-check"
	VerbRetryPush      = "retry-push"
	VerbClosePR        = "close-pr"
	VerbRemoveWorktree = "remove-worktree"
	VerbCancel         = "cancel"
	VerbRefresh        = "refresh"
	VerbAbort          = "abort"
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
	case RefreshConflicted:
		return []string{VerbAbort}
	case WaitingOnProducerDeploy:
		return []string{VerbReCheck, VerbReRun}
	default:
		return nil
	}
}

// Unattended reports whether the loop advances this state without you: a tick job owns its next
// move, so no verb is pending on the row.
func (s State) Unattended() bool {
	switch s {
	case Queued, Running, PushPending, Checking, BaseMoved:
		return true
	default:
		return false
	}
}

// Tone is the state's health band: done, live, wait, stop or idle, never a utility class. live is
// work in flight, wait is parked on a party the reason names, idle is neither.
func Tone(s State) string {
	switch s {
	case PRMerged:
		return "done"
	case Running, PushPending, Checking, BaseMoved:
		return "live"
	case Blocked, Queued, ReviewMe, WaitingOnProducerDeploy:
		return "wait"
	case Ready, Cancelled:
		return "idle"
	default:
		return "stop"
	}
}
