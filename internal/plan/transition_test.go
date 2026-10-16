package plan_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestSnapshot_Transitions(t *testing.T) {
	t.Parallel()

	tk := ticket("1", "cc-1")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mergedAt := now.Add(-time.Hour)
	green := plan.CheckState{Status: "COMPLETED", Conclusion: "SUCCESS"}
	red := plan.CheckState{Status: "COMPLETED", Conclusion: "FAILURE"}
	running := plan.CheckState{Status: "IN_PROGRESS"}
	withPR := func(pr plan.PR) plan.Observation {
		return plan.Observation{PRs: map[plan.BranchRef]plan.PR{key(tk.Branch): pr}}
	}

	tests := []struct {
		name string
		in   plan.Input
		want []plan.Transition
	}{
		{
			name: "a pull request seen for the first time opens",
			in:   plan.Input{Obs: withPR(plan.PR{Number: 7, State: plan.Open})},
			want: []plan.Transition{{
				Ticket: tk.URL, Kind: plan.TransitionOpened, At: now, Detail: "PR #7 https://github.com/r/pull/7",
			}},
		},
		{
			name: "a pull request already in the previous observation is not new",
			in: plan.Input{
				Obs:  withPR(plan.PR{Number: 7, State: plan.Open}),
				Prev: withPR(plan.PR{Number: 7, State: plan.Open}),
			},
		},
		{
			name: "an opened event already recorded is not repeated",
			in: plan.Input{
				Obs:      withPR(plan.PR{Number: 7, State: plan.Open}),
				Recorded: map[plan.TicketURL]plan.Recorded{tk.URL: {Opened: true}},
			},
		},
		{
			name: "no pull request records nothing",
			in:   plan.Input{},
		},
		{
			name: "a merged pull request records its merge time",
			in: plan.Input{
				Obs:  withPR(plan.PR{Number: 7, State: plan.Merged, MergedAt: mergedAt}),
				Prev: withPR(plan.PR{Number: 7, State: plan.Open}),
			},
			want: []plan.Transition{{Ticket: tk.URL, Kind: plan.TransitionMerged, At: mergedAt, Detail: "PR #7 merged"}},
		},
		{
			name: "a merge already recorded is not repeated",
			in: plan.Input{
				Obs:      withPR(plan.PR{Number: 7, State: plan.Merged, MergedAt: mergedAt}),
				Prev:     withPR(plan.PR{Number: 7, State: plan.Merged}),
				Recorded: map[plan.TicketURL]plan.Recorded{tk.URL: {Merged: true}},
			},
		},
		{
			name: "a check that finishes lands once, in name order",
			in: plan.Input{
				Obs: withPR(plan.PR{Number: 7, State: plan.Open, Checks: map[string]plan.CheckState{
					"b": red, "a": green, "c": running,
				}}),
				Prev: withPR(plan.PR{Number: 7, State: plan.Open, Checks: map[string]plan.CheckState{"a": running}}),
			},
			want: []plan.Transition{
				{Ticket: tk.URL, Kind: plan.TransitionChecksLanded, At: now, Detail: "a " + verdict.Success.String()},
				{Ticket: tk.URL, Kind: plan.TransitionChecksLanded, At: now, Detail: "b " + verdict.Failure.String()},
			},
		},
		{
			name: "a check that had already finished does not land again",
			in: plan.Input{
				Obs:  withPR(plan.PR{Number: 7, State: plan.Open, Checks: map[string]plan.CheckState{"a": green}}),
				Prev: withPR(plan.PR{Number: 7, State: plan.Open, Checks: map[string]plan.CheckState{"a": red}}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.in.Now = now
			tt.in.Tickets = []plan.Ticket{tk}
			got := plan.Rules{}.Derive(tt.in).Transitions()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Transitions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_Verdicts(t *testing.T) {
	t.Parallel()

	tk := ticket("1", "cc-1")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	checked := plan.Rules{Checks: map[string]verdict.Predicate{"r": {Success: "CI"}}}
	passing := plan.Observation{PRs: map[plan.BranchRef]plan.PR{key(tk.Branch): {
		Number: 7, State: plan.Open, HeadOid: "tip",
		Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
	}}}
	base := func() plan.Input {
		return plan.Input{
			Now:     now,
			Tickets: []plan.Ticket{tk},
			Obs:     passing,
			Prev:    passing,
			Runs:    map[plan.TicketURL]plan.RunSummary{tk.URL: {Outcome: option.Some(plan.OutcomePush)}},
			Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{
				tk.URL: {PushedTip: "tip", PushedAt: now.Add(-time.Minute)},
			}},
		}
	}

	t.Run("a changed verdict is a transition and is saved", func(t *testing.T) {
		t.Parallel()

		in := base()
		in.LastVerdicts = map[plan.TicketURL]string{tk.URL: "checking"}
		snap := checked.Derive(in)

		got := snap.Transitions()
		if len(got) != 1 || got[0].Kind != plan.TransitionVerdict || got[0].Ticket != tk.URL || got[0].At != now {
			t.Fatalf("Transitions() = %+v, want one verdict transition for %s at %s", got, tk.URL, now)
		}
		want := map[plan.TicketURL]string{tk.URL: verdict.ReviewMe.String()}
		if diff := cmp.Diff(want, snap.Verdicts()); diff != "" {
			t.Errorf("Verdicts() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an unchanged verdict records nothing", func(t *testing.T) {
		t.Parallel()

		in := base()
		in.LastVerdicts = map[plan.TicketURL]string{tk.URL: verdict.ReviewMe.String()}
		snap := checked.Derive(in)

		if got := snap.Transitions(); len(got) != 0 {
			t.Errorf("Transitions() = %+v, want none", got)
		}
		if diff := cmp.Diff(in.LastVerdicts, snap.Verdicts()); diff != "" {
			t.Errorf("Verdicts() mismatch (-want +got):\n%s", diff)
		}
	})

	skips := []struct {
		name   string
		mutate func(*plan.Input, *plan.Rules)
	}{
		{"a run that did not push", func(in *plan.Input, _ *plan.Rules) {
			in.Runs = map[plan.TicketURL]plan.RunSummary{tk.URL: {Outcome: option.Some(plan.OutcomeFailed)}}
		}},
		{"a refused push", func(in *plan.Input, _ *plan.Rules) {
			in.Pushes = map[plan.TicketURL]plan.PushFact{tk.URL: {Refused: true}}
		}},
		{"a failed push", func(in *plan.Input, _ *plan.Rules) {
			in.Pushes = map[plan.TicketURL]plan.PushFact{tk.URL: {Failed: true}}
		}},
		{"a closed pull request", func(in *plan.Input, _ *plan.Rules) {
			in.Obs = plan.Observation{PRs: map[plan.BranchRef]plan.PR{key(tk.Branch): {Number: 7, State: plan.Closed}}}
			in.Prev = in.Obs
		}},
		{"a repo with no checks predicate", func(_ *plan.Input, r *plan.Rules) { *r = plan.Rules{} }},
	}
	for _, tt := range skips {
		t.Run(tt.name+" has no verdict transition", func(t *testing.T) {
			t.Parallel()

			in, rules := base(), checked
			tt.mutate(&in, &rules)
			if got := rules.Derive(in).Transitions(); len(got) != 0 {
				t.Errorf("Transitions() = %+v, want none", got)
			}
		})
	}
}
