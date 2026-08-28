package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestProspectiveBase(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	crossRepoBlocker := plan.Ticket{URL: "sandbox://PLA-9", Repo: "other-repo", Branch: "pla-9"}
	byURL := map[string]plan.Ticket{blocker.URL: blocker, crossRepoBlocker.URL: crossRepoBlocker}

	tests := []struct {
		name     string
		ticket   plan.Ticket
		stacking bool
		want     string
	}{
		{
			name:     "no blockers bases on main",
			ticket:   plan.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox"},
			stacking: true,
			want:     "main",
		},
		{
			name: "one same-repo blocker with stacking on bases on the blocker's branch",
			ticket: plan.Ticket{
				URL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://CC-1"},
			},
			stacking: true,
			want:     "cc-1-first",
		},
		{
			name: "one same-repo blocker with stacking off bases on main",
			ticket: plan.Ticket{
				URL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://CC-1"},
			},
			stacking: false,
			want:     "main",
		},
		{
			name: "a cross-repo blocker never becomes the base",
			ticket: plan.Ticket{
				URL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://PLA-9"},
			},
			stacking: true,
			want:     "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.ProspectiveBase(tt.ticket, byURL, tt.stacking)
			if got != tt.want {
				t.Errorf("ProspectiveBase = %q, want %q", got, tt.want)
			}
		})
	}
}
