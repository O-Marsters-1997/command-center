package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestSnapshot_SlotFree(t *testing.T) {
	t.Parallel()

	first, second := ticket("1", "cc-1-first"), ticket("2", "cc-2-second")
	oneAgent := map[plan.TicketURL]plan.RunSummary{first.URL: {ID: 1, Pgid: option.Some(4242)}}
	twoAgents := map[plan.TicketURL]plan.RunSummary{
		first.URL:  {ID: 1, Pgid: option.Some(4242)},
		second.URL: {ID: 2, Pgid: option.Some(4243)},
	}
	oneCommand := map[plan.TicketURL]plan.VerifyFact{first.URL: {Alive: true}}

	tests := []struct {
		name    string
		in      plan.Input
		command bool
		started plan.Load
		want    bool
	}{
		{name: "an idle agent lane is free", want: true},
		{name: "a live agent below the cap leaves a slot", in: plan.Input{Runs: oneAgent}, want: true},
		{name: "live agents at the cap fill the lane", in: plan.Input{Runs: twoAgents}},
		{name: "agents started this tick fill the lane", in: plan.Input{Runs: oneAgent}, started: plan.Load{Agents: 1}},
		{name: "commands started this tick leave the agent lane alone", started: plan.Load{Commands: 5}, want: true},
		{name: "an idle command lane is free", command: true, want: true},
		{name: "a live command fills the command lane", in: plan.Input{Verifies: oneCommand}, command: true},
		{name: "a live command leaves the agent lane alone", in: plan.Input{Verifies: oneCommand}, want: true},
		{name: "commands started this tick fill the command lane", command: true, started: plan.Load{Commands: 1}},
		{name: "agents started leave the command lane alone", command: true, started: plan.Load{Agents: 5}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.in.Tickets = []plan.Ticket{first, second}
			snap := plan.Rules{MaxAgents: 2, MaxVerifies: 1}.Derive(tt.in)
			if got := snap.SlotFree(tt.command, tt.started); got != tt.want {
				t.Errorf("SlotFree(command=%t, %+v) = %t, want %t", tt.command, tt.started, got, tt.want)
			}
		})
	}
}
