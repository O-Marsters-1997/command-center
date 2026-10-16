package plan_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

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
