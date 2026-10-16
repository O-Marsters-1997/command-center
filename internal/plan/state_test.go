package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestParseState(t *testing.T) {
	t.Parallel()

	for _, want := range plan.AllStates() {
		t.Run(want.String(), func(t *testing.T) {
			t.Parallel()
			got, err := plan.ParseState(want.String())
			if err != nil || got != want {
				t.Errorf("ParseState(%q) = %v, %v, want %v, nil", want.String(), got, err, want)
			}
		})
	}

	t.Run("an unknown name", func(t *testing.T) {
		t.Parallel()
		if _, err := plan.ParseState("no_such_state"); err == nil {
			t.Error("ParseState(no_such_state) error = nil, want one")
		}
	})
}

var stateDecisions = []struct {
	state      plan.State
	verbs      []plan.Verb
	unattended bool
}{
	{state: plan.Blocked, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.Ready, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.Queued, verbs: []plan.Verb{plan.VerbCancel}, unattended: true},
	{state: plan.Running, verbs: []plan.Verb{plan.VerbKill, plan.VerbFollowUp}, unattended: true},
	{state: plan.Failed, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.CutFailed, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.AwaitingInput, verbs: []plan.Verb{plan.VerbFollowUp, plan.VerbReRun}},
	{state: plan.PushPending, unattended: true},
	{state: plan.Checking, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}, unattended: true},
	{state: plan.NeedsYou, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbKill}},
	{state: plan.PushFailed, verbs: []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.ReviewMe},
	{state: plan.PRMerged, verbs: []plan.Verb{plan.VerbRemoveWorktree}},
	{state: plan.PRClosedUnmerged, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbRemoveWorktree}},
	{state: plan.BaseGone, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbRemoveWorktree}},
	{state: plan.Cancelled, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.BaseMoved, verbs: []plan.Verb{plan.VerbRefresh, plan.VerbReRun, plan.VerbFollowUp}, unattended: true},
	{state: plan.CIFailed, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.RefreshConflicted, verbs: []plan.Verb{plan.VerbAbort}},
	{state: plan.ConflictsWithMain, verbs: []plan.Verb{plan.VerbRefresh}},
	{state: plan.VerificationFailed, verbs: []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.WaitingOnProducerDeploy, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
}

func TestVerbs(t *testing.T) {
	t.Parallel()

	if len(stateDecisions) != len(plan.AllStates()) {
		t.Fatalf("table covers %d states, AllStates() has %d", len(stateDecisions), len(plan.AllStates()))
	}
	for _, tt := range stateDecisions {
		t.Run(tt.state.String(), func(t *testing.T) {
			t.Parallel()
			got := plan.Verbs(tt.state)
			if diff := cmp.Diff(tt.verbs, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Verbs(%s) mismatch (-want +got):\n%s", tt.state, diff)
			}
			rowButtons := 0
			for _, v := range got {
				if v != plan.VerbFollowUp {
					rowButtons++
				}
			}
			if rowButtons > 3 {
				t.Errorf("Verbs(%s) = %v, %d row buttons, want at most 3", tt.state, got, rowButtons)
			}
		})
	}
}

func TestState_Unattended(t *testing.T) {
	t.Parallel()

	for _, tt := range stateDecisions {
		t.Run(tt.state.String(), func(t *testing.T) {
			t.Parallel()
			if got := tt.state.Unattended(); got != tt.unattended {
				t.Errorf("%s.Unattended() = %t, want %t", tt.state, got, tt.unattended)
			}
		})
	}
}

func TestState_Label(t *testing.T) {
	t.Parallel()

	want := map[plan.State]string{
		plan.Blocked: "Blocked", plan.Ready: "Ready to launch", plan.Queued: "Queued",
		plan.Running: "Agent running", plan.Failed: "Failed", plan.CutFailed: "Cut failed",
		plan.PushPending: "Waiting to push", plan.Checking: "Waiting on checks", plan.NeedsYou: "Needs you",
		plan.PushFailed: "Push failed", plan.AwaitingInput: "Asked you a question",
		plan.ReviewMe: "Ready for review", plan.PRMerged: "Merged", plan.PRClosedUnmerged: "PR closed",
		plan.BaseGone: "Base gone", plan.Cancelled: "Cancelled", plan.BaseMoved: "Base moved",
		plan.CIFailed: "Checks failing", plan.RefreshConflicted: "Refresh conflicted",
		plan.ConflictsWithMain: "Conflicts with main", plan.VerificationFailed: "Verification failed",
		plan.WaitingOnProducerDeploy: "Waiting on deploy",
	}
	if len(want) != len(plan.AllStates()) {
		t.Fatalf("table covers %d states, AllStates() has %d", len(want), len(plan.AllStates()))
	}
	for state, label := range want {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()
			if got := state.Label(); got != label {
				t.Errorf("%s.Label() = %q, want %q", state, got, label)
			}
		})
	}
}
