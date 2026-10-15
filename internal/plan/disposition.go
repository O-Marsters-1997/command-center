package plan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/option"
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

const emptyQuestion = "(the agent asked without writing a question)"

func Asked(kind RunKind, questionFile option.Option[string]) option.Option[string] {
	if kind != RunKindAgent && kind != RunKindFollowUp || questionFile.IsNone() {
		return option.None[string]()
	}
	question := strings.TrimSpace(strings.ToValidUTF8(questionFile.Unwrap(), ""))
	if question == "" {
		return option.Some(emptyQuestion)
	}
	return option.Some(question)
}

func Disposition(commitsAfterBaseline int) Outcome {
	if commitsAfterBaseline > 0 {
		return OutcomePush
	}
	return OutcomeFailed
}
