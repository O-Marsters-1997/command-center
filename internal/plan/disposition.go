package plan

import (
	"fmt"
	"slices"
)

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

var allOutcomes = []Outcome{OutcomePush, OutcomeFailed, OutcomeCutFailed, OutcomeAsked, OutcomeContinued}

func AllOutcomes() []Outcome { return slices.Clone(allOutcomes) }

func ParseOutcome(s string) (Outcome, error) {
	for _, o := range allOutcomes {
		if o.String() == s {
			return o, nil
		}
	}
	return 0, fmt.Errorf("unknown outcome %q", s)
}

func Disposition(commitsAfterBaseline int) Outcome {
	if commitsAfterBaseline > 0 {
		return OutcomePush
	}
	return OutcomeFailed
}
