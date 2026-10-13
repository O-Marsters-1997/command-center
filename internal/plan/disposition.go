package plan

import "fmt"

// Outcome is a dead run's disposition, recorded on runs.outcome as data, never inferred from
// missing events.
type Outcome int

const (
	OutcomePush Outcome = iota
	OutcomeFailed
	OutcomeCutFailed
	OutcomeAsked
	OutcomeContinued
)

func (o Outcome) String() string {
	switch o {
	case OutcomePush:
		return "push"
	case OutcomeFailed:
		return "failed"
	case OutcomeCutFailed:
		return "cut_failed"
	case OutcomeAsked:
		return "asked"
	case OutcomeContinued:
		return "continued"
	default:
		return "failed"
	}
}

func ParseOutcome(s string) (Outcome, error) {
	for _, o := range []Outcome{OutcomePush, OutcomeFailed, OutcomeCutFailed, OutcomeAsked, OutcomeContinued} {
		if o.String() == s {
			return o, nil
		}
	}
	return 0, fmt.Errorf("unknown outcome %q", s)
}

// Disposition derives a dead run's outcome from commits after its own baseline_sha: any commit
// at all means the agent left something behind to push.
func Disposition(commitsAfterBaseline int) Outcome {
	if commitsAfterBaseline > 0 {
		return OutcomePush
	}
	return OutcomeFailed
}
