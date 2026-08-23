package plan

// The verbs a row can offer. These are the wire values the page's forms post and internal/cc's
// handler accepts, so they are spelled exactly as the route's `verb` field, not prettified.
// `cancel` is absent because it is Phase 2 (plans/command-centre-phase-1.md § Deliberately
// unreachable in Phase 1), and `refresh` because stacking = false makes `base moved` unreachable.
const (
	VerbLaunch         = "launch"
	VerbKill           = "kill"
	VerbReRun          = "re-run"
	VerbRetryPush      = "retry-push"
	VerbClosePR        = "close-pr"
	VerbRemoveWorktree = "remove-worktree"
)

// Verbs is the verbs a row in this state offers, in the order the page renders them
// (docs/prd-command-centre.md § The states). Two states offer none: `queued`, whose only verb
// is Phase 2's `cancel`, and `push_pending`, where the push being waited on is the next tick's
// own work — re-run would race it and there is no live process to kill.
func Verbs(s State) []string {
	switch s {
	case Ready, Blocked:
		return []string{VerbLaunch}
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
	default:
		return nil
	}
}
