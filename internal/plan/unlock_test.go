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
	secondBlocker := plan.Task{TicketURL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third"}
	fanIn := plan.Task{
		TicketURL: "sandbox://CC-4",
		Repo:      "cc-sandbox",
		Branch:    "cc-4-fourth",
		BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-3"},
	}
	crossRepoBlocker := plan.Task{TicketURL: "sandbox://PLA-9", Repo: "other-repo", Branch: "pla-9"}
	withCrossRepoEdge := plan.Task{
		TicketURL: "sandbox://CC-5",
		Repo:      "cc-sandbox",
		Branch:    "cc-5-fifth",
		BlockedBy: []string{"sandbox://CC-1", "sandbox://PLA-9"},
	}
	byURL := map[string]plan.Task{
		root.TicketURL:              root,
		dependent.TicketURL:         dependent,
		secondBlocker.TicketURL:     secondBlocker,
		fanIn.TicketURL:             fanIn,
		crossRepoBlocker.TicketURL:  crossRepoBlocker,
		withCrossRepoEdge.TicketURL: withCrossRepoEdge,
	}

	tests := []struct {
		name         string
		task         plan.Task
		prs          map[string]plan.PRState
		stacking     bool
		wantUnlocked bool
		wantBase     string
		wantBlocking []string
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
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker whose pull request is closed unmerged stays blocked",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Closed},
			wantUnlocked: false,
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker with an open pull request unlocks off main while stacking is off",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking:     false,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a blocker with an open pull request unlocks off the blocker's branch while stacking is on",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking:     true,
			wantUnlocked: true,
			wantBase:     "cc-1-first",
		},
		{
			name:         "a merged blocker unlocks off main while stacking is off",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking:     false,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a merged blocker unlocks off main even while stacking is on",
			task:         dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking:     true,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "two blockers with one merely open stays blocked",
			task:         fanIn,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open, "cc-3-third": plan.Merged},
			wantUnlocked: false,
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "two blockers both merged unlocks off main",
			task:         fanIn,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged, "cc-3-third": plan.Merged},
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name: "a cross-repo blocker changes neither unlock nor base",
			task: withCrossRepoEdge,
			prs: map[string]plan.PRState{
				"cc-1-first": plan.Open,
				// PLA-9 has no PR entry at all: it must not matter, because it is a gating
				// edge, not a stacking edge.
			},
			stacking:     true,
			wantUnlocked: true,
			wantBase:     "cc-1-first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.Unlocked(tt.task, byURL, tt.prs, tt.stacking)
			if got.Unlocked != tt.wantUnlocked {
				t.Errorf("unlocked = %v, want %v (reason %q)", got.Unlocked, tt.wantUnlocked, got.Reason)
			}
			if tt.wantUnlocked && got.BaseBranch != tt.wantBase {
				t.Errorf("base branch = %q, want %q", got.BaseBranch, tt.wantBase)
			}
			if !tt.wantUnlocked && got.BaseBranch != "" {
				t.Errorf("base branch = %q, want empty for a blocked row", got.BaseBranch)
			}
			if got.Reason == "" {
				t.Error("reason is empty; the page renders it on every row")
			}
			if tt.reasonNames != "" && !strings.Contains(string(got.Reason), tt.reasonNames) {
				t.Errorf("reason %q does not name the blocker %q", got.Reason, tt.reasonNames)
			}
			if len(tt.wantBlocking) > 0 && !equalUnordered(got.Blocking, tt.wantBlocking) {
				t.Errorf("blocking = %v, want %v", got.Blocking, tt.wantBlocking)
			}
			if tt.wantUnlocked && len(got.Blocking) != 0 {
				t.Errorf("blocking = %v, want empty for an unlocked row", got.Blocking)
			}
		})
	}
}

func equalUnordered(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

func TestUnlockedUnknownBlockerStaysBlocked(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-9", BlockedBy: []string{"sandbox://GHOST"}}
	got := plan.Unlocked(task, map[string]plan.Task{}, map[string]plan.PRState{}, false)
	if got.Unlocked {
		t.Error("a blocker with no task row unlocked the row; fail closed")
	}
	if !strings.Contains(string(got.Reason), "sandbox://GHOST") {
		t.Errorf("reason %q does not name the unknown blocker", got.Reason)
	}
}
