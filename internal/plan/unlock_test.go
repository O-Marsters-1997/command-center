package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestUnlocked(t *testing.T) {
	t.Parallel()

	root := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	dependent := plan.Ticket{
		URL:       "sandbox://CC-2",
		Repo:      "cc-sandbox",
		Branch:    "cc-2-second",
		BlockedBy: []string{"sandbox://CC-1"},
	}
	secondBlocker := plan.Ticket{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third"}
	fanIn := plan.Ticket{
		URL:       "sandbox://CC-4",
		Repo:      "cc-sandbox",
		Branch:    "cc-4-fourth",
		BlockedBy: []string{"sandbox://CC-1", "sandbox://CC-3"},
	}
	crossRepoBlocker := plan.Ticket{URL: "sandbox://PLA-9", Repo: "other-repo", Branch: "pla-9"}
	withCrossRepoEdge := plan.Ticket{
		URL:       "sandbox://CC-5",
		Repo:      "cc-sandbox",
		Branch:    "cc-5-fifth",
		BlockedBy: []string{"sandbox://CC-1", "sandbox://PLA-9"},
	}
	byURL := map[string]plan.Ticket{
		root.URL:              root,
		dependent.URL:         dependent,
		secondBlocker.URL:     secondBlocker,
		fanIn.URL:             fanIn,
		crossRepoBlocker.URL:  crossRepoBlocker,
		withCrossRepoEdge.URL: withCrossRepoEdge,
	}

	tests := []struct {
		name         string
		ticket       plan.Ticket
		prs          map[string]plan.PRState
		stacking     bool
		wantUnlocked bool
		wantBase     string
		wantBlocking []string
		reasonNames  string
	}{
		{
			name:         "no blockers unlocks off main",
			ticket:       root,
			prs:          map[string]plan.PRState{},
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a blocker with no pull request stays blocked",
			ticket:       dependent,
			prs:          map[string]plan.PRState{},
			wantUnlocked: false,
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker whose pull request is closed unmerged stays blocked",
			ticket:       dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Closed},
			wantUnlocked: false,
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "a blocker with an open pull request unlocks off main while stacking is off",
			ticket:       dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking:     false,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a blocker with an open pull request unlocks off the blocker's branch while stacking is on",
			ticket:       dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking:     true,
			wantUnlocked: true,
			wantBase:     "cc-1-first",
		},
		{
			name:         "a merged blocker unlocks off main while stacking is off",
			ticket:       dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking:     false,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "a merged blocker unlocks off main even while stacking is on",
			ticket:       dependent,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking:     true,
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:         "two blockers with one merely open stays blocked",
			ticket:       fanIn,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Open, "cc-3-third": plan.Merged},
			wantUnlocked: false,
			wantBlocking: []string{"sandbox://CC-1"},
			reasonNames:  "sandbox://CC-1",
		},
		{
			name:         "two blockers both merged unlocks off main",
			ticket:       fanIn,
			prs:          map[string]plan.PRState{"cc-1-first": plan.Merged, "cc-3-third": plan.Merged},
			wantUnlocked: true,
			wantBase:     "main",
		},
		{
			name:   "a cross-repo blocker changes neither unlock nor base",
			ticket: withCrossRepoEdge,
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

			got := plan.Unlocked(tt.ticket, byURL, tt.prs, tt.stacking)
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

// TestUnlockedBlockerClosedFlag covers Unlock.BlockerClosed on its own: it must be true only
// when the single blocker's own pull request was closed without merging, never for an absent
// one — the distinction `base gone` needs to tell "never had a PR" apart from "had one, and it
// was closed" (inv. 19).
func TestUnlockedBlockerClosedFlag(t *testing.T) {
	t.Parallel()

	root := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	dependent := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second",
		BlockedBy: []string{"sandbox://CC-1"},
	}
	byURL := map[string]plan.Ticket{root.URL: root, dependent.URL: dependent}

	tests := []struct {
		name string
		prs  map[string]plan.PRState
		want bool
	}{
		{name: "no pull request at all", prs: map[string]plan.PRState{}, want: false},
		{name: "closed without merging",
			prs: map[string]plan.PRState{"cc-1-first": plan.Closed}, want: true},
		{name: "open", prs: map[string]plan.PRState{"cc-1-first": plan.Open}, want: false},
		{name: "merged", prs: map[string]plan.PRState{"cc-1-first": plan.Merged}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.Unlocked(dependent, byURL, tt.prs, false).BlockerClosed
			if got != tt.want {
				t.Errorf("BlockerClosed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnlockedUnknownBlockerStaysBlocked(t *testing.T) {
	t.Parallel()

	ticket := plan.Ticket{URL: "sandbox://CC-9", BlockedBy: []string{"sandbox://GHOST"}}
	got := plan.Unlocked(ticket, map[string]plan.Ticket{}, map[string]plan.PRState{}, false)
	if got.Unlocked {
		t.Error("a blocker with no ticket row unlocked the row; fail closed")
	}
	if !strings.Contains(string(got.Reason), "sandbox://GHOST") {
		t.Errorf("reason %q does not name the unknown blocker", got.Reason)
	}
}
