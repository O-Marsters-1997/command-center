package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func authorised(t plan.Ticket) plan.LaunchMembership {
	return plan.LaunchMembership{LaunchID: 1, Members: 1, PromptHash: plan.Hash(plan.Compose(t))}
}

func TestSnapshot_LaunchAfter(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second")
	third := ticket("3", "cc-3-third")
	live := 4242

	tests := []struct {
		name  string
		rules plan.Rules
		in    plan.Input
		want  []plan.TicketURL
	}{
		{
			name:  "an authorised, unlocked ticket with no run launches",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: authorised(first)},
			},
			want: []plan.TicketURL{first.URL},
		},
		{
			name:  "a ticket outside every launch never launches",
			rules: plan.Rules{MaxAgents: 2},
			in:    plan.Input{Tickets: []plan.Ticket{first}},
		},
		{
			name:  "an edited prompt no longer matches what was authorised",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets: []plan.Ticket{first},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{
					first.URL: {LaunchID: 1, Members: 1, PromptHash: "stale"},
				},
			},
		},
		{
			name:  "a ticket that already has a run does not launch again",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: authorised(first)},
				Runs:        map[plan.TicketURL]plan.RunSummary{first.URL: {ID: 1, Outcome: option.Some(plan.OutcomePush)}},
			},
		},
		{
			name:  "a live run takes one of the free slots",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets: []plan.Ticket{first, second, third},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{
					second.URL: authorised(second), third.URL: authorised(third),
				},
				Runs: map[plan.TicketURL]plan.RunSummary{first.URL: {ID: 1, Pgid: option.Some(live)}},
			},
			want: []plan.TicketURL{second.URL},
		},
		{
			name:  "the five-hour reading at the limit pauses every launch",
			rules: plan.Rules{MaxAgents: 2, SpendLimit5h: 80},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: authorised(first)},
				FiveHour:    0.8,
			},
		},
		{
			name:  "a reading below the limit launches",
			rules: plan.Rules{MaxAgents: 2, SpendLimit5h: 80},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: authorised(first)},
				FiveHour:    0.5,
			},
			want: []plan.TicketURL{first.URL},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, derive(tt.rules, tt.in).LaunchAfter(0), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("LaunchAfter(0) mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("counts agents started this tick", func(t *testing.T) {
		t.Parallel()
		in := plan.Input{
			Tickets:     []plan.Ticket{first, second},
			Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: authorised(first), second.URL: authorised(second)},
		}
		snap := plan.Rules{MaxAgents: 1}.Derive(in)

		if diff := cmp.Diff([]plan.TicketURL{first.URL}, snap.LaunchAfter(0)); diff != "" {
			t.Errorf("LaunchAfter(0) mismatch (-want +got):\n%s", diff)
		}
		if got := snap.LaunchAfter(1); len(got) != 0 {
			t.Errorf("LaunchAfter(1) = %v, want none", got)
		}
	})
}

func TestSnapshot_Entry(t *testing.T) {
	t.Parallel()

	root := ticket("1", "cc-1-first")
	member := authorised(root)
	row := plan.PushRow{PushedTip: "abc", BaseBranch: "main"}
	snap := derive(plan.Rules{}, plan.Input{
		Tickets:     []plan.Ticket{root},
		Memberships: map[plan.TicketURL]plan.LaunchMembership{root.URL: member},
		Verdict:     plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{root.URL: row}},
	})

	got := entry(t, snap, root.URL)
	if got.PromptHash != member.PromptHash {
		t.Errorf("Entry(%s).PromptHash = %q, want %q", root.URL, got.PromptHash, member.PromptHash)
	}
	if got.LastPush != option.Some(row) {
		t.Errorf("Entry(%s).LastPush = %v, want %v", root.URL, got.LastPush, row)
	}

	t.Run("an unknown ticket", func(t *testing.T) {
		t.Parallel()
		if _, ok := snap.Entry("sandbox://CC-404"); ok {
			t.Error("Entry(sandbox://CC-404) ok = true, want false")
		}
	})
}
