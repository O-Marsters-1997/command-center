package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		unlock     plan.Unlock
		authorised bool
		want       plan.State
		wantReason plan.Reason
	}{
		{
			name:       "locked and unauthorised derives blocked",
			unlock:     plan.Unlock{Unlocked: false, Reason: "waiting on sandbox://CC-1"},
			want:       plan.Blocked,
			wantReason: "waiting on sandbox://CC-1",
		},
		{
			name:       "unlocked and unauthorised derives ready",
			unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			want:       plan.Ready,
			wantReason: "no blockers",
		},
		{
			name:       "unlocked and authorised derives queued waiting for a slot",
			unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			authorised: true,
			want:       plan.Queued,
			wantReason: "waiting for a slot",
		},
		{
			name:       "locked and authorised derives queued naming the blocker",
			unlock:     plan.Unlock{Reason: "blocked by sandbox://CC-1", Blocking: []string{"sandbox://CC-1"}},
			authorised: true,
			want:       plan.Queued,
			wantReason: "waiting on sandbox://CC-1's PR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: tt.unlock, Authorised: tt.authorised})
			if state != tt.want {
				t.Errorf("state = %v, want %v", state, tt.want)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()

	if plan.Blocked.String() != "blocked" || plan.Ready.String() != "ready" || plan.Queued.String() != "queued" {
		t.Errorf("states render as %q, %q and %q", plan.Blocked, plan.Ready, plan.Queued)
	}
}
