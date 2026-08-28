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
		want       []string
		unattended bool
		tone       string
	}{
		{state: plan.Blocked, want: []string{plan.VerbLaunch}, tone: "wait"},
		{state: plan.Ready, want: []string{plan.VerbLaunch}, tone: "idle"},
		{state: plan.Queued, want: []string{plan.VerbCancel}, unattended: true, tone: "wait"},
		{state: plan.Running, want: []string{plan.VerbKill}, unattended: true, tone: "live"},
		{state: plan.Failed, want: []string{plan.VerbReRun}, tone: "stop"},
		{state: plan.CutFailed, want: []string{plan.VerbReRun}, tone: "stop"},
		{state: plan.PushPending, want: nil, unattended: true, tone: "live"},
		{state: plan.Checking, want: []string{plan.VerbReRun, plan.VerbClosePR}, unattended: true, tone: "live"},
		{
			state: plan.NeedsYou,
			want:  []string{plan.VerbReRun, plan.VerbKill, plan.VerbClosePR},
			tone:  "stop",
		},
		{state: plan.PushFailed, want: []string{plan.VerbRetryPush, plan.VerbReRun}, tone: "stop"},
		{state: plan.ReviewMe, want: []string{plan.VerbClosePR}, tone: "wait"},
		{state: plan.PRMerged, want: []string{plan.VerbRemoveWorktree}, tone: "done"},
		{
			state: plan.PRClosedUnmerged,
			want:  []string{plan.VerbReRun, plan.VerbRemoveWorktree},
			tone:  "stop",
		},
		{state: plan.BaseGone, want: []string{plan.VerbReRun, plan.VerbRemoveWorktree}, tone: "stop"},
		{state: plan.Cancelled, want: []string{plan.VerbLaunch}, tone: "idle"},
		{state: plan.BaseMoved, want: []string{plan.VerbRefresh, plan.VerbReRun}, unattended: true, tone: "live"},
		{state: plan.RefreshConflicted, want: []string{plan.VerbAbort}, tone: "stop"},
		{
			state: plan.ConflictsWithMain,
			want:  []string{plan.VerbRefresh, plan.VerbClosePR},
			tone:  "stop",
		},
		{
			state: plan.VerificationFailed,
			want:  []string{plan.VerbRetryPush, plan.VerbReRun},
			tone:  "stop",
		},
		{
			state: plan.WaitingOnProducerDeploy,
			want:  []string{plan.VerbReCheck, plan.VerbReRun},
			tone:  "wait",
		},
	}

	if len(tests) != plan.StateCount {
		t.Fatalf("table covers %d states, the enum has %d", len(tests), plan.StateCount)
	}

	tones := map[string]bool{}
	for _, tt := range tests {
		tones[tt.tone] = true
	}
	if len(tones) != 5 {
		t.Errorf("table spends %d tones, the stylesheet knows 5: %v", len(tones), tones)
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			t.Parallel()

			got := plan.Verbs(tt.state)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Verbs(%s) = %v, want %v", tt.state, got, tt.want)
			}
			if len(got) > 3 {
				t.Errorf("Verbs(%s) offers %d verbs, the board has room for 3", tt.state, len(got))
			}
			if got := tt.state.Unattended(); got != tt.unattended {
				t.Errorf("%s.Unattended() = %t, want %t", tt.state, got, tt.unattended)
			}
			if got := plan.Tone(tt.state); got != tt.tone {
				t.Errorf("Tone(%s) = %q, want %q", tt.state, got, tt.tone)
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
	if want := []string{plan.VerbRemoveWorktree}; !slices.Equal(plan.Verbs(plan.PRMerged), want) {
		t.Errorf("Verbs(merged) = %v, want %v — the reason merged is attended", plan.Verbs(plan.PRMerged), want)
	}
}
