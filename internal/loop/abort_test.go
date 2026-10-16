package loop_test

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
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
	clock := fixedClock(at.Add(time.Minute))
	return conflictFixture{
		f: f, aliveRuns: aliveRuns, store: store,
		loop:   newLoop(store, observe, clock, cfg, ws, runner.ProcessRunner{}),
		server: openServer(store, clock, ""),
	}
}

func (c conflictFixture) tick(t *testing.T) {
	t.Helper()
	runOnce(t, c.loop)
}

func (c conflictFixture) midMerge(t *testing.T) bool {
	t.Helper()
	mid, err := git.MidMerge(t.Context(), c.f.childWorktree)
	if err != nil {
		t.Fatalf("MidMerge(%q): %v", c.f.childWorktree, err)
	}
	return mid
}

func TestAbort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		verbs         []plan.Verb
		wantMidMerge  bool
		wantConflicts int
		wantState     string
	}{
		{name: "abort alone", verbs: []plan.Verb{plan.VerbAbort}, wantState: "base_moved", wantConflicts: 1},
		{
			name: "abort then refresh", verbs: []plan.Verb{plan.VerbAbort, plan.VerbRefresh},
			wantMidMerge: true, wantConflicts: 2, wantState: "refresh_conflicted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, repoPath := repoWithOrigin(t)
			c := newConflictFixture(t, root, repoPath, testAt)
			c.tick(t)
			c.tick(t)
			for _, verb := range tt.verbs {
				if err := c.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{Ticket: c.f.child.URL, Verb: verb, At: testAt.Add(time.Hour)}); err != nil {
					t.Fatalf("QueueVerbIntent(%q): %v", verb, err)
				}
			}
			c.tick(t)
			c.tick(t)

			if got := c.midMerge(t); got != tt.wantMidMerge {
				t.Errorf("mid-merge = %v, want %v", got, tt.wantMidMerge)
			}
			if row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL); row.State != tt.wantState {
				t.Errorf("child state = %q, want %q", row.State, tt.wantState)
			}
			pending, err := c.store.PendingVerbIntents(t.Context(), plan.VerbAbort)
			if err != nil {
				t.Fatalf("PendingVerbIntents(abort): %v", err)
			}
			if len(pending) != 0 {
				t.Errorf("PendingVerbIntents(abort) = %+v, want none", pending)
			}
			events, err := c.store.Events(t.Context())
			if err != nil {
				t.Fatalf("Events(): %v", err)
			}
			conflicts := 0
			for _, event := range events {
				if event.Kind == "refresh_conflicted" {
					conflicts++
				}
			}
			if !hasEvent(events, "merge_aborted", "") || conflicts != tt.wantConflicts {
				t.Errorf("merge_aborted = %v, refresh_conflicted events = %d, want true and %d",
					hasEvent(events, "merge_aborted", ""), conflicts, tt.wantConflicts)
			}
		})
	}

	t.Run("never touches a worktree with a live run", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		c := newConflictFixture(t, root, repoPath, testAt)
		c.tick(t)
		c.tick(t)
		c.aliveRuns[c.f.child.URL] = true
		if err := c.store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{Ticket: c.f.child.URL, Verb: plan.VerbAbort, At: testAt.Add(time.Hour)}); err != nil {
			t.Fatalf("QueueVerbIntent(abort): %v", err)
		}
		c.tick(t)

		if !c.midMerge(t) {
			t.Error("mid-merge = false, want the live run's worktree untouched")
		}
		if !hasEventIn(t, c.store, "merge_abort_failed", "a run is alive") {
			t.Error("Events() has no merge_abort_failed naming the live run")
		}
	})
}
