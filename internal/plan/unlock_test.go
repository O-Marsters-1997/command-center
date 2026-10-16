package plan_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

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
			if !got.Unlocked || got.BaseBranch != tt.wantBase {
				t.Errorf("Unlocked(%s, %v, stacking %t) = %+v, want unlocked off %q",
					tt.ticket.URL, tt.prs, tt.stacking, got, tt.wantBase)
			}
			if got.Reason == "" {
				t.Errorf("Unlocked(%s, %v).Reason = \"\", want one", tt.ticket.URL, tt.prs)
			}
			if len(got.Blocking) != 0 {
				t.Errorf("Unlocked(%s, %v).Blocking = %v, want none", tt.ticket.URL, tt.prs, got.Blocking)
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
				t.Errorf("Unlocked(%s, %v) = %+v, want blocked without a base", tt.ticket.URL, tt.prs, got)
			}
			if !strings.Contains(string(got.Reason), tt.reasonNames) {
				t.Errorf("Unlocked(%s, %v).Reason = %q, want it to name %q", tt.ticket.URL, tt.prs, got.Reason, tt.reasonNames)
			}
			sorted := cmpopts.SortSlices(func(a, b plan.TicketURL) bool { return a < b })
			if diff := cmp.Diff(tt.wantBlocking, got.Blocking, sorted); diff != "" {
				t.Errorf("Unlocked(%s, %v).Blocking mismatch (-want +got):\n%s", tt.ticket.URL, tt.prs, diff)
			}
		})
	}

	t.Run("blocker closed flag", unlockedBlockerClosed)
	t.Run("an unknown blocker stays blocked", unlockedUnknownBlocker)
}

func unlockedBlockerClosed(t *testing.T) {
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
				t.Errorf("Unlocked(%s, %v).BlockerClosed = %t, want %t", dependent.URL, tt.prs, got, tt.want)
			}
		})
	}
}

func unlockedUnknownBlocker(t *testing.T) {
	t.Parallel()

	ticket := plan.Ticket{URL: "sandbox://CC-9", BlockedBy: []plan.TicketURL{"sandbox://GHOST"}}
	got := plan.Unlocked(ticket, map[plan.TicketURL]plan.Ticket{}, map[string]plan.PRState{}, false)
	if got.Unlocked || !strings.Contains(string(got.Reason), "sandbox://GHOST") {
		t.Errorf("Unlocked(%s, no tickets) = %+v, want blocked naming sandbox://GHOST", ticket.URL, got)
	}
}
