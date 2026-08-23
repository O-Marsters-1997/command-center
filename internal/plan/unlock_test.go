package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestUnlocked(t *testing.T) {
	t.Parallel()

	root := plan.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	dependent := plan.Task{
		TicketURL: "sandbox://CC-2",
		Repo:      "cc-sandbox",
		Branch:    "cc-2-second",
		BlockedBy: []string{"sandbox://CC-1"},
	}
	byURL := map[string]plan.Task{root.TicketURL: root, dependent.TicketURL: dependent}

	tests := []struct {
		name         string
		task         plan.Task
		prs          map[string]plan.PRState
		wantUnlocked bool
		wantBase     string
		reasonNames  string
	}{
		{
			name:         "no blockers unlocks off main",
			task:         root,
			prs:          map[string]plan.PRState{},
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a blocker with no pull request stays blocked",
			task:         dependent,
			prs:          map[string]plan.PRState{},
			wantUnlocked: false,
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker whose pull request is closed unmerged stays blocked",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Closed},
			wantUnlocked: false,
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker with an open pull request unlocks off main while stacking is off",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open},
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a merged blocker unlocks off main",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged},
			wantUnlocked: true,
			wantBase:     "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.Unlocked(tt.task, byURL, tt.prs)
			if got.Unlocked != tt.wantUnlocked {
				t.Errorf("unlocked = %v, want %v (reason %q)", got.Unlocked, tt.wantUnlocked, got.Reason)
			}
			if tt.wantUnlocked && got.BaseBranch != tt.wantBase {
				t.Errorf("base branch = %q, want %q", got.BaseBranch, tt.wantBase)
			}
			if got.Reason == "" {
				t.Error("reason is empty; the page renders it on every row")
			}
			if tt.reasonNames != "" && !strings.Contains(string(got.Reason), tt.reasonNames) {
				t.Errorf("reason %q does not name the blocker %q", got.Reason, tt.reasonNames)
			}
		})
	}
}

func TestUnlockedUnknownBlockerStaysBlocked(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-9", BlockedBy: []string{"sandbox://GHOST"}}
	got := plan.Unlocked(task, map[string]plan.Task{}, map[string]plan.PRState{})
	if got.Unlocked {
		t.Error("a blocker with no task row unlocked the row; fail closed")
	}
	if !strings.Contains(string(got.Reason), "sandbox://GHOST") {
		t.Errorf("reason %q does not name the unknown blocker", got.Reason)
	}
}
