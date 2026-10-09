package plan_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestUnlocksInvertsBlockedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tickets []plan.Ticket
		want    map[string][]string
	}{
		{name: "no tickets", want: map[string][]string{}},
		{
			name:    "a ticket nothing waits on unlocks nothing",
			tickets: []plan.Ticket{{URL: "a"}},
			want:    map[string][]string{},
		},
		{
			name: "a chain",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "b", BlockedBy: []string{"a"}},
				{URL: "c", BlockedBy: []string{"b"}},
			},
			want: map[string][]string{"a": {"b"}, "b": {"c"}},
		},
		{
			name: "fan out keeps input order",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "c", BlockedBy: []string{"a"}},
				{URL: "b", BlockedBy: []string{"a"}},
			},
			want: map[string][]string{"a": {"c", "b"}},
		},
		{
			name: "fan in lists the waiter under each blocker once",
			tickets: []plan.Ticket{
				{URL: "a"},
				{URL: "b"},
				{URL: "c", BlockedBy: []string{"a", "b", "a"}},
			},
			want: map[string][]string{"a": {"c"}, "b": {"c"}},
		},
		{
			name:    "an untracked blocker still lists its waiter",
			tickets: []plan.Ticket{{URL: "b", BlockedBy: []string{"gone"}}},
			want:    map[string][]string{"gone": {"b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plan.Unlocks(tt.tickets); !maps.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("Unlocks = %v, want %v", got, tt.want)
			}
		})
	}
}
