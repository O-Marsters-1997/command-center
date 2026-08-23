package plan

// Outcome is a dead run's disposition, recorded on runs.outcome as data — never inferred from
// missing events (inv. 7). Its members are prefixed (OutcomePush, not Push) because State's own
// Running/Failed/CutFailed/PushPending would otherwise collide with these names in the same
// package.
type Outcome int

const (
	OutcomePush Outcome = iota
	OutcomeFailed
	OutcomeCutFailed
)

func (o Outcome) String() string {
	switch o {
	case OutcomePush:
		return "push"
	case OutcomeFailed:
		return "failed"
	case OutcomeCutFailed:
		return "cut_failed"
	default:
		return "failed"
	}
}

// Disposition derives a dead run's outcome from commits after its own baseline_sha (docs/prd-
// command-centre.md § A run): any commit at all means the agent left something behind to push.
func Disposition(commitsAfterBaseline int) Outcome {
	if commitsAfterBaseline > 0 {
		return OutcomePush
	}
	return OutcomeFailed
}
