package plan_test

import (
	"slices"
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
		BlockedBy: []plan.TicketURL{"sandbox://CC-1"},
	}
	secondBlocker := plan.Ticket{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third"}
	fanIn := plan.Ticket{
		URL:       "sandbox://CC-4",
		Repo:      "cc-sandbox",
		Branch:    "cc-4-fourth",
		BlockedBy: []plan.TicketURL{"sandbox://CC-1", "sandbox://CC-3"},
	}
	crossRepoBlocker := plan.Ticket{URL: "sandbox://PLA-9", Repo: "other-repo", Branch: "pla-9"}
	withCrossRepoEdge := plan.Ticket{
		URL:       "sandbox://CC-5",
		Repo:      "cc-sandbox",
		Branch:    "cc-5-fifth",
		BlockedBy: []plan.TicketURL{"sandbox://CC-1", "sandbox://PLA-9"},
	}
	byURL := map[plan.TicketURL]plan.Ticket{
		root.URL:              root,
		dependent.URL:         dependent,
		secondBlocker.URL:     secondBlocker,
		fanIn.URL:             fanIn,
		crossRepoBlocker.URL:  crossRepoBlocker,
		withCrossRepoEdge.URL: withCrossRepoEdge,
	}

	unlocked := []struct {
		name     string
		ticket   plan.Ticket
		prs      map[string]plan.PRState
		stacking bool
		wantBase string
	}{
		{
			name:     "no blockers unlocks off main",
			ticket:   root,
			prs:      map[string]plan.PRState{},
			wantBase: "main",
		},
		{
			name:     "a blocker with an open pull request unlocks off main while stacking is off",
			ticket:   dependent,
			prs:      map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking: false,
			wantBase: "main",
		},
		{
			name:     "a blocker with an open pull request unlocks off the blocker's branch while stacking is on",
			ticket:   dependent,
			prs:      map[string]plan.PRState{"cc-1-first": plan.Open},
			stacking: true,
			wantBase: "cc-1-first",
		},
		{
			name:     "a merged blocker unlocks off main while stacking is off",
			ticket:   dependent,
			prs:      map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking: false,
			wantBase: "main",
		},
		{
			name:     "a merged blocker unlocks off main even while stacking is on",
			ticket:   dependent,
			prs:      map[string]plan.PRState{"cc-1-first": plan.Merged},
			stacking: true,
			wantBase: "main",
		},
		{
			name:     "two blockers both merged unlocks off main",
			ticket:   fanIn,
			prs:      map[string]plan.PRState{"cc-1-first": plan.Merged, "cc-3-third": plan.Merged},
			wantBase: "main",
		},
		{
			name:   "a cross-repo blocker changes neither unlock nor base",
			ticket: withCrossRepoEdge,
			prs: map[string]plan.PRState{
				"cc-1-first": plan.Open,
			},
			stacking: true,
			wantBase: "cc-1-first",
		},
	}

	for _, tt := range unlocked {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.Unlocked(tt.ticket, byURL, tt.prs, tt.stacking)
			if !got.Unlocked {
				t.Errorf("unlocked = false, want true (reason %q)", got.Reason)
			}
			if got.BaseBranch != tt.wantBase {
				t.Errorf("base branch = %q, want %q", got.BaseBranch, tt.wantBase)
			}
			if got.Reason == "" {
				t.Error("reason is empty; the page renders it on every row")
			}
			if len(got.Blocking) != 0 {
				t.Errorf("blocking = %v, want empty for an unlocked row", got.Blocking)
			}
		})
	}

	blocked := []struct {
		name         string
		ticket       plan.Ticket
		prs          map[string]plan.PRState
		wantBlocking []plan.TicketURL
		reasonNames  string
	}{
		{"a blocker with no pull request stays blocked", dependent, map[string]plan.PRState{},
			[]plan.TicketURL{"sandbox://CC-1"}, "sandbox://CC-1"},
		{"a blocker whose pull request is closed unmerged stays blocked", dependent,
			map[string]plan.PRState{"cc-1-first": plan.Closed}, []plan.TicketURL{"sandbox://CC-1"}, "sandbox://CC-1"},
		{"two blockers with one merely open stays blocked", fanIn,
			map[string]plan.PRState{"cc-1-first": plan.Open, "cc-3-third": plan.Merged},
			[]plan.TicketURL{"sandbox://CC-1"}, "sandbox://CC-1"},
	}
	for _, tt := range blocked {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plan.Unlocked(tt.ticket, byURL, tt.prs, false)
			if got.Unlocked || got.BaseBranch != "" {
				t.Errorf("Unlocked() = %+v, want blocked without a base", got)
			}
			if !strings.Contains(string(got.Reason), tt.reasonNames) {
				t.Errorf("reason %q does not name %q", got.Reason, tt.reasonNames)
			}
			if !equalUnordered(got.Blocking, tt.wantBlocking) {
				t.Errorf("blocking = %v, want %v", got.Blocking, tt.wantBlocking)
			}
		})
	}
}

func equalUnordered(got, want []plan.TicketURL) bool {
	if len(got) != len(want) {
		return false
	}
	return slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want)))
}

func TestUnlockedBlockerClosedFlag(t *testing.T) {
	t.Parallel()

	root := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	dependent := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second",
		BlockedBy: []plan.TicketURL{"sandbox://CC-1"},
	}
	byURL := map[plan.TicketURL]plan.Ticket{root.URL: root, dependent.URL: dependent}

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

	ticket := plan.Ticket{URL: "sandbox://CC-9", BlockedBy: []plan.TicketURL{"sandbox://GHOST"}}
	got := plan.Unlocked(ticket, map[plan.TicketURL]plan.Ticket{}, map[string]plan.PRState{}, false)
	if got.Unlocked {
		t.Error("a blocker with no ticket row unlocked the row; fail closed")
	}
	if !strings.Contains(string(got.Reason), "sandbox://GHOST") {
		t.Errorf("reason %q does not name the unknown blocker", got.Reason)
	}
}
