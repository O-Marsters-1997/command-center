package cc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// writeVerifyScript writes an executable shell script and returns its path -- a repo's verify_command in these tests.
func writeVerifyScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verify.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestARestackThatFailsVerificationReadsVerificationFailedAndIsNotPushed(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	childTip0 := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	verifyScript := writeVerifyScript(t, "#!/bin/sh\necho 'undefined: dup' >&2\nexit 1\n")
	cfg.Repos[0].VerifyCommand = []string{verifyScript}
	clock := fixedClock(at.Add(time.Minute))
	loop := cc.NewLoop(store, observe, clock, cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "verification_failed", verifyScript) {
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

	remoteChildTip := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/child"))
	if remoteChildTip != childTip0 {
		t.Errorf("remote child tip = %s, want unchanged %s: a failed verification must not push", remoteChildTip, childTip0)
	}

	server := cc.NewServer(store, clock, cfg.Repos, "")
	page := renderPage(t, server)
	if state := rowState(t, page, f.child.URL); state != "verification_failed" {
		t.Fatalf("child's state = %q, want verification_failed", state)
	}
	for _, verb := range []string{plan.VerbRetryPush, plan.VerbReRun} {
		if !strings.Contains(page, `value="`+verb+`"`) {
			t.Errorf("page does not offer %s:\n%s", verb, page)
		}
	}
}

func TestARepoWithNoVerifyCommandConfiguredIsUnaffected(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root) // VerifyCommand left unset
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
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
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	countFile := filepath.Join(t.TempDir(), "count")
	script := writeVerifyScript(t, "#!/bin/sh\n"+
		"n=$(cat \""+countFile+"\" 2>/dev/null || echo 0)\n"+
		"echo $((n+1)) > \""+countFile+"\"\n"+
		"exit 1\n")

	cfg, ws := stackedConfigAndWorkspace(t, root)
	cfg.Repos[0].VerifyCommand = []string{script}
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if err := loop.RunOnce(t.Context()); err != nil {
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
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	cfg.Repos[0].VerifyCommand = []string{writeVerifyScript(t, "#!/bin/sh\nexit 1\n")}
	clock := fixedClock(at.Add(time.Minute))
	loop := cc.NewLoop(store, observe, clock, cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	facts, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !facts[f.child.URL].VerificationFailed {
		t.Fatalf("refresh facts = %+v, want %s verification failed", facts, f.child.URL)
	}

	if err := store.QueueVerbIntent(t.Context(), f.child.URL, plan.VerbRetryPush, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(t.Context()); err != nil {
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
	if factsAfter[f.child.URL].VerificationFailed {
		t.Errorf("refresh facts = %+v, want the latch cleared by the push", factsAfter)
	}
}

// TestTwoIndependentAdditionsOfTheSameHelperMergeCleanlyButFailGoVet reproduces issue #85's first
// mechanism: two independently-authored insertions at different points in the same file merge
// with no conflict markers at all, and only a real build or vet catches the redeclaration.
func TestTwoIndependentAdditionsOfTheSameHelperMergeCleanlyButFailGoVet(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	commitFile(t, repoPath, "go.mod", "module fixture\n\ngo 1.21\n")
	commitFile(t, repoPath, "helpers.go", "package fixture\n\nfunc Base() {}\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "main")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	f := newStackedFixture(t, repoPath, store, at)

	// The parent's own PR adds Dup at the bottom of the file.
	commitFile(t, f.parentWorktree, "helpers.go", "package fixture\n\nfunc Base() {}\n\nfunc Dup() {}\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "parent")

	// Main independently advanced (another PR squash-merged) with the same helper inserted at the
	// top: a disjoint line range from the parent's own change, so the merge below has no conflict.
	advanceMain(t, root, "helpers.go", "package fixture\n\nfunc Dup() {}\n\nfunc Base() {}\n")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin", "main")
	mainTip1 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/remotes/origin/main"))

	remoteParentTip0 := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/parent"))

	obs := baseObservation(f, f.parentTip0)
	obs.BranchTips["repo//main"] = mainTip1
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	cfg.Repos[0].VerifyCommand = []string{mustLookPath(t, "go"), "vet", "./..."}
	clock := fixedClock(at.Add(time.Minute))
	loop := cc.NewLoop(store, observe, clock, cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
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

	remoteParentTip1 := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/parent"))
	if remoteParentTip1 != remoteParentTip0 {
		t.Errorf("remote parent tip = %s, want unchanged %s: a failed verification must not push",
			remoteParentTip1, remoteParentTip0)
	}

	server := cc.NewServer(store, clock, cfg.Repos, "")
	if state := rowState(t, renderPage(t, server), f.parent.URL); state != "verification_failed" {
		t.Fatalf("parent's state = %q, want verification_failed", state)
	}
}
