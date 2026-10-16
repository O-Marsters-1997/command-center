package plan_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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

func TestSnapshot_Removal(t *testing.T) {
	t.Parallel()
	blocker := ticket("1", "cc-1-blocker")
	child := ticket("2", "cc-2-child", blocker.URL)
	pushed := plan.PushRow{PushedTip: "pushed"}
	ran := map[plan.TicketURL]plan.RunSummary{child.URL: {Outcome: option.Some(plan.OutcomePush)}}

	tests := []struct {
		name string
		in   plan.Input
		want plan.Removal
	}{
		{
			name: "merged pull request proves its head and the last push",
			in: plan.Input{
				Obs: plan.Observation{PRs: map[plan.BranchRef]plan.PR{
					key(child.Branch): {State: plan.Merged, HeadOid: "head"},
				}},
				Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{child.URL: pushed}},
			},
			want: plan.Removal{ProvenTips: []plan.SHA{"pushed", "head"}},
		},
		{
			name: "closed blocker with a run is removable on the last push alone",
			in: plan.Input{
				Obs: plan.Observation{PRs: map[plan.BranchRef]plan.PR{
					key(blocker.Branch): {State: plan.Closed},
				}},
				Runs:    ran,
				Verdict: plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{child.URL: pushed}},
			},
			want: plan.Removal{ProvenTips: []plan.SHA{"pushed"}},
		},
		{
			name: "neither merged nor base gone",
			in:   plan.Input{Obs: plan.Observation{PRs: openPRs(blocker, child)}, Runs: ran},
			want: plan.Removal{Refusal: "neither merged nor base gone"},
		},
		{
			name: "agent awaiting an answer holds its worktree even when merged",
			in: plan.Input{
				Obs: plan.Observation{PRs: map[plan.BranchRef]plan.PR{key(child.Branch): {State: plan.Merged}}},
				Runs: map[plan.TicketURL]plan.RunSummary{
					child.URL: {Outcome: option.Some(plan.OutcomeAsked)},
				},
			},
			want: plan.Removal{Refusal: plan.HeldWorktreeRefusal},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.in.Tickets = []plan.Ticket{blocker, child}
			got := derive(plan.Rules{}, tt.in).Removal(child.URL)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Removal(%s) mismatch (-want +got):\n%s", child.URL, diff)
			}
		})
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

func TestSnapshot_Session(t *testing.T) {
	t.Parallel()
	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second")
	snap := derive(plan.Rules{}, plan.Input{
		Tickets:  []plan.Ticket{first, second},
		Sessions: map[plan.TicketURL]string{first.URL: "abc"},
	})

	if got, ok := snap.Session(first.URL); !ok || got != "abc" {
		t.Errorf("Session(%s) = %q, %t, want abc, true", first.URL, got, ok)
	}
	if got, ok := snap.Session(second.URL); ok {
		t.Errorf("Session(%s) = %q, true, want none", second.URL, got)
	}
}

func TestSnapshot_Pushable(t *testing.T) {
	t.Parallel()
	tk := ticket("1", "cc-1-first")
	pushed := plan.Push{Ticket: tk.URL, LocalTip: "def"}
	forced := plan.Push{Ticket: tk.URL, LocalTip: "def", Force: true}
	lastPush := func(tip plan.SHA) plan.VerdictFacts {
		return plan.VerdictFacts{PushRows: map[plan.TicketURL]plan.PushRow{tk.URL: {PushedTip: tip}}}
	}
	run := func(outcome plan.Outcome) map[plan.TicketURL]plan.RunSummary {
		return map[plan.TicketURL]plan.RunSummary{tk.URL: {Outcome: option.Some(outcome)}}
	}

	tests := []struct {
		name  string
		in    plan.Input
		alive bool
		noTip bool
		want  []plan.Push
	}{
		{name: "never pushed", in: plan.Input{Runs: run(plan.OutcomePush)}, want: []plan.Push{pushed}},
		{name: "outcome is not push", in: plan.Input{Runs: run(plan.OutcomeAsked)}},
		{name: "no run", in: plan.Input{}},
		{name: "agent alive", in: plan.Input{Runs: run(plan.OutcomePush)}, alive: true},
		{name: "no local tip", in: plan.Input{Runs: run(plan.OutcomePush)}, noTip: true},
		{
			name: "push failed",
			in: plan.Input{
				Runs: run(plan.OutcomePush), Pushes: map[plan.TicketURL]plan.PushFact{tk.URL: {Failed: true}},
			},
		},
		{
			name: "push refused",
			in: plan.Input{
				Runs: run(plan.OutcomePush), Pushes: map[plan.TicketURL]plan.PushFact{tk.URL: {Refused: true}},
			},
		},
		{
			name: "verification failed",
			in: plan.Input{
				Runs:      run(plan.OutcomePush),
				Refreshes: map[plan.TicketURL]plan.RefreshFact{tk.URL: {VerificationFailed: true}},
			},
		},
		{name: "tip unchanged since last push", in: plan.Input{Runs: run(plan.OutcomePush), Verdict: lastPush("def")}},
		{
			name: "tip moved past last push",
			in:   plan.Input{Runs: run(plan.OutcomePush), Verdict: lastPush("abc")},
			want: []plan.Push{pushed},
		},
		{
			name: "restack forces",
			in: plan.Input{
				Runs: run(plan.OutcomePush), Verdict: lastPush("abc"),
				Restacked: map[plan.TicketURL]bool{tk.URL: true},
			},
			want: []plan.Push{forced},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := plan.NewObservation(time.Time{})
			if !tt.noTip {
				obs.LocalTips[key(tk.Branch)] = "def"
			}
			obs.Runs[tk.URL] = plan.RunObservation{Alive: tt.alive}
			tt.in.Tickets = []plan.Ticket{tk}
			tt.in.Obs = obs
			got := derive(plan.Rules{}, tt.in).Pushable(obs)
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Pushable() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSnapshot_Pushable_Body(t *testing.T) {
	t.Parallel()
	root := ticket("1", "cc-1-first")
	child := ticket("2", "cc-2-second", root.URL)
	o := plan.NewObservation(time.Time{})
	o.LocalTips[key(child.Branch)] = "def"
	o.PRs[key(root.Branch)] = plan.PR{Number: 7, State: plan.Open}
	in := plan.Input{
		Tickets: []plan.Ticket{root, child}, Obs: o,
		Runs: map[plan.TicketURL]plan.RunSummary{child.URL: {Outcome: option.Some(plan.OutcomePush)}},
	}
	rules := plan.Rules{Stacking: map[string]bool{"r": true}}
	got := derive(rules, in).Pushable(o)
	want := []plan.Push{{Ticket: child.URL, LocalTip: "def", Body: "Merge after #7"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Pushable() mismatch (-want +got):\n%s", diff)
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
			intents: []plan.Intent{
				{ID: 1, Ticket: child.URL, Verb: plan.VerbRefresh},
				{ID: 2, Ticket: child.URL, Verb: plan.VerbRefresh},
			},
			want: []plan.Refresh{{Ticket: child.URL, Row: row, Requested: true}},
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

func TestSnapshot_RepoGates(t *testing.T) {
	t.Parallel()
	snap := derive(plan.Rules{}, plan.Input{
		Repos: []plan.RepoStatus{
			{Name: "ready", Phase: plan.RepoReady},
			{Name: "cloning", Phase: plan.RepoCloning},
			{Name: "merge", Phase: plan.RepoRefused, RefusalKind: plan.RefusalMergeSettings, Refusal: "no merge commits"},
			{Name: "parse", Phase: plan.RepoRefused, RefusalKind: plan.RefusalSettingsParse, Refusal: "bad toml"},
		},
		Obs: plan.Observation{
			SettingsErrors: map[string]string{"parse": "bad toml"},
			RepoErrors:     map[string]string{"errored": "fetch failed"},
		},
	})

	wantSkipped := map[string]bool{"cloning": true, "merge": true, "parse": true, "errored": true}
	if diff := cmp.Diff(wantSkipped, snap.SkippedRepos()); diff != "" {
		t.Errorf("SkippedRepos() mismatch (-want +got):\n%s", diff)
	}
	wantProblems := []string{
		"merge refused (" + string(plan.RefusalMergeSettings) + "): no merge commits",
		"observe errored: fetch failed",
		"settings for parse: bad toml",
	}
	if diff := cmp.Diff(wantProblems, snap.RepoProblems()); diff != "" {
		t.Errorf("RepoProblems() mismatch (-want +got):\n%s", diff)
	}
}
