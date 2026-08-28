package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestLaunchPlan(t *testing.T) {
	t.Parallel()

	unlocked := plan.Unlock{Unlocked: true, BaseBranch: "main", Reason: "no blockers"}
	blocked := plan.Unlock{Reason: "blocked"}

	tests := []struct {
		name             string
		candidates       []plan.LaunchCandidate
		currentlyRunning int
		maxAgents        int
		want             []string
	}{
		{
			name: "an unlocked authorised hash-matching candidate with no prior run launches",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      []string{"sandbox://CC-1"},
		},
		{
			name: "a locked candidate is excluded",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: blocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "an unauthorised candidate is excluded",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: false, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "a mismatched prompt hash is excluded",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: false},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "a candidate with a prior run is excluded: LaunchPlan never re-runs",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, HasRun: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "eligible candidates are capped at maxAgents minus currentlyRunning, in input order",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{TicketURL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{TicketURL: "sandbox://CC-3", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			currentlyRunning: 1,
			maxAgents:        2,
			want:             []string{"sandbox://CC-1"},
		},
		{
			name: "a candidate whose base already carries a conflict is excluded",
			candidates: []plan.LaunchCandidate{
				{
					TicketURL: "sandbox://CC-2", Unlock: unlocked, Authorised: true,
					PromptHashMatches: true, ConflictedBase: "cc-1-first",
				},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "no free slots launches nothing",
			candidates: []plan.LaunchCandidate{
				{TicketURL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			currentlyRunning: 1,
			maxAgents:        1,
			want:             nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := plan.LaunchPlan(tt.candidates, tt.currentlyRunning, tt.maxAgents)
			if !equalUnordered(got, tt.want) || len(got) != len(tt.want) {
				t.Errorf("LaunchPlan() = %v, want %v", got, tt.want)
			}
		})
	}
}
