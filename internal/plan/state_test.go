package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

var stateDecisions = []struct {
	state plan.State
	verbs []plan.Verb
}{
	{state: plan.Blocked, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.Ready, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.Queued, verbs: []plan.Verb{plan.VerbCancel}},
	{state: plan.Running, verbs: []plan.Verb{plan.VerbKill, plan.VerbFollowUp}},
	{state: plan.Failed, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.AwaitingInput, verbs: []plan.Verb{plan.VerbFollowUp, plan.VerbReRun}},
	{state: plan.PushPending},
	{state: plan.AwaitingPR, verbs: []plan.Verb{plan.VerbRaise, plan.VerbFollowUp, plan.VerbReRun}},
	{state: plan.Checking, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.NeedsYou, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbKill, plan.VerbAdoptHead}},
	{state: plan.PushFailed, verbs: []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.ReviewMe, verbs: []plan.Verb{plan.VerbFollowUp}},
	{state: plan.PRMerged, verbs: []plan.Verb{plan.VerbRemoveWorktree}},
	{state: plan.Withdrawn, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbRemoveWorktree}},
	{state: plan.Cancelled, verbs: []plan.Verb{plan.VerbLaunch}},
	{state: plan.BaseMoved, verbs: []plan.Verb{plan.VerbRefresh, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.CIFailed, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbAdoptHead}},
	{state: plan.RefreshConflicted, verbs: []plan.Verb{plan.VerbResolve, plan.VerbAbort}},
	{state: plan.ConflictsWithMain, verbs: []plan.Verb{plan.VerbRefresh}},
	{state: plan.VerificationFailed, verbs: []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.WaitingOnProducerDeploy, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.PushRefused, verbs: []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.Parked, verbs: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
	{state: plan.Verifying},
	{state: plan.WaitingToVerify},
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

func TestState_Label(t *testing.T) {
	t.Parallel()

	want := map[plan.State]string{
		plan.Blocked: "Blocked", plan.Ready: "Ready to launch", plan.Queued: "Queued",
		plan.Running: "Agent running", plan.Failed: "Failed",
		plan.PushPending: "Waiting to push", plan.Checking: "Waiting on checks", plan.NeedsYou: "Needs you",
		plan.PushFailed: "Push failed", plan.PushRefused: "Push refused", plan.AwaitingInput: "Asked you a question",
		plan.ReviewMe: "Ready for review", plan.PRMerged: "Merged", plan.Withdrawn: "PR closed",
		plan.Cancelled: "Cancelled", plan.BaseMoved: "Base moved",
		plan.CIFailed: "Checks failing", plan.RefreshConflicted: "Refresh conflicted",
		plan.ConflictsWithMain: "Conflicts with main", plan.VerificationFailed: "Verification failed",
		plan.WaitingOnProducerDeploy: "Waiting on deploy", plan.AwaitingPR: "Awaiting PR",
		plan.Parked: "Parked", plan.Verifying: "Verifying", plan.WaitingToVerify: "Waiting to verify",
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
