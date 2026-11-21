package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func runStep(command bool) plan.Next {
	return plan.Next{Kind: plan.NextRun, Step: option.Some(plan.Step{Name: "s", Command: command})}
}

func TestLaunchPlan(t *testing.T) {
	t.Parallel()

	unlocked := noBlockers
	blocked := plan.Unlock{Reason: "blocked"}

	tests := []struct {
		name             string
		candidates       []plan.LaunchCandidate
		currentlyRunning int
		runningCommands  int
		maxAgents        int
		maxCommands      int
		spendPaused      bool
		want             []plan.TicketURL
	}{
		{
			name: "an unlocked authorised hash-matching candidate with no prior run launches",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name: "eligible candidates launch in input order",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents: 2,
			want:      []plan.TicketURL{"sandbox://CC-2", "sandbox://CC-1"},
		},
		{
			name: "a locked candidate is excluded",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: blocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "an unauthorised candidate is excluded",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: false, PromptHashMatches: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "a mismatched prompt hash is excluded",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: false},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "a candidate with a prior run is excluded: LaunchPlan never re-runs",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, HasRun: true},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "eligible candidates are capped at maxAgents minus currentlyRunning, in input order",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{URL: "sandbox://CC-3", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			currentlyRunning: 1,
			maxAgents:        2,
			want:             []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name: "a candidate with no run whose blocker's CI is red is excluded",
			candidates: []plan.LaunchCandidate{
				{
					URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true,
					PromptHashMatches: true, RedBlocker: "sandbox://CC-1",
				},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "a candidate whose base already carries a conflict is excluded",
			candidates: []plan.LaunchCandidate{
				{
					URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true,
					PromptHashMatches: true, ConflictedBase: "cc-1-first",
				},
			},
			maxAgents: 1,
			want:      nil,
		},
		{
			name: "in-flight passes take slots before new launches",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
				{URL: "sandbox://CC-2", HasRun: true, InFlight: true},
			},
			maxAgents: 1,
			want:      []plan.TicketURL{"sandbox://CC-2"},
		},
		{
			name: "one agent and one command step launch together under max_agents 1 and max_verifies 1",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(false)},
				{URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(true)},
			},
			maxAgents:   1,
			maxCommands: 1,
			want:        []plan.TicketURL{"sandbox://CC-1", "sandbox://CC-2"},
		},
		{
			name: "a command step over its cap stays queued while an agent slot is free",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(true)},
				{URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(false)},
			},
			runningCommands: 1,
			maxAgents:       1,
			maxCommands:     1,
			want:            []plan.TicketURL{"sandbox://CC-2"},
		},
		{
			name: "a queued command step launches once a command slot frees",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(true)},
			},
			maxCommands: 1,
			want:        []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name: "command steps are capped at max_verifies in input order",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(true)},
				{URL: "sandbox://CC-2", Unlock: unlocked, Authorised: true, PromptHashMatches: true, Next: runStep(true)},
			},
			maxCommands: 1,
			want:        []plan.TicketURL{"sandbox://CC-1"},
		},
		{
			name: "no free slots launches nothing",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			currentlyRunning: 1,
			maxAgents:        1,
			want:             nil,
		},
		{
			name: "spend paused excludes an otherwise-eligible candidate",
			candidates: []plan.LaunchCandidate{
				{URL: "sandbox://CC-1", Unlock: unlocked, Authorised: true, PromptHashMatches: true},
			},
			maxAgents:   1,
			spendPaused: true,
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			load := plan.Load{Agents: tt.currentlyRunning, Commands: tt.runningCommands}
			caps := plan.Caps{Agents: tt.maxAgents, Commands: tt.maxCommands}
			got := plan.LaunchPlan(tt.candidates, load, caps, tt.spendPaused)
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("LaunchPlan(%+v, %+v, paused %t) mismatch (-want +got):\n%s",
					load, caps, tt.spendPaused, diff)
			}
		})
	}
}
