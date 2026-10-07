package plan

// The verbs a row can offer. These are the wire values the page's forms post and internal/loop's
// handler accepts, so they are spelled exactly as the route's `verb` field, not prettified.
const (
	VerbLaunch           = "launch"
	VerbKill             = "kill"
	VerbReRun            = "re-run"
	VerbReCheck          = "re-check"
	VerbRetryPush        = "retry-push"
	VerbClosePR          = "close-pr"
	VerbRemoveWorktree   = "remove-worktree"
	VerbCancel           = "cancel"
	VerbRefresh          = "refresh"
	VerbAbort            = "abort"
	VerbResolve          = "resolve"
	VerbFollowUp         = "follow-up"
	VerbCommitResolution = "commit-resolution"
)

// IsRowVerb reports whether v is a verb POST /verb accepts against an existing ticket. Launch goes
// through POST /launch's own preview and hash check instead.
func IsRowVerb(v string) bool {
	switch v {
	case VerbKill, VerbReRun, VerbReCheck, VerbRetryPush, VerbClosePR, VerbRemoveWorktree, VerbCancel,
		VerbRefresh, VerbAbort, VerbResolve, VerbFollowUp, VerbCommitResolution:
		return true
	}
	return false
}

// VerdictLabel names a run's verdict for comparison against the last recorded one and as a row's
// BaseVerdict. Empty for a nil fact (no run yet) or when no verdict flag is set, neither of which
// counts as a transition.
func VerdictLabel(fact *RunFact) string {
	if fact == nil {
		return ""
	}
	switch {
	case fact.VerdictBaseMoved:
		return "base_moved"
	case fact.VerdictWaitingOnProducer:
		return "waiting_on_producer_deploy"
	case fact.VerdictReviewMe:
		return "review_me"
	case fact.VerdictCIFailed:
		return "ci_failed"
	case fact.VerdictNeedsYou:
		return "needs_you"
	case fact.VerdictReason != "":
		return "checking"
	default:
		return ""
	}
}

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
		return []string{VerbReRun, VerbFollowUp}
	case ConflictResolved:
		return []string{VerbCommitResolution}
	case Checking:
		return []string{VerbReRun, VerbFollowUp, VerbClosePR}
	case NeedsYou:
		return []string{VerbReRun, VerbFollowUp, VerbKill, VerbClosePR}
	case CIFailed:
		return []string{VerbReRun, VerbFollowUp, VerbClosePR}
	case PushFailed:
		return []string{VerbRetryPush, VerbReRun, VerbFollowUp}
	case ReviewMe:
		return []string{VerbClosePR}
	case PRMerged:
		return []string{VerbRemoveWorktree}
	case PRClosedUnmerged, BaseGone:
		return []string{VerbReRun, VerbFollowUp, VerbRemoveWorktree}
	case BaseMoved:
		return []string{VerbRefresh, VerbReRun, VerbFollowUp}
	case RefreshConflicted:
		return []string{VerbAbort}
	case ConflictsWithMain:
		return []string{VerbResolve, VerbRefresh, VerbClosePR}
	case VerificationFailed:
		return []string{VerbRetryPush, VerbReRun, VerbFollowUp}
	case WaitingOnProducerDeploy:
		return []string{VerbReCheck, VerbReRun, VerbFollowUp}
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
	case Blocked, Queued, ReviewMe, WaitingOnProducerDeploy, ConflictResolved:
		return "wait"
	case Ready, Cancelled:
		return "idle"
	default:
		return "stop"
	}
}
