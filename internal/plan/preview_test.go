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
		slice          map[plan.TicketURL]bool
		activeLaunchID int64
		conflictedBase string
		wantLabel      plan.PreviewLabel
		reasonNames    string
	}{
		{
			name:      "an unlocked row starts now",
			unlock:    plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			slice:     map[plan.TicketURL]bool{},
			wantLabel: plan.Now,
		},
		{
			name:        "every blocker present in the slice starts on unlock",
			unlock:      plan.Unlock{Blocking: []plan.TicketURL{"sandbox://CC-1"}, Reason: "blocked by sandbox://CC-1"},
			slice:       map[plan.TicketURL]bool{"sandbox://CC-1": true, "sandbox://CC-2": true},
			wantLabel:   plan.OnUnlock,
			reasonNames: "sandbox://CC-1",
		},
		{
			name:        "a blocker outside the slice is refused, naming that blocker",
			unlock:      plan.Unlock{Blocking: []plan.TicketURL{"sandbox://CC-9"}, Reason: "blocked by sandbox://CC-9"},
			slice:       map[plan.TicketURL]bool{"sandbox://CC-2": true},
			wantLabel:   plan.Refused,
			reasonNames: "sandbox://CC-9",
		},
		{
			name:           "a ticket already in an active launch is refused, naming that launch",
			unlock:         plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"},
			slice:          map[plan.TicketURL]bool{},
			activeLaunchID: 3,
			wantLabel:      plan.Refused,
			reasonNames:    "launch 3",
		},
		{
			name:           "a row whose base already carries a conflict is refused, naming that base",
			unlock:         plan.Unlock{Unlocked: true, BaseBranch: "cc-1-first", Reason: "every blocker has a pull request"},
			slice:          map[plan.TicketURL]bool{},
			conflictedBase: "cc-1-first",
			wantLabel:      plan.Refused,
			reasonNames:    "cc-1-first",
		},
		{
			name:        "a refused blocker has no open or merged pull request",
			unlock:      plan.Unlock{Blocking: []plan.TicketURL{"sandbox://CC-9"}},
			slice:       map[plan.TicketURL]bool{},
			wantLabel:   plan.Refused,
			reasonNames: "no open or merged pull request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			label, reason := plan.Preview(tt.unlock, tt.slice, tt.activeLaunchID, tt.conflictedBase)
			if label != tt.wantLabel {
				t.Errorf("Preview(%+v) label = %v, want %v (reason %q)", tt.unlock, label, tt.wantLabel, reason)
			}
			if tt.reasonNames != "" && !strings.Contains(string(reason), tt.reasonNames) {
				t.Errorf("Preview(%+v) reason = %q, want it to name %q", tt.unlock, reason, tt.reasonNames)
			}
		})
	}
}
