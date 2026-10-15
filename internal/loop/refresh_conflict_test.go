package loop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func conflictingAdvance(t *testing.T, repoPath string, store *storepkg.Store, f stackedFixture, at time.Time) string {
	t.Helper()

	gittest.Commit(t, f.childWorktree, "shared.txt", "the child's line\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "child")
	childTip := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")
	push := storepkg.Push{
		Ticket: plan.TicketURL(f.child.URL), Tip: plan.SHA(childTip), BaseSHA: plan.SHA(f.parentTip0),
		BaseBranch: "parent", At: at,
	}
	if err := store.RecordPush(t.Context(), push); err != nil {
		t.Fatalf("RecordPush(%q): %v", f.child.URL, err)
	}

	gittest.Commit(t, f.parentWorktree, "shared.txt", "the parent's line\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "parent")
	return gittest.Run(t, repoPath, "rev-parse", "refs/heads/parent")
}

type conflictFixture struct {
	f         stackedFixture
	loop      *loop.Loop
	server    *web.Server
	store     *storepkg.Store
	aliveRuns map[string]bool
}

func newConflictFixture(t *testing.T, root, repoPath string, at time.Time) conflictFixture {
	t.Helper()

	store := openStore(t)
	f := newStackedFixture(t, repoPath, store, at)
	parentTip1 := conflictingAdvance(t, repoPath, store, f, at)
	childTip := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")

	aliveRuns := map[string]bool{}
	observe := func(ctx context.Context) (plan.Observation, error) {
		obs := baseObservation(f, parentTip1)
		for ticketURL, alive := range aliveRuns {
			obs.Runs[plan.TicketURL(ticketURL)] = plan.RunObservation{Alive: alive}
		}
		obs.PRs[branchRef("repo", "child")] = plan.PR{
			Number: 2, HeadRef: "child", State: plan.Open, HeadOid: plan.SHA(childTip),
			Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
		}
		obs.Settings["repo"] = config.RepoSettings{Stacking: true, Checks: verdict.Predicate{Success: "CI"}}
		mid, err := git.MidMerge(ctx, f.childWorktree)
		if err != nil {
			return plan.Observation{}, err
		}
		obs.MidMerge[branchRef("repo", "child")] = mid
		return obs, nil
	}

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	clock := cctest.Clock{At: at.Add(time.Minute)}
	return conflictFixture{
		f:         f,
		aliveRuns: aliveRuns,
		loop:      newLoop(store, observe, clock, cfg, ws, runner.ProcessRunner{}),
		server:    openServer(store, clock, ""),
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
	mid, err := git.MidMerge(t.Context(), c.f.childWorktree)
	if err != nil {
		t.Fatalf("MidMerge(%q): %v", c.f.childWorktree, err)
	}
	return mid
}

func TestAConflictingRefreshLeavesTheWorktreeMidMergeAndTheRowReadsRefreshConflicted(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	at := testAt
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t)
	if !c.midMerge(t) {
		t.Fatal("the child worktree has no MERGE_HEAD: the conflicting merge did not leave it mid-merge")
	}

	c.tick(t)

	page := renderPage(t, c.server)
	row := webtest.RowFor(t, page, c.f.child.URL)
	if state := row.State; state != "refresh_conflicted" {
		t.Fatalf("child's state = %q, want refresh_conflicted:\n%s", state, row.Row.HTML())
	}
	if !strings.Contains(page, `value="`+string(plan.VerbAbort)+`"`) {
		t.Errorf("page offers no abort button:\n%s", page)
	}
}

func TestAbortConflictWithOptionalRefresh(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		verbs         []plan.Verb
		wantMidMerge  bool
		wantConflicts int
		wantState     string
	}{
		{name: "abort alone", verbs: []plan.Verb{plan.VerbAbort}, wantState: "base_moved", wantConflicts: 1},
		{name: "abort then refresh", verbs: []plan.Verb{plan.VerbAbort, plan.VerbRefresh},
			wantMidMerge: true, wantConflicts: 2, wantState: "refresh_conflicted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			at := testAt
			c := newConflictFixture(t, root, repoPath, at)
			c.tick(t)
			c.tick(t)
			for _, verb := range tt.verbs {
				if err := c.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
					Ticket: c.f.child.URL, Verb: verb, At: at.Add(time.Hour),
				}); err != nil {
					t.Fatalf("QueueVerbIntent(%q, %q): %v", c.f.child.URL, verb, err)
				}
			}
			c.tick(t)
			c.tick(t)
			if got := c.midMerge(t); got != tt.wantMidMerge {
				t.Errorf("mid-merge after abort = %v, want %v", got, tt.wantMidMerge)
			}
			row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL)
			if row.State != tt.wantState {
				t.Errorf("child's state after abort = %q, want %q:\n%s", row.State, tt.wantState, row.Row.HTML())
			}
			pending, err := c.store.PendingVerbIntents(t.Context(), plan.VerbAbort)
			if err != nil {
				t.Fatalf("PendingVerbIntents(abort): %v", err)
			}
			if len(pending) != 0 {
				t.Errorf("pending abort intents = %+v, want none", pending)
			}
			events, err := c.store.Events(t.Context())
			if err != nil {
				t.Fatalf("Events(): %v", err)
			}
			if !hasEvent(events, "merge_aborted", "") {
				t.Errorf("events = %+v, want merge_aborted", events)
			}
			conflicts := 0
			for _, event := range events {
				if event.Kind == "refresh_conflicted" {
					conflicts++
				}
			}
			if conflicts != tt.wantConflicts {
				t.Errorf("refresh_conflicted events = %d, want %d", conflicts, tt.wantConflicts)
			}
		})
	}
}

func TestAbortNeverTouchesAWorktreeWithALiveRun(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	at := testAt
	c := newConflictFixture(t, root, repoPath, at)

	c.tick(t)
	c.tick(t)

	c.aliveRuns[c.f.child.URL] = true
	if err := c.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
		Ticket: c.f.child.URL, Verb: plan.VerbAbort, At: at.Add(time.Hour),
	}); err != nil {
		t.Fatalf("QueueVerbIntent(%q, abort): %v", c.f.child.URL, err)
	}
	c.tick(t)

	if !c.midMerge(t) {
		t.Error("abort ran git merge --abort in a worktree with a live run (inv. 4)")
	}
	events, err := c.store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
	}
	if !hasEvent(events, "merge_abort_failed", "a run is alive") {
		t.Errorf("events = %+v, want a merge_abort_failed naming the live run", events)
	}
}

func TestAHumanResolvingTheConflictByHandClearsTheStateWithNoVerb(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	at := testAt
	c := newConflictFixture(t, root, repoPath, at)
	parentTip1 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/parent")

	c.tick(t)
	c.tick(t)

	gittest.Commit(t, c.f.childWorktree, "shared.txt", "both lines, reconciled\n")
	if c.midMerge(t) {
		t.Fatal("committing the resolution left MERGE_HEAD behind")
	}

	c.tick(t)

	row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL)
	if row.State == "refresh_conflicted" {
		t.Errorf("child still refresh_conflicted after hand resolution:\n%s", row.Row.HTML())
	}
	pushes, err := c.store.LatestPushes(t.Context())
	if err != nil {
		t.Fatalf("LatestPushes(): %v", err)
	}
	if got := pushes[plan.TicketURL(c.f.child.URL)].BaseSHAAtPush; string(got) != parentTip1 {
		t.Errorf("child's recorded base sha = %s, want the advanced parent tip %s: the push step delivers the "+
			"hand-resolved merge", got, parentTip1)
	}
}

func TestAGitFailureDuringRefreshThatIsNotAConflictIsATickError(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	at := testAt
	c := newConflictFixture(t, root, repoPath, at)
	gittest.Run(t, repoPath, "update-ref", "-d", "refs/remotes/origin/parent")

	err := c.loop.RunOnce(t.Context())
	if err == nil {
		t.Errorf("RunOnce = nil, want the failed advance of %s reported as a tick error", c.f.child.URL)
	}

	events, eventsErr := c.store.Events(t.Context())
	if eventsErr != nil {
		t.Fatalf("Events(): %v", eventsErr)
	}
	if hasEvent(events, "refresh_conflicted", "") {
		t.Errorf("events = %+v, want no refresh_conflicted: the worktree is not mid-merge", events)
	}
}
