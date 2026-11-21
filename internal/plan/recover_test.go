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

func TestSnapshot_AutoRemovals(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")

	checkedOut := map[plan.BranchRef]string{key(first.Branch): "/wt/cc-1"}

	tests := []struct {
		name      string
		pr        plan.PRState
		worktrees map[plan.BranchRef]string
		intents   []plan.Intent
		held      []plan.Intent
		refusals  map[plan.TicketURL]string
		want      []plan.TicketURL
	}{
		{name: "merged with a worktree", pr: plan.Merged, worktrees: checkedOut, want: []plan.TicketURL{first.URL}},
		{name: "open pull request", pr: plan.Open, worktrees: checkedOut},
		{name: "worktree already gone", pr: plan.Merged},
		{name: "intent pending", pr: plan.Merged, worktrees: checkedOut,
			intents: []plan.Intent{{ID: 1, Ticket: first.URL, Verb: plan.VerbRemoveWorktree}}},
		{name: "intent held for undo", pr: plan.Merged, worktrees: checkedOut,
			held: []plan.Intent{{ID: 1, Ticket: first.URL, Verb: plan.VerbRemoveWorktree}}},
		{name: "removal refused before", pr: plan.Merged, worktrees: checkedOut,
			refusals: map[plan.TicketURL]string{first.URL: "worktree is dirty"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[key(first.Branch)] = plan.PR{Number: 1, State: tt.pr}
			obs.Worktrees = tt.worktrees
			snap := derive(plan.Rules{}, plan.Input{
				Tickets: []plan.Ticket{first}, Obs: obs,
				Runs:     map[plan.TicketURL]plan.RunSummary{first.URL: {Outcome: option.Some(plan.OutcomePush)}},
				Intents:  map[plan.TicketURL][]plan.Intent{first.URL: tt.intents},
				Held:     map[plan.TicketURL][]plan.Intent{first.URL: tt.held},
				Removals: tt.refusals,
			})
			if diff := cmp.Diff(tt.want, snap.AutoRemovals(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("AutoRemovals() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_Undoable(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")

	tests := []struct {
		name string
		pr   plan.PRState
		url  plan.TicketURL
		want bool
	}{
		{name: "open pull request", pr: plan.Open, url: first.URL, want: true},
		{name: "merged pull request", pr: plan.Merged, url: first.URL},
		{name: "unknown ticket", pr: plan.Open, url: "sandbox://missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.PRs[key(first.Branch)] = plan.PR{Number: 1, State: tt.pr}
			snap := derive(plan.Rules{}, plan.Input{
				Tickets: []plan.Ticket{first}, Obs: obs,
				Runs: map[plan.TicketURL]plan.RunSummary{first.URL: {Outcome: option.Some(plan.OutcomePush)}},
			})
			if got := snap.Undoable(tt.url); got != tt.want {
				t.Errorf("Undoable(%q) = %t, want %t", tt.url, got, tt.want)
			}
		})
	}
}

func TestSnapshot_AutoFollowUps_VerifyFailed(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1")
	failedVerify := plan.VerifyFact{Tip: "t1", Outcome: option.Some(plan.OutcomeFailed), LogPath: "verify.log"}
	started := []plan.AutoFollowUp{{Ticket: first.URL, Cause: plan.CauseVerifyFailed, Tip: "t1", LogPath: "verify.log"}}

	tests := []struct {
		name      string
		history   plan.AutoHistory
		want      []plan.AutoFollowUp
		wantState plan.State
	}{
		{name: "a failed verify on the tip", want: started, wantState: plan.VerificationFailed},
		{name: "a spent round counts toward the shared cap", history: plan.AutoHistory{Rounds: 1, LastTip: "old"},
			want: started, wantState: plan.VerificationFailed},
		{name: "round already run for this tip", history: plan.AutoHistory{Rounds: 1, LastTip: "t1"},
			wantState: plan.VerificationFailed},
		{name: "rounds spent park the ticket", history: plan.AutoHistory{Rounds: plan.MaxAutoRounds, LastTip: "old"},
			wantState: plan.Parked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			obs.LocalTips[key(first.Branch)] = "t1"
			snap := derive(plan.Rules{Verify: map[string]bool{"r": true}}, plan.Input{
				Tickets: []plan.Ticket{first}, Obs: obs,
				Runs:     map[plan.TicketURL]plan.RunSummary{first.URL: {Outcome: option.Some(plan.OutcomePush)}},
				Verifies: map[plan.TicketURL]plan.VerifyFact{first.URL: failedVerify},
				Auto:     map[plan.TicketURL]plan.AutoHistory{first.URL: tt.history},
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
