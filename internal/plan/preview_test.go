package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestPreview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		unlock         plan.Unlock
		slice          map[string]bool
		activeLaunchID int64
		wantLabel      plan.PreviewLabel
		reasonNames    string
	}{
		{
			name:      "an unlocked row starts now",
			unlock:    plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			slice:     map[string]bool{},
			wantLabel: plan.Now,
		},
		{
			name:        "every blocker present in the slice starts on unlock",
			unlock:      plan.Unlock{Blocking: []string{"sandbox://CC-1"}, Reason: "blocked by sandbox://CC-1"},
			slice:       map[string]bool{"sandbox://CC-1": true, "sandbox://CC-2": true},
			wantLabel:   plan.OnUnlock,
			reasonNames: "sandbox://CC-1",
		},
		{
			name:        "a blocker outside the slice is refused, naming that blocker",
			unlock:      plan.Unlock{Blocking: []string{"sandbox://CC-9"}, Reason: "blocked by sandbox://CC-9"},
			slice:       map[string]bool{"sandbox://CC-2": true},
			wantLabel:   plan.Refused,
			reasonNames: "sandbox://CC-9",
		},
		{
			name:           "a ticket already in an active launch is refused, naming that launch",
			unlock:         plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			slice:          map[string]bool{},
			activeLaunchID: 3,
			wantLabel:      plan.Refused,
			reasonNames:    "launch 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			label, reason := plan.Preview(tt.unlock, tt.slice, tt.activeLaunchID)
			if label != tt.wantLabel {
				t.Errorf("label = %v, want %v (reason %q)", label, tt.wantLabel, reason)
			}
			if tt.reasonNames != "" && !strings.Contains(string(reason), tt.reasonNames) {
				t.Errorf("reason %q does not name %q", reason, tt.reasonNames)
			}
		})
	}
}

func TestPreviewRefusedNamesNoOpenOrMergedPullRequest(t *testing.T) {
	t.Parallel()

	_, reason := plan.Preview(
		plan.Unlock{Blocking: []string{"sandbox://CC-9"}},
		map[string]bool{}, 0,
	)
	if !strings.Contains(string(reason), "no open or merged pull request") {
		t.Errorf("reason %q does not say the blocker has no open or merged pull request", reason)
	}
}

func TestPreviewLabelString(t *testing.T) {
	t.Parallel()

	if plan.Now.String() != "now" || plan.OnUnlock.String() != "on unlock" || plan.Refused.String() != "refused" {
		t.Errorf("labels render as %q, %q, %q", plan.Now, plan.OnUnlock, plan.Refused)
	}
}
