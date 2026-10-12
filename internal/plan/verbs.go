package plan

const (
	VerbLaunch           = "launch"
	VerbKill             = "kill"
	VerbReRun            = "re-run"
	VerbRetryPush        = "retry-push"
	VerbRemoveWorktree   = "remove-worktree"
	VerbCancel           = "cancel"
	VerbRefresh          = "refresh"
	VerbAbort            = "abort"
	VerbResolve          = "resolve"
	VerbFollowUp         = "follow-up"
	VerbCommitResolution = "commit-resolution"
)

func IsRowVerb(v string) bool {
	switch v {
	case VerbKill, VerbReRun, VerbRetryPush, VerbRemoveWorktree, VerbCancel,
		VerbRefresh, VerbAbort, VerbResolve, VerbFollowUp, VerbCommitResolution:
		return true
	}
	return false
}

func Verbs(s State) []string {
	switch s {
	case Ready, Blocked, Cancelled:
		return []string{VerbLaunch}
	case Queued:
		return []string{VerbCancel}
	case Running:
		return []string{VerbKill, VerbFollowUp}
	case AwaitingInput:
		return []string{VerbFollowUp, VerbReRun}
	case Failed, CutFailed:
		return []string{VerbReRun, VerbFollowUp}
	case ConflictResolved:
		return []string{VerbCommitResolution}
	case Checking:
		return []string{VerbReRun, VerbFollowUp}
	case NeedsYou:
		return []string{VerbReRun, VerbFollowUp, VerbKill}
	case CIFailed:
		return []string{VerbReRun, VerbFollowUp}
	case PushFailed:
		return []string{VerbRetryPush, VerbReRun, VerbFollowUp}
	case PRMerged:
		return []string{VerbRemoveWorktree}
	case PRClosedUnmerged, BaseGone:
		return []string{VerbReRun, VerbFollowUp, VerbRemoveWorktree}
	case BaseMoved:
		return []string{VerbRefresh, VerbReRun, VerbFollowUp}
	case RefreshConflicted:
		return []string{VerbAbort}
	case ConflictsWithMain:
		return []string{VerbResolve, VerbRefresh}
	case VerificationFailed:
		return []string{VerbRetryPush, VerbReRun, VerbFollowUp}
	case WaitingOnProducerDeploy:
		return []string{VerbReRun, VerbFollowUp}
	case PushPending, ReviewMe:
		return nil
	default:
		return nil
	}
}

func (s State) Unattended() bool {
	switch s {
	case Queued, Running, PushPending, Checking, BaseMoved:
		return true
	case Blocked, Ready, Failed, CutFailed, NeedsYou, PushFailed, ReviewMe, PRMerged, PRClosedUnmerged,
		BaseGone, Cancelled, CIFailed, RefreshConflicted, ConflictsWithMain, VerificationFailed,
		WaitingOnProducerDeploy, ConflictResolved, AwaitingInput:
		return false
	default:
		return false
	}
}
