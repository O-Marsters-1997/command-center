package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

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
