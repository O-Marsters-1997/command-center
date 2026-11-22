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

func TestState_Stage(t *testing.T) {
	t.Parallel()

	want := map[plan.State]plan.Stage{
		plan.Blocked:                 plan.StageBlocked,
		plan.Ready:                   plan.StageReady,
		plan.Queued:                  plan.StageReady,
		plan.Running:                 plan.StageWorking,
		plan.Failed:                  plan.StageWorking,
		plan.PushPending:             plan.StageWorking,
		plan.PushFailed:              plan.StageWorking,
		plan.PushRefused:             plan.StageWorking,
		plan.Verifying:               plan.StageWorking,
		plan.WaitingToVerify:         plan.StageWorking,
		plan.VerificationFailed:      plan.StageWorking,
		plan.AwaitingInput:           plan.StageWorking,
		plan.AwaitingPR:              plan.StagePR,
		plan.Checking:                plan.StagePR,
		plan.CIFailed:                plan.StagePR,
		plan.Parked:                  plan.StagePR,
		plan.BaseMoved:               plan.StagePR,
		plan.ConflictsWithMain:       plan.StagePR,
		plan.RefreshConflicted:       plan.StagePR,
		plan.NeedsYou:                plan.StagePR,
		plan.WaitingOnProducerDeploy: plan.StagePR,
		plan.ReviewMe:                plan.StageReview,
		plan.PRMerged:                plan.StageMerged,
		plan.Withdrawn:               plan.StageNone,
		plan.Cancelled:               plan.StageNone,
	}
	if len(want) != len(plan.AllStates()) {
		t.Fatalf("table covers %d states, AllStates() has %d", len(want), len(plan.AllStates()))
	}
	for _, s := range plan.AllStates() {
		if got := s.Stage(); got != want[s] {
			t.Errorf("%s.Stage() = %q, want %q", s, got, want[s])
		}
	}
	if got := plan.StateUnknown.Stage(); got != plan.StageNone {
		t.Errorf("StateUnknown.Stage() = %q, want none", got)
	}
}

func TestAllStages(t *testing.T) {
	t.Parallel()

	want := []plan.Stage{
		plan.StageBlocked, plan.StageReady, plan.StageWorking, plan.StagePR, plan.StageReview, plan.StageMerged,
	}
	if diff := cmp.Diff(want, plan.AllStages()); diff != "" {
		t.Errorf("AllStages() mismatch (-want +got):\n%s", diff)
	}
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
