package cc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// stackedConfigAndWorkspace mirrors testConfigAndWorkspace (fakes_test.go) but with stacking on:
// refresh's own base recompute (plan.Unlocked) only ever names a non-main base under stacking.
func stackedConfigAndWorkspace(t *testing.T, root string) (cc.Config, cc.Workspace) {
	t.Helper()
	cfg := cc.Config{
		MaxAgents: 0,
		Repos:     []cc.Repo{{Name: "repo", Path: "repo", Stacking: true}},
	}
	ws := cc.Workspace{
		Root:         root,
		RunsDir:      t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
	}
	return cfg, ws
}

// forcePushDivergentCommit pushes a commit sharing no history with branch, from a throwaway
// clone, so the branch's local worktree can never fast-forward onto it.
func forcePushDivergentCommit(t *testing.T, root, branch string) {
	t.Helper()
	remote := filepath.Join(root, "remote.git")
	clone := filepath.Join(t.TempDir(), "divergent-clone")
	runGit(t, "clone", "-q", remote, clone)
	runGit(t, "-C", clone, "checkout", "-q", "-b", branch, "origin/main")
	if err := os.WriteFile(filepath.Join(clone, "divergent.txt"), []byte("someone else's history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", clone, "add", "divergent.txt")
	runGit(t, "-C", clone, "commit", "-q", "-m", "diverging commit")
	runGit(t, "-C", clone, "push", "-q", "--force", "origin", "HEAD:"+branch)
}

// stackedFixture cuts a parent branch and a child stacked on it, both pushed and recorded as
// tasks. Their commits diverge from the common ancestor, so a refresh merge is a genuine
// three-way merge rather than a fast-forward.
type stackedFixture struct {
	parent, child                 cc.Task
	parentWorktree, childWorktree string
	parentTip0, mainSHA           string
}

func newStackedFixture(t *testing.T, repoPath string, store *cc.Store, at time.Time) stackedFixture {
	t.Helper()
	ctx := context.Background()

	parentWorktree := cutWorktree(t, repoPath, "parent")
	commitFile(t, parentWorktree, "parent.txt", "parent's original work\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "parent")
	parentTip0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/parent"))

	childWorktree := filepath.Join(t.TempDir(), "wt-child")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "child", childWorktree, "origin/parent")
	commitFile(t, childWorktree, "child.txt", "child's own work\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "child")
	childTip0 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))

	parent := cc.Task{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"}
	child := cc.Task{
		TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"},
	}
	if err := store.UpsertTasks(ctx, []cc.Task{parent, child}); err != nil {
		t.Fatal(err)
	}
	dispositionAsPushed(t, store, parent.TicketURL, at)
	dispositionAsPushed(t, store, child.TicketURL, at)
	mainSHA := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))
	if err := store.RecordPush(ctx, parent.TicketURL, parentTip0, "main", mainSHA, at); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, child.TicketURL, childTip0, "parent", parentTip0, at); err != nil {
		t.Fatal(err)
	}

	return stackedFixture{
		parent: parent, child: child,
		parentWorktree: parentWorktree, childWorktree: childWorktree, parentTip0: parentTip0, mainSHA: mainSHA,
	}
}

// advanceParent pushes a second commit onto the parent branch -- "the parent needs a fix" (PRD §
// Scenarios) -- and returns its new tip, which the child's recorded base_sha_at_push (parentTip0)
// no longer matches.
func advanceParent(t *testing.T, repoPath string, f stackedFixture) string {
	t.Helper()
	commitFile(t, f.parentWorktree, "parent-fix.txt", "the fix\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "parent")
	return strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/parent"))
}

func baseObservation(f stackedFixture, parentTip string) cc.Observation {
	return cc.Observation{
		Worktrees: map[string]string{"parent": f.parentWorktree, "child": f.childWorktree},
		PRs: map[string]gh.PR{
			"parent": {Number: 1, HeadRef: "parent", State: gh.Open},
			"child":  {Number: 2, HeadRef: "child", State: gh.Open},
		},
		BranchTips: map[string]string{"parent": parentTip, "main": f.mainSHA},
		Runs:       map[string]cc.RunObservation{},
		MidMerge:   map[string]bool{},
	}
}

func TestAutomaticRefreshMergesTheAdvancedParentAndThePushStepDeliversItSameTick(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	childHead := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))
	parents := strings.Fields(runGitOutput(t, "-C", f.childWorktree, "log", "-1", "--format=%P", childHead))
	if len(parents) != 2 {
		t.Fatalf("child HEAD has %d parents, want 2 (a merge commit): %v", len(parents), parents)
	}
	if _, err := os.Stat(filepath.Join(f.childWorktree, "parent-fix.txt")); err != nil {
		t.Errorf("child worktree does not contain the parent's fix after refresh: %v", err)
	}

	remoteChildTip := strings.TrimSpace(
		runGitOutput(t, "-C", filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/child"))
	if remoteChildTip != childHead {
		t.Errorf("remote child tip = %s, want %s: the merge commit must be pushed the same tick", remoteChildTip, childHead)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "refreshed", "") {
		t.Errorf("events = %+v, want a refreshed event", events)
	}
	if !hasEvent(events, "pushed", childHead) {
		t.Errorf("events = %+v, want a pushed event naming the merge commit %s", events, childHead)
	}

	pushes, err := store.LatestPushes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := pushes[f.child.TicketURL].BaseSHAAtPush; got != parentTip1 {
		t.Errorf("child's recorded base sha = %s, want the advanced parent tip %s", got, parentTip1)
	}
}

// TestAutomaticRefreshAlsoMergesAnAdvancedMainIntoARootRow covers issue #85's fourth incident: a
// row whose recorded base is main (never stacked, or retargeted there already) is exactly as
// stale as a still-stacked row once a sibling chain's own merge moves main, and baseMoved must
// fire for it the same way.
func TestAutomaticRefreshAlsoMergesAnAdvancedMainIntoARootRow(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	advanceMain(t, root)
	// advanceMain pushes from a throwaway clone, so repoPath's own tracking ref (what Merge below
	// resolves "origin/main" against) needs an explicit fetch -- unlike advanceParent, which pushes
	// straight from repoPath and so updates it as a push side effect.
	runGit(t, "-C", repoPath, "fetch", "-q", "origin", "main")
	mainTip1 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/remotes/origin/main"))

	obs := baseObservation(f, f.parentTip0)
	obs.BranchTips["main"] = mainTip1
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if _, err := os.Stat(filepath.Join(f.parentWorktree, "main-fix.txt")); err != nil {
		t.Errorf("parent worktree does not contain main's own advance after refresh: %v", err)
	}

	pushes, err := store.LatestPushes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := pushes[f.parent.TicketURL].BaseSHAAtPush; got != mainTip1 {
		t.Errorf("parent's recorded base sha = %s, want the advanced main tip %s", got, mainTip1)
	}
}

func TestAutomaticRefreshNeverTouchesAWorktreeWithALiveRun(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)
	childTip0 := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))

	obs := baseObservation(f, parentTip1)
	obs.Runs[f.child.TicketURL] = cc.RunObservation{Alive: true}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD")); got != childTip0 {
		t.Errorf("child worktree HEAD = %s, want unchanged %s: a live run must never see a git write (inv. 4)",
			got, childTip0)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(events, "refreshed", "") || hasEvent(events, "refresh_refused", "") {
		t.Errorf("events = %+v, want no refresh activity while a run is alive", events)
	}
}

func TestRefusedFastForwardReadsNeedsYouAndIsNotAutoRetriedButTheVerbRetries(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := advanceParent(t, repoPath, f)

	// Someone force-pushed different history to origin/child: the worktree's own branch, still on
	// its prior commit, can no longer fast-forward onto it.
	forcePushDivergentCommit(t, root, "child")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin")
	childTip0 := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))

	obs := baseObservation(f, parentTip1)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD")); got != childTip0 {
		t.Errorf("child worktree HEAD = %s, want unchanged %s: a refused fast-forward must not touch it", got, childTip0)
	}

	facts, err := store.RefreshFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !facts[f.child.TicketURL].Refused {
		t.Fatalf("refresh facts = %+v, want %s refused", facts, f.child.TicketURL)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "refresh_refused", "") {
		t.Errorf("events = %+v, want a refresh_refused event", events)
	}

	// A second automatic tick must not retry it.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	eventsAfter, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	refusalCount := 0
	for _, e := range eventsAfter {
		if e.Kind == "refresh_refused" {
			refusalCount++
		}
	}
	if refusalCount != 1 {
		t.Errorf("refresh_refused events = %d after a second automatic tick, want still 1: no auto-retry", refusalCount)
	}

	// The human's refresh verb retries regardless -- rewrite origin/child to the worktree's own
	// commit first, standing in for the human resolving the divergence by hand.
	runGit(t, "-C", f.childWorktree, "push", "-q", "--force", "origin", "child")
	if err := store.QueueVerbIntent(t.Context(), f.child.TicketURL, plan.VerbRefresh, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}

	newHead := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))
	if newHead == childTip0 {
		t.Error("the refresh verb must retry the fast-forward and merge, but the worktree is unchanged")
	}
	pending, err := store.PendingVerbIntents(t.Context(), plan.VerbRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending refresh intents = %+v, want none: consumed", pending)
	}
}
