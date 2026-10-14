package loop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestARestackThatFailsVerificationReadsVerificationFailedAndIsNotPushed(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.root, p.Repo, p.Store
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	verifyCommand := []string{"sh", "-c", "echo 'undefined: dup' >&2; exit 1"}
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: verifyCommand}
	clock := fixedClock(at.Add(time.Minute))
	p.cfg, p.ws = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.tick(t)

	events := p.events(t, "verification_failed")
	if len(events) != 1 || events[0].TicketURL != f.child.URL ||
		!strings.Contains(events[0].Detail, "sh -c") || !strings.Contains(events[0].Detail, "undefined: dup") {
		t.Errorf("events = %+v, want a verification_failed event naming the command", events)
	}
	for _, e := range p.events(t, "pushed") {
		if e.TicketURL == f.child.URL {
			t.Errorf("pushed event = %+v, want none for %s", e, f.child.URL)
		}
	}

	remoteChildTip := gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/child")
	if remoteChildTip != childTip0 {
		t.Errorf("remote child tip = %s, want unchanged %s: a failed verification must not push", remoteChildTip, childTip0)
	}

	server := openServer(store, clock, "")
	page := renderPage(t, server)
	if state := rowState(t, page, f.child.URL); state != "verification_failed" {
		t.Fatalf("child's state = %q, want verification_failed", state)
	}
	for _, verb := range []plan.Verb{plan.VerbRetryPush, plan.VerbReRun} {
		if !strings.Contains(page, `value="`+string(verb)+`"`) {
			t.Errorf("page does not offer %s:\n%s", verb, page)
		}
	}
}

func TestARepoWithNoVerifyCommandConfiguredIsUnaffected(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.root, p.Repo, p.Store
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.cfg, p.ws = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.tick(t)

	if events := p.events(t, "verification_failed"); len(events) != 0 {
		t.Errorf("events = %+v, want no verification_failed event: no command was configured", events)
	}
	childTip := p.head(t, f.childWorktree, "HEAD")
	events := p.events(t, "pushed")
	childPushed := false
	for _, event := range events {
		if event.TicketURL == f.child.URL && event.Detail == "pushed "+childTip+" to origin/parent" {
			childPushed = true
		}
	}
	if !childPushed {
		t.Errorf("events = %+v, want the merge pushed as normal", events)
	}
}

func TestVerificationRunsOnTheRestackNotOnAnAlreadyVerifiedTip(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.root, p.Repo, p.Store
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	countFile := filepath.Join(t.TempDir(), "count")
	countScript := "n=$(cat \"" + countFile + "\" 2>/dev/null || echo 0); " +
		"echo $((n+1)) > \"" + countFile + "\"; exit 1"
	command := []string{"sh", "-c", countScript}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: command}
	p.cfg, p.ws = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.tick(t)
	p.tick(t)

	got, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("ReadFile count: %v", err)
	}
	if want := "1\n"; string(got) != want {
		t.Errorf("verify command ran %q times, want %q: the automatic pass must not reverify "+
			"an already-failed tip every tick", got, want)
	}
}

func TestRetryPushAfterAFailedVerificationClearsTheLatch(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath, store := p.root, p.Repo, p.Store
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	*p.Obs = baseObservation(f, parentTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: []string{"false"}}
	p.cfg, p.ws = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.tick(t)

	facts, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatalf("RefreshFacts: %v", err)
	}
	if !facts[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Fatalf("refresh facts = %+v, want %s verification failed", facts, f.child.URL)
	}

	if err := store.QueueVerbIntent(t.Context(), f.child.URL, plan.VerbRetryPush, at.Add(time.Hour)); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	p.tick(t)

	events := p.events(t, "pushed")
	childTip := p.head(t, f.childWorktree, "HEAD")
	pushedChild := false
	for _, e := range events {
		if e.TicketURL == f.child.URL && e.Detail == "pushed "+childTip+" to origin/parent" {
			pushedChild = true
		}
	}
	if !pushedChild {
		t.Errorf("events = %+v, want retry-push to push %s despite the failed verification", events, f.child.URL)
	}

	factsAfter, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatalf("RefreshFacts: %v", err)
	}
	if factsAfter[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Errorf("refresh facts = %+v, want the latch cleared by the push", factsAfter)
	}
}

func TestTwoIndependentAdditionsOfTheSameHelperMergeCleanlyButFailGoVet(t *testing.T) {
	t.Parallel()
	p := newPushFixture(t)
	root, repoPath := p.root, p.Repo
	gittest.Commit(t, repoPath, "go.mod", "module fixture\n\ngo 1.21\n")
	gittest.Commit(t, repoPath, "helpers.go", "package fixture\n\nfunc Base() {}\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "main")

	store := p.Store
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	f := newStackedFixture(t, repoPath, store, at)

	// The parent's own PR adds Dup at the bottom of the file.
	gittest.Commit(t, f.parentWorktree, "helpers.go", "package fixture\n\nfunc Base() {}\n\nfunc Dup() {}\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "parent")

	// Main independently advanced (another PR squash-merged) with the same helper inserted at the
	// top: a disjoint line range from the parent's own change, so the merge below has no conflict.
	advanceMain(t, root, "helpers.go", "package fixture\n\nfunc Dup() {}\n\nfunc Base() {}\n")
	gittest.Run(t, repoPath, "fetch", "-q", "origin", "main")
	mainTip1 := gittest.Run(t, repoPath, "rev-parse", "refs/remotes/origin/main")

	remoteParentTip0 := gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/parent")

	*p.Obs = baseObservation(f, f.parentTip0)
	p.Obs.BranchTips[branchRef("repo", "main")] = plan.SHA(mainTip1)

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("LookPath go: %v", err)
	}
	vet := []string{goBinary, "vet", "./..."}
	p.Obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: vet}
	clock := fixedClock(at.Add(time.Minute))
	p.cfg, p.ws = cfg, ws
	p.reset(at.Add(time.Minute), runner.ProcessRunner{})
	p.tick(t)

	merged, err := os.ReadFile(filepath.Join(f.parentWorktree, "helpers.go"))
	if err != nil {
		t.Fatalf("ReadFile merged helpers: %v", err)
	}
	if strings.Count(string(merged), "func Dup()") != 2 {
		t.Fatalf("merged helpers.go = %q, want Dup declared twice: the merge must produce "+
			"#85's clean-but-broken result", merged)
	}

	events := p.events(t, "verification_failed")
	if len(events) != 1 || events[0].TicketURL != f.parent.URL || !strings.Contains(events[0].Detail, "Dup redeclared") {
		t.Errorf("events = %+v, want a verification_failed event naming go vet's redeclaration error", events)
	}

	remoteParentTip1 := gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/parent")
	if remoteParentTip1 != remoteParentTip0 {
		t.Errorf("remote parent tip = %s, want unchanged %s: a failed verification must not push",
			remoteParentTip1, remoteParentTip0)
	}

	server := openServer(store, clock, "")
	if state := rowState(t, renderPage(t, server), f.parent.URL); state != "verification_failed" {
		t.Fatalf("parent's state = %q, want verification_failed", state)
	}
}
