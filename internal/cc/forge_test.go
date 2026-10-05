package cc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// fakeForge is an in-memory GitHub: Create opens a green PR for every tracked branch.
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

func (*fakeForge) Ready(context.Context, string, string) error                      { return nil }
func (*fakeForge) Edit(context.Context, string, string, string) error               { return nil }
func (*fakeForge) Close(context.Context, string, string) error                      { return nil }
func (*fakeForge) Rerun(context.Context, string, string) error                      { return nil }
func (*fakeForge) RunViewLogFailed(context.Context, string, string) (string, error) { return "", nil }
func (*fakeForge) CloseIssue(context.Context, string, string) error                 { return nil }

// pathWithGitAndTpOnly leaves a PATH holding git and the fake tp but no gh, so a call that
// reaches the gh binary fails the test instead of passing on the developer's machine.
func pathWithGitAndTpOnly(t *testing.T) {
	t.Helper()
	installFakeTp(t, false)
	tp, err := exec.LookPath("tp")
	if err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, tool := range []string{tp, git} {
		if err := os.Symlink(tool, filepath.Join(bin, filepath.Base(tool))); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

func TestALoopDrivesATicketFromReadyToReviewMeWithNoGhBinary(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin and PATH use t.Setenv.
	root, _ := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	cfg.Repos[0].Remote = "git@github.com:acme/repo.git"
	cfg.Repos[0].Tracker = string(tracker.GitHub)
	cfg.Repos[0].Checks = verdict.Predicate{Success: "CI"}

	issue := tracker.Ticket{URL: "https://github.com/acme/repo/issues/1", Number: 1, Title: "Add x"}
	source := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets:  map[string][]tracker.Ticket{"project:x": {issue}},
	}
	resolve := func(tracker.Kind, string) (tracker.Source, error) { return source, nil }

	store := openStore(t)
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	forge := &fakeForge{t: t, remote: filepath.Join(root, "remote.git")}
	runner := newFakeRunner()
	loop := cc.NewLoop(store, cc.NewObserver(store, forge, cfg), fixedClock(at), cfg, ws, runner)
	loop.SetForge(forge)
	loop.SetTrackerSource(resolve)

	if err := cc.QueueImport(t.Context(), store, "project:x", at); err != nil {
		t.Fatal(err)
	}
	authoriseTicket(t, store, issue.URL, plan.Hash(plan.Compose(plan.Ticket{URL: issue.URL})), at)

	tick := func() {
		t.Helper()
		if err := loop.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
	}
	state := func() string {
		t.Helper()
		return rowState(t, renderPage(t, cc.NewServer(store, fixedClock(at), cfg.Repos, "")), issue.URL)
	}

	tick()
	if len(runner.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the imported ticket should have launched", len(runner.spawns))
	}
	commitFile(t, runner.spawns[0].WorktreePath, "x.go", "package x\n")

	runner.alive[1] = false
	runner.canReap[1] = true
	for range 4 {
		tick()
	}

	if got := state(); got != "review_me" {
		t.Errorf("row state = %q, want review_me", got)
	}
	if !forge.created {
		t.Error("the fake Forge never saw a Create: the push opened its PR some other way")
	}
}
