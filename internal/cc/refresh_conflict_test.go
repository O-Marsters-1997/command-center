package cc_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// conflictingAdvance has both branches add the same path with different contents, so refresh's
// step-3 merge can only conflict, and re-records the child's push against the pre-advance base.
func conflictingAdvance(t *testing.T, repoPath string, store *cc.Store, f stackedFixture, at time.Time) string {
	t.Helper()

	commitFile(t, f.childWorktree, "shared.txt", "the child's line\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "child")
	childTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))
	if err := store.RecordPush(context.Background(), f.child.TicketURL, childTip, "parent", f.parentTip0, at); err != nil {
		t.Fatal(err)
	}

	commitFile(t, f.parentWorktree, "shared.txt", "the parent's line\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "parent")
	return strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/parent"))
}

// conflictFixture wires a stacked pair whose refresh merge conflicts, over an observe phase that
// reads MERGE_HEAD fresh on every tick.
type conflictFixture struct {
	f      stackedFixture
	loop   *cc.Loop
	server *cc.Server
	store  *cc.Store
	// aliveRuns is the liveness every tick's observation reports.
	aliveRuns map[string]bool
}

func newConflictFixture(t *testing.T, root, repoPath string, at time.Time) conflictFixture {
	t.Helper()

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := conflictingAdvance(t, repoPath, store, f, at)
	childTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))

	aliveRuns := map[string]bool{}
	observe := func(ctx context.Context) (cc.Observation, error) {
		obs := baseObservation(f, parentTip1)
		for ticketURL, alive := range aliveRuns {
			obs.Runs[ticketURL] = cc.RunObservation{Alive: alive}
		}
		obs.PRs["child"] = gh.PR{
			Number: 2, HeadRef: "child", State: gh.Open, HeadOid: childTip,
			Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
		}
		mid, err := cc.MidMerge(ctx, f.childWorktree)
		if err != nil {
			return cc.Observation{}, err
		}
		obs.MidMerge["child"] = mid
		return obs, nil
	}

	// Without a predicate every verdict flag stays false and the row reads `checking`.
	cfg, ws := stackedConfigAndWorkspace(t, root)
	cfg.Repos[0].Checks = verdict.Predicate{Success: "CI"}
	clock := fixedClock(at.Add(time.Minute))
	return conflictFixture{
		f:         f,
		aliveRuns: aliveRuns,
		loop:      cc.NewLoop(store, observe, clock, cfg, ws, cc.ProcessRunner{}),
		server:    cc.NewServer(store, clock, cfg.Repos),
		store:     store,
	}
}

func (c conflictFixture) tick(t *testing.T) {
	t.Helper()
	if err := c.loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

func (c conflictFixture) midMerge(t *testing.T) bool {
	t.Helper()
	mid, err := cc.MidMerge(t.Context(), c.f.childWorktree)
	if err != nil {
		t.Fatal(err)
	}
	return mid
}

func TestAConflictingRefreshLeavesTheWorktreeMidMergeAndTheRowReadsRefreshConflicted(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t) // the automatic pass merges the advanced parent and conflicts
	if !c.midMerge(t) {
		t.Fatal("the child worktree has no MERGE_HEAD: the conflicting merge did not leave it mid-merge")
	}

	c.tick(t) // the next tick's observation is the one that reads MERGE_HEAD

	page := renderPage(t, c.server)
	if state := rowState(t, page, c.f.child.TicketURL); state != "refresh_conflicted" {
		t.Fatalf("child's state = %q, want refresh_conflicted", state)
	}
	if got := rowCellAt(t, page, c.f.child.TicketURL, 10); got != c.f.childWorktree {
		t.Errorf("child's rendered worktree = %q, want the path to shell into: %q", got, c.f.childWorktree)
	}
	if !strings.Contains(page, `value="`+plan.VerbAbort+`"`) {
		t.Errorf("page offers no abort button:\n%s", page)
	}
}

func TestAbortClearsTheMidMergeAndTheRowLeavesRefreshConflictedForGood(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t)
	c.tick(t)

	if err := c.store.QueueVerbIntent(t.Context(), c.f.child.TicketURL, plan.VerbAbort, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c.tick(t)

	if c.midMerge(t) {
		t.Fatal("the child worktree is still mid-merge: abort did not run git merge --abort")
	}
	pending, err := c.store.PendingVerbIntents(t.Context(), plan.VerbAbort)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending abort intents = %+v, want none: consumed", pending)
	}

	// The tick after the abort is the one whose observation clears the state.
	c.tick(t)
	if c.midMerge(t) {
		t.Fatal("the automatic pass re-conflicted the aborted merge: abort would be futile")
	}
	if state := rowState(t, renderPage(t, c.server), c.f.child.TicketURL); state != "base_moved" {
		t.Errorf("child's state after abort = %q, want base_moved: the base still moved, refresh is the human's now", state)
	}
}

func TestAbortAndRefreshInOneTickRunInThatOrderRatherThanDroppingTheRefresh(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t)
	c.tick(t)

	for _, verb := range []string{plan.VerbAbort, plan.VerbRefresh} {
		if err := c.store.QueueVerbIntent(t.Context(), c.f.child.TicketURL, verb, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	c.tick(t)

	events, err := c.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "merge_aborted", "") {
		t.Fatalf("events = %+v, want a merge_aborted", events)
	}
	conflicts := 0
	for _, e := range events {
		if e.Kind == "refresh_conflicted" {
			conflicts++
		}
	}
	if conflicts != 2 {
		t.Errorf("refresh_conflicted events = %d, want 2: the refresh queued alongside the abort must act "+
			"on the worktree the abort just freed, not be consumed and dropped", conflicts)
	}
}

func TestAbortNeverTouchesAWorktreeWithALiveRun(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t)
	c.tick(t)

	c.aliveRuns[c.f.child.TicketURL] = true
	if err := c.store.QueueVerbIntent(t.Context(), c.f.child.TicketURL, plan.VerbAbort, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c.tick(t)

	if !c.midMerge(t) {
		t.Error("abort ran git merge --abort in a worktree with a live run (inv. 4)")
	}
	events, err := c.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "merge_abort_failed", "a run is alive") {
		t.Errorf("events = %+v, want a merge_abort_failed naming the live run", events)
	}
}

func TestAHumanResolvingTheConflictByHandClearsTheStateWithNoVerb(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv.
	root, repoPath := repoWithOrigin(t)
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	c := newConflictFixture(t, root, repoPath, at)
	parentTip1 := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/parent"))

	c.tick(t)
	c.tick(t)

	commitFile(t, c.f.childWorktree, "shared.txt", "both lines, reconciled\n")
	if c.midMerge(t) {
		t.Fatal("committing the resolution left MERGE_HEAD behind")
	}

	c.tick(t)

	if state := rowState(t, renderPage(t, c.server), c.f.child.TicketURL); state == "refresh_conflicted" {
		t.Error("child still reads refresh_conflicted after the conflict was resolved and committed by hand")
	}
	pushes, err := c.store.LatestPushes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := pushes[c.f.child.TicketURL].BaseSHAAtPush; got != parentTip1 {
		t.Errorf("child's recorded base sha = %s, want the advanced parent tip %s: the push step delivers the "+
			"hand-resolved merge", got, parentTip1)
	}
}
