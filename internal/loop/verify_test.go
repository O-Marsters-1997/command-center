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
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestARestackThatFailsVerificationReadsVerificationFailedAndIsNotPushed(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	verifyCommand := []string{"sh", "-c", "echo 'undefined: dup' >&2; exit 1"}
	obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: verifyCommand}
	clock := fixedClock(at.Add(time.Minute))
	lp := newLoop(store, observe, clock, cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "verification_failed", "sh -c") {
		t.Errorf("events = %+v, want a verification_failed event naming the command", events)
	}
	if !hasEvent(events, "verification_failed", "undefined: dup") {
		t.Errorf("events = %+v, want a verification_failed event naming what came back", events)
	}
	for _, e := range events {
		if e.TicketURL == f.child.URL && e.Kind == "pushed" {
			t.Errorf("events = %+v, want no pushed event for %s: a failed verification must not reach origin",
				events, f.child.URL)
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
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root) // VerifyCommand left unset
	lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "verification_failed", "") {
		t.Errorf("events = %+v, want no verification_failed event: no command was configured", events)
	}
	if !hasEvent(events, "pushed", "") {
		t.Errorf("events = %+v, want the merge pushed as normal", events)
	}
}

func TestVerificationRunsOnTheRestackNotOnAnAlreadyVerifiedTip(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	countFile := filepath.Join(t.TempDir(), "count")
	countScript := "n=$(cat \"" + countFile + "\" 2>/dev/null || echo 0); " +
		"echo $((n+1)) > \"" + countFile + "\"; exit 1"
	command := []string{"sh", "-c", countScript}

	cfg, ws := stackedConfigAndWorkspace(t, root)
	obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: command}
	lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	got, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1\n"; string(got) != want {
		t.Errorf("verify command ran %q times, want %q: the automatic pass must not reverify "+
			"an already-failed tip every tick", got, want)
	}
}

func TestRetryPushAfterAFailedVerificationClearsTheLatch(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	store := openStore(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: []string{"false"}}
	clock := fixedClock(at.Add(time.Minute))
	lp := newLoop(store, observe, clock, cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	facts, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !facts[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Fatalf("refresh facts = %+v, want %s verification failed", facts, f.child.URL)
	}

	if err := store.QueueVerbIntent(t.Context(), f.child.URL, plan.VerbRetryPush, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pushedChild := false
	for _, e := range events {
		if e.TicketURL == f.child.URL && e.Kind == "pushed" {
			pushedChild = true
		}
	}
	if !pushedChild {
		t.Errorf("events = %+v, want retry-push to push %s despite the failed verification", events, f.child.URL)
	}

	factsAfter, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if factsAfter[plan.TicketURL(f.child.URL)].VerificationFailed {
		t.Errorf("refresh facts = %+v, want the latch cleared by the push", factsAfter)
	}
}

func TestTwoIndependentAdditionsOfTheSameHelperMergeCleanlyButFailGoVet(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	gittest.Commit(t, repoPath, "go.mod", "module fixture\n\ngo 1.21\n")
	gittest.Commit(t, repoPath, "helpers.go", "package fixture\n\nfunc Base() {}\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "main")

	store := openStore(t)
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

	obs := baseObservation(f, f.parentTip0)
	obs.BranchTips[branchRef("repo", "main")] = plan.SHA(mainTip1)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	vet := []string{goBinary, "vet", "./..."}
	obs.Settings["repo"] = config.RepoSettings{Stacking: true, VerifyCommand: vet}
	clock := fixedClock(at.Add(time.Minute))
	lp := newLoop(store, observe, clock, cfg, ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	merged, err := os.ReadFile(filepath.Join(f.parentWorktree, "helpers.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(merged), "func Dup()") != 2 {
		t.Fatalf("merged helpers.go = %q, want Dup declared twice: the merge must produce "+
			"#85's clean-but-broken result", merged)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "verification_failed", "Dup redeclared") {
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
