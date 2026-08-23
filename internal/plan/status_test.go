package plan_test

import (
	"strings"
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

func TestStatusWithLatestRun(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}

	tests := []struct {
		name       string
		latestRun  *plan.RunFact
		wantState  plan.State
		reasonHas  string
		reasonHasA string // second required substring, for the failed-with-log-path case
	}{
		{
			name:      "an alive run derives running regardless of unlock or authorisation",
			latestRun: &plan.RunFact{Alive: true},
			wantState: plan.Running,
		},
		{
			name: "a dead run with no commits derives failed, naming the log path",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomeFailed, LogPath: "/state/runs/7.jsonl",
			},
			wantState:  plan.Failed,
			reasonHas:  "no commits",
			reasonHasA: "/state/runs/7.jsonl",
		},
		{
			name: "a dead run with commits derives push pending",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomePush, LogPath: "/state/runs/8.jsonl",
			},
			wantState: plan.PushPending,
		},
		{
			name: "a failed cut derives cut failed",
			latestRun: &plan.RunFact{
				Alive: false, HasOutcome: true, Outcome: plan.OutcomeCutFailed,
			},
			wantState: plan.CutFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, reason := plan.Status(plan.Facts{Unlock: unlocked, Authorised: false, LatestRun: tt.latestRun})
			if state != tt.wantState {
				t.Errorf("state = %v, want %v (reason %q)", state, tt.wantState, reason)
			}
			if tt.reasonHas != "" && !strings.Contains(string(reason), tt.reasonHas) {
				t.Errorf("reason %q does not contain %q", reason, tt.reasonHas)
			}
			if tt.reasonHasA != "" && !strings.Contains(string(reason), tt.reasonHasA) {
				t.Errorf("reason %q does not contain the log path %q", reason, tt.reasonHasA)
			}
		})
	}
}

func TestStatusIgnoresANilLatestRun(t *testing.T) {
	t.Parallel()

	// The pre-Phase-3 2x2 stays reachable and untouched when there is no run yet.
	state, reason := plan.Status(plan.Facts{
		Unlock:     plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
		Authorised: false,
	})
	if state != plan.Ready || reason != "no blockers" {
		t.Errorf("state = %v, reason = %q, want ready/\"no blockers\"", state, reason)
	}
}
