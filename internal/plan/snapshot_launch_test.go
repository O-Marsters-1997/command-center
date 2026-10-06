package plan_test

import (
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func authorised(t plan.Ticket) plan.LaunchMembership {
	return plan.LaunchMembership{LaunchID: 1, Members: 1, PromptHash: plan.Hash(plan.Compose(t))}
}

func TestSnapshotLaunch(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second")
	third := ticket("3", "cc-3-third")
	live := 4242

	tests := []struct {
		name  string
		rules plan.Rules
		in    plan.Input
		want  []string
	}{
		{
			name:  "an authorised, unlocked ticket with no run launches",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[string]plan.LaunchMembership{first.URL: authorised(first)},
			},
			want: []string{first.URL},
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
				Memberships: map[string]plan.LaunchMembership{
					first.URL: {LaunchID: 1, Members: 1, PromptHash: "stale"},
				},
			},
		},
		{
			name:  "a ticket that already has a run does not launch again",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[string]plan.LaunchMembership{first.URL: authorised(first)},
				Runs:        map[string]plan.RunSummary{first.URL: {ID: 1, HasOutcome: true}},
			},
		},
		{
			name:  "a live run takes one of the free slots",
			rules: plan.Rules{MaxAgents: 2},
			in: plan.Input{
				Tickets: []plan.Ticket{first, second, third},
				Memberships: map[string]plan.LaunchMembership{
					second.URL: authorised(second), third.URL: authorised(third),
				},
				Runs: map[string]plan.RunSummary{first.URL: {ID: 1, Pgid: &live}},
			},
			want: []string{second.URL},
		},
		{
			name:  "the five-hour reading at the limit pauses every launch",
			rules: plan.Rules{MaxAgents: 2, SpendLimit5h: 80},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[string]plan.LaunchMembership{first.URL: authorised(first)},
				FiveHour:    0.8,
			},
		},
		{
			name:  "a reading below the limit launches",
			rules: plan.Rules{MaxAgents: 2, SpendLimit5h: 80},
			in: plan.Input{
				Tickets:     []plan.Ticket{first},
				Memberships: map[string]plan.LaunchMembership{first.URL: authorised(first)},
				FiveHour:    0.5,
			},
			want: []string{first.URL},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := derive(tt.rules, tt.in).Launch()
			if !slices.Equal(got, tt.want) {
				t.Errorf("Launch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEntryCarriesWhatTheLoopActsOn(t *testing.T) {
	t.Parallel()

	root := ticket("1", "cc-1-first")
	member := authorised(root)
	row := plan.PushRow{PushedTip: "abc", BaseBranch: "main"}
	snap := derive(plan.Rules{}, plan.Input{
		Tickets:     []plan.Ticket{root},
		Memberships: map[string]plan.LaunchMembership{root.URL: member},
		Verdict:     plan.VerdictFacts{PushRows: map[string]plan.PushRow{root.URL: row}},
	})

	got := entry(t, snap, root.URL)
	if got.PromptHash != member.PromptHash {
		t.Errorf("PromptHash = %q, want %q", got.PromptHash, member.PromptHash)
	}
	if got.LastPush == nil || *got.LastPush != row {
		t.Errorf("LastPush = %v, want %v", got.LastPush, row)
	}
}

func TestSnapshotLaunchAfterCountsAgentsStartedAndStoppedThisTick(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second")
	in := plan.Input{
		Tickets:     []plan.Ticket{first, second},
		Memberships: map[string]plan.LaunchMembership{first.URL: authorised(first), second.URL: authorised(second)},
	}
	snap := plan.Rules{MaxAgents: 1}.Derive(in)

	if got := snap.LaunchAfter(1, 0); len(got) != 0 {
		t.Errorf("LaunchAfter(1, 0) = %v, want none: a spawn took the only slot", got)
	}
	if got := snap.LaunchAfter(1, 1); !slices.Equal(got, []string{first.URL}) {
		t.Errorf("LaunchAfter(1, 1) = %v, want the first ticket: the kill freed the slot", got)
	}
}
