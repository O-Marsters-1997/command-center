package insight_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

func TestDeriveTicket(t *testing.T) {
	t.Parallel()
	at := func(m int) time.Time { return day0.Add(time.Duration(m) * time.Minute) }
	runA := insight.RunFact{
		ID: 1, StartedAt: at(0), EndedAt: at(10), WeightUSD: 1, Tokens: 100, Tools: map[string]int{"Bash": 2, "Read": 1},
	}
	runB := insight.RunFact{
		ID: 2, StartedAt: at(20), EndedAt: at(50), WeightUSD: 2, Tokens: 50, Tools: map[string]int{"Bash": 1, "Edit": 3},
	}
	calibrating := spend.Result{Factor: 0.01, Samples: spend.MinSamples - 1}

	tests := []struct {
		name  string
		facts insight.TicketFacts
		fit   spend.Result
		want  insight.TicketReport
	}{
		{
			name: "unmerged with no pushes",
			facts: insight.TicketFacts{
				Runs: []insight.RunFact{runA},
			},
			fit: calibrate,
			want: insight.TicketReport{
				AgentsWorking: 10 * time.Minute, Tokens: 100, ToolCalls: 3,
				PctWeek:    option.Some(spend.PctWeek(1, calibrate.Factor)),
				Cumulative: []insight.SpendStep{{At: at(10), Tokens: 100}},
				Tools:      []insight.ToolCount{{Name: "Bash", Calls: 2}, {Name: "Read", Calls: 1}},
				Runs:       []insight.RunFact{runA},
			},
		},
		{
			name: "merged over two runs with red then green pushes",
			facts: insight.TicketFacts{
				Runs: []insight.RunFact{runB, runA},
				Pushes: []insight.PushFact{
					{At: at(5), Verdict: insight.PushRed},
					{At: at(30), Verdict: insight.PushGreen},
					{At: at(40), Verdict: insight.PushGreen},
				},
				MergedAt: option.Some(at(60)),
			},
			fit: calibrate,
			want: insight.TicketReport{
				Merged: true, LaunchToMerge: option.Some(60 * time.Minute),
				AgentsWorking: 40 * time.Minute, Tokens: 150, ToolCalls: 7,
				PctWeek:       option.Some(spend.PctWeek(3, calibrate.Factor)),
				PushesToGreen: option.Some(2),
				Cumulative:    []insight.SpendStep{{At: at(10), Tokens: 100}, {At: at(50), Tokens: 150}},
				Tools: []insight.ToolCount{
					{Name: "Bash", Calls: 3}, {Name: "Edit", Calls: 3}, {Name: "Read", Calls: 1},
				},
				Runs: []insight.RunFact{runB, runA},
			},
		},
		{
			name: "calibrating hides the share",
			facts: insight.TicketFacts{
				Runs:   []insight.RunFact{runA},
				Pushes: []insight.PushFact{{At: at(5), Verdict: insight.PushPending}},
			},
			fit: calibrating,
			want: insight.TicketReport{
				AgentsWorking: 10 * time.Minute, Tokens: 100, ToolCalls: 3,
				Cumulative: []insight.SpendStep{{At: at(10), Tokens: 100}},
				Tools:      []insight.ToolCount{{Name: "Bash", Calls: 2}, {Name: "Read", Calls: 1}},
				Runs:       []insight.RunFact{runA},
			},
		},
		{
			name: "recorded runs split into stage shares",
			facts: insight.TicketFacts{Runs: []insight.RunFact{
				{
					ID: 1, Kind: "agent", StartedAt: at(0), EndedAt: at(10), Tokens: 80,
					Phases: []insight.SkillLoad{{Skill: "", Tokens: 20}, {Skill: "tdd", Order: 1, Tokens: 60}},
				},
				{
					ID: 2, Kind: "follow_up", StartedAt: at(20), EndedAt: at(30), Tokens: 20,
					Phases: []insight.SkillLoad{{Skill: "", Tokens: 20}},
				},
			}},
			fit: calibrating,
			want: insight.TicketReport{
				AgentsWorking: 20 * time.Minute, Tokens: 100,
				Cumulative: []insight.SpendStep{{At: at(10), Tokens: 80}, {At: at(30), Tokens: 100}},
				Stages: []insight.StageShare{
					{Stage: insight.Stage{Kind: insight.StageSetup}, Tokens: 20, Share: 0.2},
					{Stage: insight.Stage{Kind: insight.StageSkill, Skill: "tdd"}, Tokens: 60, Share: 0.6},
					{Stage: insight.Stage{Kind: insight.StageFollowUps}, Tokens: 20, Share: 0.2},
				},
				Runs: []insight.RunFact{
					{
						ID: 1, Kind: "agent", StartedAt: at(0), EndedAt: at(10), Tokens: 80,
						Phases: []insight.SkillLoad{{Skill: "", Tokens: 20}, {Skill: "tdd", Order: 1, Tokens: 60}},
					},
					{
						ID: 2, Kind: "follow_up", StartedAt: at(20), EndedAt: at(30), Tokens: 20,
						Phases: []insight.SkillLoad{{Skill: "", Tokens: 20}},
					},
				},
			},
		},
		{
			name:  "no runs",
			facts: insight.TicketFacts{MergedAt: option.Some(at(60))},
			fit:   calibrate,
			want:  insight.TicketReport{Merged: true, PctWeek: option.Some(0.0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := insight.DeriveTicket(tt.facts, tt.fit)
			opts := cmp.AllowUnexported(option.Option[time.Duration]{}, option.Option[float64]{}, option.Option[int]{})
			if diff := cmp.Diff(tt.want, got, opts); diff != "" {
				t.Errorf("DeriveTicket mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
