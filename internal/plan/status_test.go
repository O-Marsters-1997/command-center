package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		unlock plan.Unlock
		want   plan.State
	}{
		{
			name:   "locked derives blocked",
			unlock: plan.Unlock{Unlocked: false, Reason: "waiting on sandbox://CC-1"},
			want:   plan.Blocked,
		},
		{
			name:   "unlocked and unauthorised derives ready",
			unlock: plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			want:   plan.Ready,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: tt.unlock})
			if state != tt.want {
				t.Errorf("state = %v, want %v", state, tt.want)
			}
			if reason != tt.unlock.Reason {
				t.Errorf("reason = %q, want the unlock reason %q", reason, tt.unlock.Reason)
			}
		})
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()

	if plan.Blocked.String() != "blocked" || plan.Ready.String() != "ready" {
		t.Errorf("states render as %q and %q", plan.Blocked, plan.Ready)
	}
}
