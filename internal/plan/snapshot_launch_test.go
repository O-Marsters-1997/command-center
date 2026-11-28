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

			if diff := cmp.Diff(tt.want, derive(tt.rules, tt.in).LaunchAfter(nil), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("LaunchAfter(nil) mismatch (-want +got):\n%s", diff)
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

		if diff := cmp.Diff([]plan.TicketURL{first.URL}, snap.LaunchAfter(nil)); diff != "" {
			t.Errorf("LaunchAfter(nil) mismatch (-want +got):\n%s", diff)
		}
		if got := snap.LaunchAfter([]plan.TicketURL{first.URL}); len(got) != 0 {
			t.Errorf("LaunchAfter(spawned first) = %v, want none", got)
		}
	})
}

func TestSnapshot_RedBlocker(t *testing.T) {
	t.Parallel()

	parent := ticket("1", "cc-1-parent")
	child := ticket("2", "cc-2-child", parent.URL)
	rules := plan.Rules{
		MaxAgents: 2,
		Stacking:  map[string]bool{"r": true},
		Checks:    map[string]verdict.Predicate{"r": {Success: "CI"}},
	}
	pushedAt := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	input := func(conclusion string, childRun bool) plan.Input {
		in := plan.Input{
			Tickets: []plan.Ticket{parent, child},
			Memberships: map[plan.TicketURL]plan.LaunchMembership{
				parent.URL: authorised(parent), child.URL: authorised(child),
			},
			Obs: plan.Observation{
				PRs: map[plan.BranchRef]plan.PR{
					key(parent.Branch): {
						State: plan.Open, HeadOid: "tip",
						Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: conclusion}},
					},
				},
				BranchTips: map[plan.BranchRef]plan.SHA{key("main"): "main-tip"},
			},
			Runs: map[plan.TicketURL]plan.RunSummary{parent.URL: pushedRun()},
			Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{
				parent.URL: {PushedTip: "tip", BaseBranch: "main", BaseSHAAtPush: "main-tip", PushedAt: pushedAt},
			}},
		}
		if childRun {
			in.Runs[child.URL] = pushedRun()
		}
		return in
	}

	t.Run("a child with no run waits while the blocker's CI is red", func(t *testing.T) {
		t.Parallel()
		snap := derive(rules, input("FAILURE", false))

		if got := snap.LaunchAfter(nil); len(got) != 0 {
			t.Errorf("LaunchAfter(nil) = %v, want none", got)
		}
		got := entry(t, snap, child.URL)
		if want := plan.Reason("waiting on " + string(parent.URL) + "'s CI"); got.State != plan.Queued || got.Reason != want {
			t.Errorf("child = %v %q, want %v %q", got.State, got.Reason, plan.Queued, want)
		}
	})

	t.Run("the child launches once the blocker's CI is green", func(t *testing.T) {
		t.Parallel()
		got := derive(rules, input("SUCCESS", false)).LaunchAfter(nil)

		if diff := cmp.Diff([]plan.TicketURL{child.URL}, got); diff != "" {
			t.Errorf("LaunchAfter(nil) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a child that already ran keeps its unlock", func(t *testing.T) {
		t.Parallel()
		got := entry(t, derive(rules, input("FAILURE", true)), child.URL)

		if !got.Unlock.Unlocked {
			t.Errorf("child.Unlock.Unlocked = false, want true")
		}
	})
}

func TestSnapshot_LaunchContext(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	withContext := first
	withContext.LaunchContext = "prefer table tests"
	choice := plan.AgentChoice{Model: "opus", Effort: option.Some(plan.EffortHigh)}
	rules := plan.Rules{MaxAgents: 2}

	t.Run("a launch authorised with its context launches and carries its choice", func(t *testing.T) {
		t.Parallel()
		member := authorised(withContext)
		member.Context, member.Choice = withContext.LaunchContext, choice
		snap := derive(rules, plan.Input{
			Tickets:     []plan.Ticket{first},
			Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: member},
		})

		if diff := cmp.Diff([]plan.TicketURL{first.URL}, snap.LaunchAfter(nil)); diff != "" {
			t.Errorf("LaunchAfter(nil) mismatch (-want +got):\n%s", diff)
		}
		got := entry(t, snap, first.URL).Ticket
		if got.LaunchContext != withContext.LaunchContext || got.Choice != choice {
			t.Errorf("Entry(%s).Ticket = %+v, want context %q and choice %+v",
				first.URL, got, withContext.LaunchContext, choice)
		}
	})

	t.Run("a context edited after authorising no longer launches", func(t *testing.T) {
		t.Parallel()
		member := authorised(first)
		member.Context = "something else"
		snap := derive(rules, plan.Input{
			Tickets:     []plan.Ticket{first},
			Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: member},
		})

		if got := snap.LaunchAfter(nil); len(got) != 0 {
			t.Errorf("LaunchAfter(nil) = %v, want none", got)
		}
	})

	t.Run("a preview hash covers the context", func(t *testing.T) {
		t.Parallel()
		snap := derive(rules, plan.Input{Tickets: []plan.Ticket{first}})

		rows, err := snap.Preview([]plan.TicketURL{first.URL}, withContext.LaunchContext)
		if err != nil {
			t.Fatalf("Preview(%v) error = %v", first.URL, err)
		}
		if want := plan.Hash(plan.Compose(withContext)); rows[0].PromptHash != want {
			t.Errorf("Preview(%v, %q).PromptHash = %q, want %q", first.URL, withContext.LaunchContext, rows[0].PromptHash, want)
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
	if !got.LastPush.IsSome() {
		t.Fatalf("Entry(%s).LastPush = none, want %v", root.URL, row)
	}
	if diff := cmp.Diff(row, got.LastPush.Unwrap()); diff != "" {
		t.Errorf("Entry(%s).LastPush mismatch (-want +got):\n%s", root.URL, diff)
	}

	t.Run("an unknown ticket", func(t *testing.T) {
		t.Parallel()
		if _, ok := snap.Entry("sandbox://CC-404"); ok {
			t.Error("Entry(sandbox://CC-404) ok = true, want false")
		}
	})
}
