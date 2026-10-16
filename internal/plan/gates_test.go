package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSnapshot_Intents(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second")
	snap := derive(plan.Rules{}, plan.Input{
		Tickets: []plan.Ticket{first, second},
		Intents: map[plan.TicketURL][]plan.Intent{
			first.URL: {
				{ID: 3, Ticket: first.URL, Verb: plan.VerbReRun},
				{ID: 1, Ticket: first.URL, Verb: plan.VerbKill},
			},
			second.URL: {
				{ID: 2, Ticket: second.URL, Verb: plan.VerbReRun, Payload: "again"},
			},
		},
	})

	want := []plan.Intent{
		{ID: 2, Ticket: second.URL, Verb: plan.VerbReRun, Payload: "again"},
		{ID: 3, Ticket: first.URL, Verb: plan.VerbReRun},
	}
	if diff := cmp.Diff(want, snap.Intents(plan.VerbReRun)); diff != "" {
		t.Errorf("Intents(re-run) mismatch (-want +got):\n%s", diff)
	}
	if got := snap.Intents(plan.VerbAbort); len(got) != 0 {
		t.Errorf("Intents(abort) = %v, want none", got)
	}
}

func TestSnapshot_Retargets(t *testing.T) {
	t.Parallel()
	base := ticket("1", "cc-1-base")
	child := ticket("2", "cc-2-child", base.URL)
	row := plan.PushRow{PushedTip: "tip", BaseBranch: base.Branch, BaseSHAAtPush: "b0"}
	tests := []struct {
		name string
		prs  map[plan.BranchRef]plan.PR
		row  plan.PushRow
		want bool
	}{
		{name: "base merged, child open", prs: map[plan.BranchRef]plan.PR{
			key(base.Branch): {State: plan.Merged}, key(child.Branch): {State: plan.Open},
		}, row: row, want: true},
		{name: "base still open", prs: openPRs(base, child), row: row},
		{name: "child closed", prs: map[plan.BranchRef]plan.PR{
			key(base.Branch): {State: plan.Merged}, key(child.Branch): {State: plan.Closed},
		}, row: row},
		{name: "already pointed at main", prs: map[plan.BranchRef]plan.PR{
			key(base.Branch): {State: plan.Merged}, key(child.Branch): {State: plan.Open},
		}, row: plan.PushRow{PushedTip: "tip", BaseBranch: plan.DefaultBaseBranch}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap := derive(plan.Rules{}, plan.Input{
				Tickets: []plan.Ticket{base, child},
				Obs:     plan.Observation{PRs: tt.prs},
				Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{child.URL: tt.row}},
			})
			var want []plan.Retarget
			if tt.want {
				want = []plan.Retarget{{Ticket: child.URL, Row: tt.row}}
			}
			if diff := cmp.Diff(want, snap.Retargets()); diff != "" {
				t.Errorf("Retargets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_Refreshes(t *testing.T) {
	t.Parallel()
	base := ticket("1", "cc-1-base")
	child := ticket("2", "cc-2-child", base.URL)
	row := plan.PushRow{PushedTip: "tip", BaseBranch: base.Branch, BaseSHAAtPush: "b0"}
	conflictAt := func(branchTip, baseTip plan.SHA) plan.RefreshFact {
		return plan.RefreshFact{Tried: true, Conflict: option.Some(plan.ConflictAt{BranchTip: branchTip, BaseTip: baseTip})}
	}
	tests := []struct {
		name     string
		baseTip  plan.SHA
		childTip plan.SHA
		fact     plan.RefreshFact
		intents  []plan.Intent
		unpushed bool
		want     []plan.Refresh
	}{
		{name: "base moved", baseTip: "b1", want: []plan.Refresh{{Ticket: child.URL, Row: row}}},
		{name: "base unmoved", baseTip: "b0"},
		{name: "never pushed", baseTip: "b1", unpushed: true},
		{name: "tried and refused", baseTip: "b1", fact: plan.RefreshFact{Tried: true, Refused: true}},
		{name: "conflict at the same tips", baseTip: "b1", childTip: "c1", fact: conflictAt("c1", "b1")},
		{
			name: "conflict superseded by a new base tip", baseTip: "b2", childTip: "c1", fact: conflictAt("c1", "b1"),
			want: []plan.Refresh{{Ticket: child.URL, Row: row}},
		},
		{
			name: "conflict superseded by a new branch tip", baseTip: "b1", childTip: "c2", fact: conflictAt("c1", "b1"),
			want: []plan.Refresh{{Ticket: child.URL, Row: row}},
		},
		{
			name: "requested once even when the base is unmoved", baseTip: "b0",
			intents: []plan.Intent{{ID: 1, Ticket: child.URL, Verb: plan.VerbRefresh}, {ID: 2, Ticket: child.URL, Verb: plan.VerbRefresh}},
			want:    []plan.Refresh{{Ticket: child.URL, Row: row, Requested: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := plan.Input{
				Tickets: []plan.Ticket{base, child},
				Runs:    map[plan.TicketURL]plan.RunSummary{child.URL: pushedRun()},
				Obs: plan.Observation{
					PRs:        openPRs(base, child),
					BranchTips: map[plan.BranchRef]plan.SHA{key(base.Branch): tt.baseTip, key(child.Branch): tt.childTip},
				},
				Verdict:   plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{child.URL: row}},
				Refreshes: map[plan.TicketURL]plan.RefreshFact{child.URL: tt.fact},
				Intents:   map[plan.TicketURL][]plan.Intent{child.URL: tt.intents},
			}
			if tt.unpushed {
				in.Verdict = plan.VerdictFacts{}
			}
			snap := derive(plan.Rules{}, in)
			if diff := cmp.Diff(tt.want, snap.Refreshes()); diff != "" {
				t.Errorf("Refreshes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
