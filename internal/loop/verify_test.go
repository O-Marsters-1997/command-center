package loop_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func TestVerify(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, verify []string) (*pushFixture, *runnertest.Runner, stackedFixture, string) {
		t.Helper()
		p := newPushFixture(t)
		f := newStackedFixture(t, p.Repo, p.Store, testAt)
		parentTip := advanceParent(t, p.Repo, f)
		originalTip := gittest.Run(t, filepath.Join(p.root, "remote.git"), "rev-parse", "refs/heads/child")
		*p.Obs = baseObservation(f, parentTip)
		cfg, ws := testConfigAndWorkspace(t, p.root, 0, nil)
		p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: verify}
		p.cfg, p.ws = cfg, ws
		fake := &runnertest.Runner{}
		p.reset(testAt.Add(time.Minute), fake)
		return p, fake, f, originalTip
	}
	remoteTip := func(t *testing.T, p *pushFixture) string {
		t.Helper()
		return gittest.Run(t, filepath.Join(p.root, "remote.git"), "rev-parse", "refs/heads/child")
	}
	rowState := func(t *testing.T, p *pushFixture, f stackedFixture) string {
		t.Helper()
		page := renderPage(t, openServer(p.Store, fixedClock(testAt.Add(time.Minute)), ""))
		return webtest.RowFor(t, page, f.child.URL).State
	}

	t.Run("a refresh spawns the verify and a failure holds the push", func(t *testing.T) {
		t.Parallel()
		p, fake, f, originalTip := setup(t, []string{"false"})
		p.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 || !slices.Equal(spawns[0].AgentCommand, []string{"false"}) {
			t.Fatalf("spawns = %+v, want one verify command", spawns)
		}
		if got := rowState(t, p, f); got != "Verifying" {
			t.Errorf("row state = %q, want Verifying", got)
		}

		p.Obs.LocalTips = map[plan.BranchRef]plan.SHA{
			branchRef("repo", "child"): plan.SHA(p.head(t, f.childWorktree, "HEAD")),
		}
		fake.Exit(1, 1)
		p.tick(t)
		p.tick(t)
		if got := p.events(t, "verification_failed"); len(got) != 0 {
			t.Errorf("verification_failed events = %+v, want none", got)
		}
		if got := len(fake.Spawns()); got != 1 {
			t.Errorf("spawns after the failure = %d, want the failed tip not reverified", got)
		}
		if got := remoteTip(t, p); got != originalTip {
			t.Errorf("remote parent tip = %s, want unchanged %s", got, originalTip)
		}
		if got := rowState(t, p, f); got != "Verification failed" {
			t.Errorf("row state = %q, want Verification failed", got)
		}
	})

	t.Run("a passed verify lets the push through", func(t *testing.T) {
		t.Parallel()
		p, fake, f, originalTip := setup(t, []string{"true"})
		p.tick(t)
		if got := remoteTip(t, p); got != originalTip {
			t.Fatalf("remote parent tip = %s while verifying, want unchanged %s", got, originalTip)
		}
		fake.Exit(1, 0)
		p.tick(t)
		if got := remoteTip(t, p); got == originalTip {
			t.Errorf("remote parent tip = %s, want the child pushed after its verify passed (%s)", got, f.child.URL)
		}
	})

	t.Run("a repo with no verify command spawns nothing", func(t *testing.T) {
		t.Parallel()
		p, fake, _, originalTip := setup(t, nil)
		p.tick(t)
		if got := len(fake.Spawns()); got != 0 {
			t.Errorf("spawns = %d, want 0", got)
		}
		if got := remoteTip(t, p); got == originalTip {
			t.Errorf("remote parent tip = %s, want the merge pushed as normal", got)
		}
	})

	t.Run("a restart mid-verify neither respawns nor pushes the tip", func(t *testing.T) {
		t.Parallel()
		p, fake, _, originalTip := setup(t, []string{"true"})
		p.tick(t)
		p.reset(testAt.Add(2*time.Minute), fake)
		p.tick(t)
		if got := len(fake.Spawns()); got != 1 {
			t.Errorf("spawns across a restart = %d, want 1", got)
		}
		if got := remoteTip(t, p); got != originalTip {
			t.Errorf("remote parent tip = %s, want unchanged %s while the verify runs", got, originalTip)
		}
	})
}
