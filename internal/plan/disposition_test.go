package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestParseOutcome(t *testing.T) {
	t.Parallel()

	outcomes := []plan.Outcome{
		plan.OutcomePush, plan.OutcomeFailed, plan.OutcomeCutFailed, plan.OutcomeAsked, plan.OutcomeContinued,
	}
	for _, want := range outcomes {
		t.Run(want.String(), func(t *testing.T) {
			t.Parallel()
			got, err := plan.ParseOutcome(want.String())
			if err != nil || got != want {
				t.Errorf("ParseOutcome(%q) = %v, %v, want %v", want.String(), got, err, want)
			}
		})
	}

	t.Run("an unknown string", func(t *testing.T) {
		t.Parallel()
		if _, err := plan.ParseOutcome("bogus"); err == nil {
			t.Error(`ParseOutcome("bogus") error = nil, want one`)
		}
	})
}

func TestAsked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind plan.RunKind
		file option.Option[string]
		want option.Option[string]
	}{
		{name: "no question file", kind: plan.RunKindAgent, file: option.None[string](), want: option.None[string]()},
		{
			name: "agent run asked", kind: plan.RunKindAgent,
			file: option.Some("Which database?"), want: option.Some("Which database?"),
		},
		{name: "follow-up run asked", kind: plan.RunKindFollowUp, file: option.Some("Which?"), want: option.Some("Which?")},
		{
			name: "unknown kind never asks", kind: plan.RunKindUnknown,
			file: option.Some("Which?"), want: option.None[string](),
		},
		{
			name: "surrounding space is trimmed", kind: plan.RunKindAgent,
			file: option.Some("\n  Which database?\n\n"), want: option.Some("Which database?"),
		},
		{
			name: "invalid utf-8 is dropped", kind: plan.RunKindAgent,
			file: option.Some("Which\xff database?"), want: option.Some("Which database?"),
		},
		{
			name: "an empty file still asks", kind: plan.RunKindAgent,
			file: option.Some(" \n"), want: option.Some("(the agent asked without writing a question)"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.Asked(tt.kind, tt.file); got != tt.want {
				t.Errorf("Asked(%q, %v) = %v, want %v", tt.kind, tt.file, got, tt.want)
			}
		})
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
