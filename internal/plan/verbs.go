package plan

func Verbs(s State) []Verb {
	switch s {
	case Ready, Blocked, Cancelled:
		return []Verb{VerbLaunch}
	case Queued:
		return []Verb{VerbCancel}
	case Running:
		return []Verb{VerbKill, VerbFollowUp}
	case AwaitingInput:
		return []Verb{VerbFollowUp, VerbReRun}
	case Failed, CutFailed:
		return []Verb{VerbReRun, VerbFollowUp}
	case Checking:
		return []Verb{VerbReRun, VerbFollowUp}
	case NeedsYou:
		return []Verb{VerbReRun, VerbFollowUp, VerbKill}
	case CIFailed:
		return []Verb{VerbReRun, VerbFollowUp}
	case PushFailed:
		return []Verb{VerbRetryPush, VerbReRun, VerbFollowUp}
	case PRMerged:
		return []Verb{VerbRemoveWorktree}
	case PRClosedUnmerged, BaseGone:
		return []Verb{VerbReRun, VerbFollowUp, VerbRemoveWorktree}
	case BaseMoved:
		return []Verb{VerbRefresh, VerbReRun, VerbFollowUp}
	case RefreshConflicted:
		return []Verb{VerbAbort}
	case ConflictsWithMain:
		return []Verb{VerbRefresh}
	case VerificationFailed:
		return []Verb{VerbRetryPush, VerbReRun, VerbFollowUp}
	case WaitingOnProducerDeploy:
		return []Verb{VerbReRun, VerbFollowUp}
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
		WaitingOnProducerDeploy, AwaitingInput:
		return false
	default:
		return false
	}
}
