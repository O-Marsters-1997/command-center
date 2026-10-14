package plan_test

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStateDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state      plan.State
		want       []plan.Verb
		unattended bool
	}{
		{state: plan.Blocked, want: []plan.Verb{plan.VerbLaunch}},
		{state: plan.Ready, want: []plan.Verb{plan.VerbLaunch}},
		{state: plan.Queued, want: []plan.Verb{plan.VerbCancel}, unattended: true},
		{state: plan.Running, want: []plan.Verb{plan.VerbKill, plan.VerbFollowUp}, unattended: true},
		{state: plan.Failed, want: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
		{state: plan.CutFailed, want: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
		{state: plan.AwaitingInput, want: []plan.Verb{plan.VerbFollowUp, plan.VerbReRun}},
		{state: plan.PushPending, want: nil, unattended: true},
		{
			state:      plan.Checking,
			want:       []plan.Verb{plan.VerbReRun, plan.VerbFollowUp},
			unattended: true,
		},
		{
			state: plan.NeedsYou,
			want:  []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbKill},
		},
		{
			state: plan.PushFailed,
			want:  []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp},
		},
		{state: plan.ReviewMe, want: nil},
		{state: plan.PRMerged, want: []plan.Verb{plan.VerbRemoveWorktree}},
		{
			state: plan.PRClosedUnmerged,
			want:  []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbRemoveWorktree},
		},
		{
			state: plan.BaseGone,
			want:  []plan.Verb{plan.VerbReRun, plan.VerbFollowUp, plan.VerbRemoveWorktree},
		},
		{state: plan.Cancelled, want: []plan.Verb{plan.VerbLaunch}},
		{
			state:      plan.BaseMoved,
			want:       []plan.Verb{plan.VerbRefresh, plan.VerbReRun, plan.VerbFollowUp},
			unattended: true,
		},
		{state: plan.CIFailed, want: []plan.Verb{plan.VerbReRun, plan.VerbFollowUp}},
		{state: plan.RefreshConflicted, want: []plan.Verb{plan.VerbAbort}},
		{
			state: plan.ConflictsWithMain,
			want:  []plan.Verb{plan.VerbRefresh},
		},
		{
			state: plan.VerificationFailed,
			want:  []plan.Verb{plan.VerbRetryPush, plan.VerbReRun, plan.VerbFollowUp},
		},
		{
			state: plan.WaitingOnProducerDeploy,
			want:  []plan.Verb{plan.VerbReRun, plan.VerbFollowUp},
		},
	}

	if len(tests) != plan.StateCount {
		t.Fatalf("table covers %d states, the enum has %d", len(tests), plan.StateCount)
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			t.Parallel()

			got := plan.Verbs(tt.state)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Verbs(%s) = %v, want %v", tt.state, got, tt.want)
			}
			rowButtons := 0
			for _, v := range got {
				if v != plan.VerbFollowUp {
					rowButtons++
				}
			}
			if rowButtons > 3 {
				t.Errorf("Verbs(%s) renders %d row buttons, the board has room for 3", tt.state, rowButtons)
			}
			if got := tt.state.Unattended(); got != tt.unattended {
				t.Errorf("%s.Unattended() = %t, want %t", tt.state, got, tt.unattended)
			}
		})
	}
}

func TestBaseMovedIsUnattendedBecauseAutoRefreshSweepsIt(t *testing.T) {
	t.Parallel()

	if !plan.BaseMoved.Unattended() {
		t.Error("base_moved must be unattended: autoRefresh merges the moved base in with no verb pressed")
	}
	if plan.RefreshConflicted.Unattended() {
		t.Error("refresh_conflicted must be attended: autoRefresh's own gate skips a conflicted row until you abort")
	}
}

func TestMergedIsAttendedBecauseOnlyRemoveWorktreeClearsIt(t *testing.T) {
	t.Parallel()

	if plan.PRMerged.Unattended() {
		t.Error("merged must be attended: nothing clears the row but you")
	}
	if want := []plan.Verb{plan.VerbRemoveWorktree}; !slices.Equal(plan.Verbs(plan.PRMerged), want) {
		t.Errorf("Verbs(merged) = %v, want %v — the reason merged is attended", plan.Verbs(plan.PRMerged), want)
	}
}
