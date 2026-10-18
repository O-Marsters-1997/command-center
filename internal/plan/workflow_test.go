package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestWorkflow_Next(t *testing.T) {
	t.Parallel()

	w := plan.Workflow{
		MaxFixRounds: 2,
		Steps: []plan.Step{
			{Name: "setup", Command: true},
			{Name: "implement"},
			{Name: "check", Command: true},
			{Name: "fix", OnFail: true},
			{Name: "pr"},
		},
	}
	run := func(step plan.StepName, o plan.Outcome) plan.StepRun {
		return plan.StepRun{Step: step, Outcome: option.Some(o)}
	}
	passed, failed := plan.OutcomePassed, plan.OutcomeFailed
	live := plan.StepRun{Step: "implement", Alive: true}

	tests := []struct {
		name      string
		pass      []plan.StepRun
		hasVerify bool
		kind      plan.NextKind
		step      plan.StepName
		round     int
		reason    string
	}{
		{name: "empty pass runs the first step", hasVerify: true, kind: plan.NextRun, step: "setup"},
		{
			name: "passed step runs the next", hasVerify: true,
			pass: []plan.StepRun{run("setup", passed)},
			kind: plan.NextRun, step: "implement",
		},
		{
			name: "failed check runs fix", hasVerify: true,
			pass: []plan.StepRun{run("setup", passed), run("implement", passed), run("check", failed)},
			kind: plan.NextRun, step: "fix", round: 1,
		},
		{
			name: "fix re-runs check", hasVerify: true,
			pass: []plan.StepRun{
				run("setup", passed), run("implement", passed), run("check", failed), run("fix", passed),
			},
			kind: plan.NextRun, step: "check", round: 1,
		},
		{
			name: "second failure runs a second fix", hasVerify: true,
			pass: []plan.StepRun{
				run("setup", passed), run("implement", passed),
				run("check", failed), run("fix", passed), run("check", failed),
			},
			kind: plan.NextRun, step: "fix", round: 2,
		},
		{
			name: "third failure parks naming the step", hasVerify: true,
			pass: []plan.StepRun{
				run("setup", passed), run("implement", passed),
				run("check", failed), run("fix", passed), run("check", failed), run("fix", passed), run("check", failed),
			},
			kind: plan.NextPark, reason: "step check",
		},
		{
			name: "a live run means no next step", hasVerify: true,
			pass: []plan.StepRun{run("setup", passed), live},
			kind: plan.NextRun,
		},
		{name: "empty pass skips command steps without a verify command", kind: plan.NextRun, step: "implement"},
		{
			name: "no verify command skips check",
			pass: []plan.StepRun{run("implement", passed)},
			kind: plan.NextRun, step: "pr",
		},
		{
			name: "final step pushing is done", hasVerify: true,
			pass: []plan.StepRun{run("setup", passed), run("pr", plan.OutcomePush)},
			kind: plan.NextDone,
		},
		{
			name: "final step failing parks", hasVerify: true,
			pass: []plan.StepRun{run("setup", passed), run("pr", failed)},
			kind: plan.NextPark, reason: "step pr",
		},
		{
			name: "a pass starts at the latest first step", hasVerify: true,
			pass: []plan.StepRun{
				run("setup", passed), run("implement", passed), run("check", passed), run("pr", failed), run("setup", passed),
			},
			kind: plan.NextRun, step: "implement",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := w.Next(tt.pass, tt.hasVerify)
			if got.Kind != tt.kind {
				t.Fatalf("Next kind = %v, want %v (reason %q)", got.Kind, tt.kind, got.Reason)
			}
			switch {
			case tt.step == "" && got.Step.IsSome():
				t.Errorf("Next step = %q, want none", got.Step.Unwrap().Name)
			case tt.step != "" && (got.Step.IsNone() || got.Step.Unwrap().Name != tt.step):
				t.Errorf("Next step = %v, want %q", got.Step, tt.step)
			}
			if got.Round != tt.round {
				t.Errorf("Next round = %d, want %d", got.Round, tt.round)
			}
			if !strings.Contains(string(got.Reason), tt.reason) {
				t.Errorf("Next reason = %q, want it to contain %q", got.Reason, tt.reason)
			}
		})
	}
}

func TestStep_StepOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step plan.Step
		ev   plan.StepEvidence
		want plan.Outcome
	}{
		{"command exit zero", plan.Step{Command: true}, plan.StepEvidence{ExitCode: option.Some(0)}, plan.OutcomePassed},
		{"command exit one", plan.Step{Command: true}, plan.StepEvidence{ExitCode: option.Some(1)}, plan.OutcomeFailed},
		{"command never exited", plan.Step{Command: true}, plan.StepEvidence{}, plan.OutcomeFailed},
		{
			"writer with handoff", plan.Step{Writes: true},
			plan.StepEvidence{Handoff: option.Some("notes")}, plan.OutcomePassed,
		},
		{
			"writer with blank handoff", plan.Step{Writes: true},
			plan.StepEvidence{Handoff: option.Some(" \n")}, plan.OutcomeFailed,
		},
		{"editor with commits", plan.Step{}, plan.StepEvidence{Commits: 1}, plan.OutcomePassed},
		{"editor without commits", plan.Step{}, plan.StepEvidence{}, plan.OutcomeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.step.StepOutcome(tt.ev); got != tt.want {
				t.Errorf("StepOutcome() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposeStep(t *testing.T) {
	t.Parallel()

	ticket := plan.Ticket{URL: "https://example.test/issues/7"}
	pass := []plan.StepRun{
		{Step: "read", Handoff: option.Some("first notes")},
		{Step: "implement"},
		{Step: "review", Handoff: option.Some("second notes")},
	}
	got := plan.ComposeStep(ticket, plan.Step{Prompt: "Do the thing."}, pass)

	rest := got
	for _, want := range []string{"Do the thing.", string(ticket.URL), "first notes", "second notes"} {
		i := strings.Index(rest, want)
		if i < 0 {
			t.Fatalf("ComposeStep() = %q, want %q in order", got, want)
		}
		rest = rest[i+len(want):]
	}
}
