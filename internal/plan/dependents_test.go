package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestUnlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tickets []plan.Ticket
		want    map[plan.TicketURL][]plan.TicketURL
	}{
		{
			name: "a chain",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "b", BlockedBy: []plan.TicketURL{"a"}},
				{URL: "c", BlockedBy: []plan.TicketURL{"b"}},
			},
			want: map[plan.TicketURL][]plan.TicketURL{"a": {"b"}, "b": {"c"}},
		},
		{
			name: "fan out keeps input order",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "c", BlockedBy: []plan.TicketURL{"a"}},
				{URL: "b", BlockedBy: []plan.TicketURL{"a"}},
			},
			want: map[plan.TicketURL][]plan.TicketURL{"a": {"c", "b"}},
		},
		{
			name: "fan in lists the waiter under each blocker once",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "b"},
				{URL: "c", BlockedBy: []plan.TicketURL{"a", "b", "a"}},
			},
			want: map[plan.TicketURL][]plan.TicketURL{"a": {"c"}, "b": {"c"}},
		},
		{
			name:    "an untracked blocker still lists its waiter",
			tickets: []plan.Ticket{{URL: "b", BlockedBy: []plan.TicketURL{"gone"}}},
			want:    map[plan.TicketURL][]plan.TicketURL{"gone": {"b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, plan.Unlocks(tt.tickets)); diff != "" {
				t.Errorf("Unlocks(%v) mismatch (-want +got):\n%s", tt.tickets, diff)
			}
		})
	}
}
