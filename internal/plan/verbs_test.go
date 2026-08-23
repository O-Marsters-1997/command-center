package plan_test

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestVerbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state plan.State
		want  []string
	}{
		{state: plan.Blocked, want: []string{plan.VerbLaunch}},
		{state: plan.Ready, want: []string{plan.VerbLaunch}},
		{state: plan.Queued, want: []string{plan.VerbCancel}},
		{state: plan.Running, want: []string{plan.VerbKill}},
		{state: plan.Failed, want: []string{plan.VerbReRun}},
		{state: plan.CutFailed, want: []string{plan.VerbReRun}},
		{state: plan.PushPending, want: nil},
		{state: plan.Checking, want: []string{plan.VerbReRun, plan.VerbClosePR}},
		{state: plan.NeedsYou, want: []string{plan.VerbReRun, plan.VerbKill, plan.VerbClosePR}},
		{state: plan.PushFailed, want: []string{plan.VerbRetryPush, plan.VerbReRun}},
		{state: plan.ReviewMe, want: []string{plan.VerbClosePR}},
		{state: plan.PRMerged, want: []string{plan.VerbRemoveWorktree}},
		{state: plan.PRClosedUnmerged, want: []string{plan.VerbReRun, plan.VerbRemoveWorktree}},
		{state: plan.BaseGone, want: []string{plan.VerbReRun, plan.VerbRemoveWorktree}},
		{state: plan.Cancelled, want: []string{plan.VerbLaunch}},
	}

	// Cancelled is the last state in the enum, so a new one added after it lands here as a
	// missing row rather than as a silently unverbed row on the page.
	if len(tests) != int(plan.Cancelled)+1 {
		t.Fatalf("table covers %d states, the enum has %d", len(tests), int(plan.Cancelled)+1)
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			t.Parallel()

			if got := plan.Verbs(tt.state); !slices.Equal(got, tt.want) {
				t.Errorf("Verbs(%s) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
