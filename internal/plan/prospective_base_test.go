package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestProspectiveBase(t *testing.T) {
	t.Parallel()

	blocker := plan.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	crossRepoBlocker := plan.Task{TicketURL: "sandbox://PLA-9", Repo: "other-repo", Branch: "pla-9"}
	byURL := map[string]plan.Task{blocker.TicketURL: blocker, crossRepoBlocker.TicketURL: crossRepoBlocker}

	tests := []struct {
		name     string
		task     plan.Task
		stacking bool
		want     string
	}{
		{
			name:     "no blockers bases on main",
			task:     plan.Task{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox"},
			stacking: true,
			want:     "main",
		},
		{
			name: "one same-repo blocker with stacking on bases on the blocker's branch",
			task: plan.Task{
				TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://CC-1"},
			},
			stacking: true,
			want:     "cc-1-first",
		},
		{
			name: "one same-repo blocker with stacking off bases on main",
			task: plan.Task{
				TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://CC-1"},
			},
			stacking: false,
			want:     "main",
		},
		{
			name: "a cross-repo blocker never becomes the base",
			task: plan.Task{
				TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", BlockedBy: []string{"sandbox://PLA-9"},
			},
			stacking: true,
			want:     "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.ProspectiveBase(tt.task, byURL, tt.stacking)
			if got != tt.want {
				t.Errorf("ProspectiveBase = %q, want %q", got, tt.want)
			}
		})
	}
}
