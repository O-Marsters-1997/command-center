package plan_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
)

func TestSnapshot_AutoFollowUps(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")
	rules := plan.Rules{Checks: map[string]verdict.Predicate{"r": {Success: "CI"}}}
	failed := func(attempt int) plan.CheckState {
		return plan.CheckState{Status: "COMPLETED", Conclusion: "FAILURE", Attempt: attempt}
	}

	tests := []struct {
		name      string
		check     plan.CheckState
		history   plan.AutoHistory
		intents   []plan.Intent
		want      []plan.AutoFollowUp
		wantState plan.State
	}{
		{name: "failed twice", check: failed(2),
			want:      []plan.AutoFollowUp{{Ticket: first.URL, Cause: plan.CauseCIFailed, Tip: "tip"}},
			wantState: plan.CIFailed},
		{name: "second round after the first", check: failed(2), history: plan.AutoHistory{Rounds: 1, LastTip: "old"},
			want:      []plan.AutoFollowUp{{Ticket: first.URL, Cause: plan.CauseCIFailed, Tip: "tip"}},
			wantState: plan.CIFailed},
		{name: "failed once is still a rerun", check: failed(1), wantState: plan.CIFailed},
		{name: "attempt unread", check: failed(0), wantState: plan.CIFailed},
		{name: "round already run for this tip", check: failed(2),
			history: plan.AutoHistory{Rounds: 1, LastTip: "tip"}, wantState: plan.CIFailed},
		{name: "rounds spent park the ticket", check: failed(2),
			history: plan.AutoHistory{Rounds: plan.MaxAutoRounds, LastTip: "old"}, wantState: plan.Parked},
		{name: "operator intent pending", check: failed(2), wantState: plan.CIFailed,
			intents: []plan.Intent{{ID: 1, Ticket: first.URL, Verb: plan.VerbFollowUp}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[key(first.Branch)] = plan.PR{
				Number: 1, State: plan.Open, HeadOid: "tip", Checks: map[string]plan.CheckState{"CI": tt.check},
			}
			snap := derive(rules, plan.Input{
				Tickets: []plan.Ticket{first}, Obs: obs,
				Runs:    map[plan.TicketURL]plan.RunSummary{first.URL: {Outcome: option.Some(plan.OutcomePush)}},
				Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{first.URL: {PushedTip: "tip"}}},
				Auto:    map[plan.TicketURL]plan.AutoHistory{first.URL: tt.history},
				Intents: map[plan.TicketURL][]plan.Intent{first.URL: tt.intents},
			})
			if got := entry(t, snap, first.URL).State; got != tt.wantState {
				t.Errorf("state = %s, want %s", got, tt.wantState)
			}
			if diff := cmp.Diff(tt.want, snap.AutoFollowUps(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("AutoFollowUps() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_ParkedLocksItsDependents(t *testing.T) {
	t.Parallel()
	parked, child, grandchild, merged := ticket("1", "cc-1"), ticket("2", "cc-2"), ticket("3", "cc-3"), ticket("4", "cc-4")
	child.BlockedBy = []plan.TicketURL{parked.URL}
	grandchild.BlockedBy = []plan.TicketURL{child.URL}
	merged.BlockedBy = []plan.TicketURL{parked.URL}
	obs := plan.NewObservation(time.Time{})
	obs.PRs[key(parked.Branch)] = plan.PR{
		Number: 1, State: plan.Open, HeadOid: "tip",
		Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE", Attempt: 2}},
	}
	obs.PRs[key(merged.Branch)] = plan.PR{Number: 4, State: plan.Merged}
	in := plan.Input{
		Tickets: []plan.Ticket{parked, child, grandchild, merged}, Obs: obs,
		Runs:    map[plan.TicketURL]plan.RunSummary{parked.URL: {Outcome: option.Some(plan.OutcomePush)}},
		Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{parked.URL: {PushedTip: "tip"}}},
		Auto:    map[plan.TicketURL]plan.AutoHistory{parked.URL: {Rounds: plan.MaxAutoRounds, LastTip: "old"}},
	}
	rules := plan.Rules{Checks: map[string]verdict.Predicate{"r": {Success: "CI"}}}

	snap := derive(rules, in)
	if got := entry(t, snap, parked.URL); got.State != plan.Parked || got.LockedBehind != 2 {
		t.Errorf("parked entry = state %s, locked %d, want parked, 2", got.State, got.LockedBehind)
	}
	var parkedTransitions int
	for _, tr := range snap.Transitions() {
		if tr.Kind == plan.TransitionParked {
			parkedTransitions++
		}
	}
	if parkedTransitions != 1 {
		t.Errorf("parked transitions = %d, want 1", parkedTransitions)
	}

	in.Recorded = map[plan.TicketURL]plan.Recorded{parked.URL: {Parked: true}}
	for _, tr := range derive(rules, in).Transitions() {
		if tr.Kind == plan.TransitionParked {
			t.Errorf("recorded park produced %v, want none", tr)
		}
	}
}

func TestSnapshot_Reruns(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")
	const lintURL = "https://github.com/o/r/actions/runs/1/job/2"
	const buildURL = "https://github.com/o/r/actions/runs/3/job/4"
	failed := func(url string, attempt int) plan.CheckState {
		return plan.CheckState{Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: url, Attempt: attempt}
	}

	tests := []struct {
		name  string
		state plan.PRState
		check map[string]plan.CheckState
		want  []plan.Rerun
	}{
		{name: "failed on attempt 1", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1)},
			want:  []plan.Rerun{{Ticket: first.URL, DetailsURL: lintURL}}},
		{name: "failed checks list in name order", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1), "Build": failed(buildURL, 1)},
			want: []plan.Rerun{
				{Ticket: first.URL, DetailsURL: buildURL}, {Ticket: first.URL, DetailsURL: lintURL},
			}},
		{name: "failed on attempt 2 is never rerun", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 2)}},
		{name: "attempt unread", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 0)}},
		{name: "passed on attempt 1", state: plan.Open,
			check: map[string]plan.CheckState{
				"Lint": {Status: "COMPLETED", Conclusion: "SUCCESS", DetailsURL: lintURL, Attempt: 1},
			}},
		{name: "still running", state: plan.Open,
			check: map[string]plan.CheckState{"Lint": {Status: "IN_PROGRESS", DetailsURL: lintURL, Attempt: 1}}},
		{name: "merged PR", state: plan.Merged,
			check: map[string]plan.CheckState{"Lint": failed(lintURL, 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[key(first.Branch)] = plan.PR{Number: 1, State: tt.state, Checks: tt.check}
			snap := derive(plan.Rules{}, plan.Input{Tickets: []plan.Ticket{first}, Obs: obs})
			if diff := cmp.Diff(tt.want, snap.Reruns(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Reruns() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
