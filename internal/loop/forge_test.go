package loop_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

type fakeForge struct {
	t       *testing.T
	remote  string
	created bool
}

func (f *fakeForge) List(_ context.Context, _ string, tracked []string) (gh.Snapshot, error) {
	byBranch := map[string]gh.PR{}
	if f.created {
		for _, branch := range tracked {
			byBranch[branch] = gh.PR{
				Number: 1, HeadRef: branch, State: gh.Open,
				HeadOid: strings.TrimSpace(runGitOutput(f.t, "-C", f.remote, "rev-parse", "refs/heads/"+branch)),
				Checks:  map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			}
		}
	}
	return gh.Snapshot{ByBranch: byBranch}, nil
}

func (*fakeForge) IssueTitles(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (f *fakeForge) Create(context.Context, string, string, string, bool) error {
	f.created = true
	return nil
}

func (*fakeForge) Ready(context.Context, string, string) error        { return nil }
func (*fakeForge) Edit(context.Context, string, string, string) error { return nil }
func (*fakeForge) CloseIssue(context.Context, string, string) error   { return nil }

func pathWithGitAndTpOnly(t *testing.T) {
	t.Helper()
	installFakeTp(t, false)
	bin := t.TempDir()
	for _, name := range []string{"tp", "git"} {
		tool, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(tool, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

func TestALoopDrivesATicketFromReadyToReviewMeWithNoGhBinary(t *testing.T) {
	root, _ := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	pushSettingsFile(t, config.CheckoutPath(root, "repo"), "[checks]\nsuccess = \"CI\"\n")

	issue := tracker.Ticket{URL: "https://github.com/acme/repo/issues/1", Number: 1, Title: "Add x"}
	source := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets:  map[string][]tracker.Ticket{"project:x": {issue}},
	}
	resolve := func(tracker.Kind, string) (tracker.Source, error) { return source, nil }

	store := openStore(t)
	trackRepo(t, store, "repo", "git@github.com:acme/repo.git")
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	forge := &fakeForge{t: t, remote: filepath.Join(root, "remote.git")}
	runner := runner.NewFake()
	lp := loop.NewLoop(store, loop.NewObserver(store, forge, cfg), fixedClock(at), cfg, ws, runner)
	lp.SetForge(forge)
	lp.SetTrackerSource(resolve)

	if err := store.QueueVerbIntent(t.Context(), "project:x", storepkg.ImportVerb, at); err != nil {
		t.Fatal(err)
	}
	authoriseTicket(t, store, issue.URL, plan.Hash(plan.Compose(plan.Ticket{URL: issue.URL})), at)

	tick := func() {
		t.Helper()
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
	}
	state := func() string {
		t.Helper()
		return rowState(t, renderPage(t, web.NewServer(store, fixedClock(at), "")), issue.URL)
	}

	tick()
	if len(runner.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the imported ticket should have launched", len(runner.Spawns))
	}
	commitFile(t, runner.Spawns[0].WorktreePath, "x.go", "package x\n")

	runner.Alive[1] = false
	runner.CanReap[1] = true
	for range 4 {
		tick()
	}

	if got := state(); got != "review_me" {
		t.Errorf("row state = %q, want review_me", got)
	}
	if !forge.created {
		t.Error("the fake Forge never saw a Create: the push opened its PR some other way")
	}

	obs, _, err := store.LastObservation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.LocalTips) == 0 {
		t.Error("the observation holds no local tips: observe must read them for the push step")
	}
}
