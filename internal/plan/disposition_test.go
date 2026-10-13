package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestParseOutcomeRoundTripsEveryOutcome(t *testing.T) {
	t.Parallel()

	outcomes := []plan.Outcome{
		plan.OutcomePush, plan.OutcomeFailed, plan.OutcomeCutFailed, plan.OutcomeAsked, plan.OutcomeContinued,
	}
	for _, want := range outcomes {
		got, err := plan.ParseOutcome(want.String())
		if err != nil || got != want {
			t.Errorf("ParseOutcome(%q) = %v, %v, want %v", want.String(), got, err, want)
		}
	}
}

func TestParseOutcomeRejectsAnUnknownString(t *testing.T) {
	t.Parallel()

	if _, err := plan.ParseOutcome("bogus"); err == nil {
		t.Error(`ParseOutcome("bogus") error = nil, want an error`)
	}
}

func TestDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		commits int
		want    plan.Outcome
	}{
		{name: "no commits after baseline derives failed", commits: 0, want: plan.OutcomeFailed},
		{name: "one commit after baseline derives push", commits: 1, want: plan.OutcomePush},
		{name: "several commits after baseline derives push", commits: 4, want: plan.OutcomePush},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.Disposition(tt.commits); got != tt.want {
				t.Errorf("Disposition(%d) = %v, want %v", tt.commits, got, tt.want)
			}
		})
	}
}
