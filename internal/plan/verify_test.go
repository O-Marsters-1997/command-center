package plan_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestSnapshot_Overdue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	run := func(id plan.RunID, kind plan.RunKind, age time.Duration) plan.RunSummary {
		return plan.RunSummary{
			ID: id, Kind: kind, Pgid: option.Some(int(id)),
			ProcStartedAt: option.Some(now.Add(-age)),
		}
	}
	finished := run(5, plan.RunKindCommand, time.Hour)
	finished.Outcome = option.Some(plan.OutcomeFailed)

	tests := []struct {
		name string
		runs []plan.RunSummary
		want []plan.RunID
	}{
		{
			name: "a command run past the deadline",
			runs: []plan.RunSummary{run(1, plan.RunKindCommand, plan.CommandDeadline+time.Second)},
			want: []plan.RunID{1},
		},
		{
			name: "a command run at the deadline",
			runs: []plan.RunSummary{run(1, plan.RunKindCommand, plan.CommandDeadline)},
		},
		{name: "a finished command run", runs: []plan.RunSummary{finished}},
		{
			name: "agent, follow-up and resolve runs never expire",
			runs: []plan.RunSummary{
				run(1, plan.RunKindAgent, time.Hour),
				run(2, plan.RunKindFollowUp, time.Hour),
				run(3, plan.RunKindResolve, time.Hour),
			},
		},
		{
			name: "overdue runs come back in id order",
			runs: []plan.RunSummary{
				run(7, plan.RunKindCommand, time.Hour),
				run(3, plan.RunKindCommand, time.Hour),
				run(4, plan.RunKindCommand, time.Minute),
			},
			want: []plan.RunID{3, 7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := plan.Input{Now: now, Runs: map[plan.TicketURL]plan.RunSummary{}}
			for _, r := range tt.runs {
				in.Runs[plan.TicketURL(fmt.Sprintf("sandbox://%d", r.ID))] = r
			}
			if diff := cmp.Diff(tt.want, derive(plan.Rules{}, in).Overdue()); diff != "" {
				t.Errorf("Overdue() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_VerifyAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	first, second := ticket("1", "cc-1-first"), ticket("2", "cc-2-second")
	pushRun := map[plan.TicketURL]plan.RunSummary{
		first.URL:  {Outcome: option.Some(plan.OutcomePush)},
		second.URL: {Outcome: option.Some(plan.OutcomePush)},
	}
	advanced := map[plan.TicketURL]plan.RefreshFact{first.URL: {Advanced: true}, second.URL: {Advanced: true}}
	obs := plan.NewObservation(now)
	obs.LocalTips[key(first.Branch)], obs.LocalTips[key(second.Branch)] = "t1", "t2"

	tests := []struct {
		name     string
		verifies map[plan.TicketURL]plan.VerifyFact
		spawned  int
		want     []plan.Verify
	}{
		{
			name:    "both fit under a cap of two",
			spawned: 0,
			want:    []plan.Verify{{Ticket: first.URL, Tip: "t1"}, {Ticket: second.URL, Tip: "t2"}},
		},
		{
			name:    "a command spawned this tick takes a slot",
			spawned: 1,
			want:    []plan.Verify{{Ticket: first.URL, Tip: "t1"}},
		},
		{
			name:     "a live verify takes a slot and its ticket is not pending",
			verifies: map[plan.TicketURL]plan.VerifyFact{first.URL: {Tip: "t1", Alive: true, StartedAt: now}},
			want:     []plan.Verify{{Ticket: second.URL, Tip: "t2"}},
		},
		{
			name: "a passed verify for the tip is not pending",
			verifies: map[plan.TicketURL]plan.VerifyFact{
				first.URL: {Tip: "t1", Outcome: option.Some(plan.OutcomePassed)},
			},
			want: []plan.Verify{{Ticket: second.URL, Tip: "t2"}},
		},
		{
			name: "a verify for an older tip is pending again",
			verifies: map[plan.TicketURL]plan.VerifyFact{
				first.URL: {Tip: "old", Outcome: option.Some(plan.OutcomePassed)},
			},
			want: []plan.Verify{{Ticket: first.URL, Tip: "t1"}, {Ticket: second.URL, Tip: "t2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rules := plan.Rules{Verify: map[string]bool{"r": true}, MaxVerifies: 2}
			snap := derive(rules, plan.Input{
				Now: now, Tickets: []plan.Ticket{first, second}, Obs: obs, Runs: pushRun,
				Refreshes: advanced, Verifies: tt.verifies,
			})
			if diff := cmp.Diff(tt.want, snap.VerifyAfter(tt.spawned)); diff != "" {
				t.Errorf("VerifyAfter(%d) mismatch (-want +got):\n%s", tt.spawned, diff)
			}
		})
	}
}

func TestRules_Derive_VerifyState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tk := ticket("1", "cc-1-first")
	obs := plan.NewObservation(now)
	obs.LocalTips[key(tk.Branch)] = "t1"
	obs.Runs[tk.URL] = plan.RunObservation{Alive: true}
	pending := map[plan.TicketURL]plan.RefreshFact{tk.URL: {Advanced: true}}
	tests := []struct {
		name string
		fact plan.VerifyFact
		want plan.State
	}{
		{name: "alive", fact: plan.VerifyFact{Tip: "t1", Alive: true, StartedAt: now}, want: plan.Verifying},
		{
			name: "alive past the deadline counts as failed",
			fact: plan.VerifyFact{Tip: "t1", Alive: true, StartedAt: now.Add(-2 * plan.CommandDeadline)},
			want: plan.VerificationFailed,
		},
		{
			name: "failed",
			fact: plan.VerifyFact{Tip: "t1", Outcome: option.Some(plan.OutcomeFailed)},
			want: plan.VerificationFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap := derive(plan.Rules{Verify: map[string]bool{"r": true}, MaxVerifies: 1}, plan.Input{
				Now: now, Tickets: []plan.Ticket{tk}, Obs: obs, Refreshes: pending,
				Runs:     map[plan.TicketURL]plan.RunSummary{tk.URL: {Outcome: option.Some(plan.OutcomePush)}},
				Verifies: map[plan.TicketURL]plan.VerifyFact{tk.URL: tt.fact},
			})
			if got := entry(t, snap, tk.URL).State; got != tt.want {
				t.Errorf("state = %s, want %s", got, tt.want)
			}
		})
	}

	t.Run("a pending verify is waiting to verify", func(t *testing.T) {
		t.Parallel()
		snap := derive(plan.Rules{Verify: map[string]bool{"r": true}, MaxVerifies: 1}, plan.Input{
			Now: now, Tickets: []plan.Ticket{tk}, Obs: obs, Refreshes: pending,
			Runs: map[plan.TicketURL]plan.RunSummary{tk.URL: {Outcome: option.Some(plan.OutcomePush)}},
		})
		if got := entry(t, snap, tk.URL).State; got != plan.WaitingToVerify {
			t.Errorf("state = %s, want %s", got, plan.WaitingToVerify)
		}
	})
}
