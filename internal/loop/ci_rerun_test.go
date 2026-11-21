package loop_test

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

const (
	lintRun   = "https://github.com/o/r/actions/runs/11/job/1"
	buildRun  = "https://github.com/o/r/actions/runs/11/job/2"
	passedRun = "https://github.com/o/r/actions/runs/12/job/3"
)

func failedCheck(url string) gh.CheckState {
	return gh.CheckState{Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: url}
}

func TestRerunFailedChecks(t *testing.T) {
	t.Parallel()

	t.Run("observe reads Attempt only for failed checks that name an Actions run", func(t *testing.T) {
		t.Parallel()
		root, _ := repoWithOrigin(t)
		cfg, _ := testConfigAndWorkspace(t, root, 1, []string{"true"})
		st := openStore(t)
		trackRepo(t, st, "repo", filepath.Join(root, "remote.git"))
		storetest.Tickets(t, st, sandboxTicket("1"))
		forge := &ghtest.Forge{}
		forge.Seed(ghtest.Seed{PRs: []gh.PR{{
			Number: 1, HeadRef: "cc-1", State: gh.Open, Checks: map[string]gh.CheckState{
				"Lint":   failedCheck(lintRun),
				"Test":   {Status: "COMPLETED", Conclusion: "SUCCESS", DetailsURL: passedRun},
				"Status": failedCheck("https://example.com/status/1"),
			},
		}}})

		obs, err := loop.NewObserver(st, forge, cfg)(t.Context())
		if err != nil {
			t.Fatalf("observe() error = %v", err)
		}
		got := map[string]int{}
		for name, c := range obs.PRs[branchRef("repo", "cc-1")].Checks {
			got[name] = c.Attempt
		}
		if diff := cmp.Diff(map[string]int{"Lint": 1, "Test": 0, "Status": 0}, got); diff != "" {
			t.Errorf("check attempts mismatch (-want +got):\n%s", diff)
		}
		if n := len(forge.Calls("Attempt")); n != 1 {
			t.Errorf("Attempt calls = %d, want 1", n)
		}
	})

	t.Run("reruns a check failed on attempt 1 once, across a restart", func(t *testing.T) {
		t.Parallel()
		root, _ := repoWithOrigin(t)
		cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
		st := openStore(t)
		trackRepo(t, st, "repo", filepath.Join(root, "remote.git"))
		storetest.Tickets(t, st, sandboxTicket("1"))
		forge := &ghtest.Forge{}
		forge.Seed(ghtest.Seed{PRs: []gh.PR{{
			Number: 1, HeadRef: "cc-1", State: gh.Open, Checks: map[string]gh.CheckState{
				"Lint": failedCheck(lintRun), "Build": failedCheck(buildRun),
			},
		}}})
		observe := loop.NewObserver(st, forge, cfg)

		for range 3 {
			lp := newLoop(st, observe, fixedClock(testAt), cfg, ws, &runnertest.Runner{}, loop.WithForge(forge))
			runOnce(t, lp)
		}
		if n := len(forge.Calls("Rerun")); n != 1 {
			t.Errorf("Rerun calls across three ticks and restarts = %d, want 1", n)
		}
	})

	t.Run("never reruns a check failed on attempt 2", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.Obs.PRs[branchRef("repo", "cc-1")] = plan.PR{Number: 1, State: plan.Open, Checks: map[string]plan.CheckState{
			"Lint": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: lintRun, Attempt: 2},
		}}
		f.tick(t)
		if n := len(f.Forge.Calls("Rerun")); n != 0 {
			t.Errorf("Rerun calls = %d, want 0", n)
		}
	})
}
