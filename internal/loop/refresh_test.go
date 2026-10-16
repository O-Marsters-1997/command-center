package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func forcePushDivergentCommit(t *testing.T, root, branch string) {
	t.Helper()
	remote := filepath.Join(root, "remote.git")
	clone := filepath.Join(t.TempDir(), "divergent-clone")
	gittest.Run(t, ".", "clone", "-q", remote, clone)
	gittest.Run(t, clone, "config", "user.name", "t")
	gittest.Run(t, clone, "config", "user.email", "t@example.com")
	gittest.Run(t, clone, "checkout", "-q", "-b", branch, "origin/main")
	if err := os.WriteFile(filepath.Join(clone, "divergent.txt"), []byte("someone else's history\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(divergent.txt): %v", err)
	}
	gittest.Run(t, clone, "add", "divergent.txt")
	gittest.Run(t, clone, "commit", "-q", "-m", "diverging commit")
	gittest.Run(t, clone, "push", "-q", "--force", "origin", "HEAD:"+branch)
}

type stackedFixture struct {
	parent, child                 storepkg.Ticket
	parentWorktree, childWorktree string
	parentTip0, mainSHA           string
}

func newStackedFixture(t *testing.T, repoPath string, store *storepkg.Store, at time.Time) stackedFixture {
	t.Helper()
	ctx := t.Context()

	parentWorktree := cutWorktree(t, repoPath, "parent")
	gittest.Commit(t, parentWorktree, "parent.txt", "parent's original work\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "parent")
	parentTip0 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/parent")

	childWorktree := filepath.Join(t.TempDir(), "wt-child")
	gittest.Run(t, repoPath, "worktree", "add", "-b", "child", childWorktree, "origin/parent")
	gittest.Commit(t, childWorktree, "child.txt", "child's own work\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "child")
	childTip0 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")

	parent := storepkg.Ticket{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"}
	child := storepkg.Ticket{
		URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"},
	}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{parent, child}); err != nil {
		t.Fatalf("UpsertTickets(parent, child): %v", err)
	}
	dispositionAsPushed(t, store, parent.URL, at)
	dispositionAsPushed(t, store, child.URL, at)
	mainSHA := gittest.Run(t, repoPath, "rev-parse", "origin/main")
	parentPush := storepkg.Push{
		Ticket: plan.TicketURL(parent.URL), Tip: plan.SHA(parentTip0), BaseSHA: plan.SHA(mainSHA),
		BaseBranch: "main", At: at,
	}
	if err := store.RecordPush(ctx, parentPush); err != nil {
		t.Fatalf("RecordPush(parent): %v", err)
	}
	childPush := storepkg.Push{
		Ticket: plan.TicketURL(child.URL), Tip: plan.SHA(childTip0), BaseSHA: plan.SHA(parentTip0),
		BaseBranch: "parent", At: at,
	}
	if err := store.RecordPush(ctx, childPush); err != nil {
		t.Fatalf("RecordPush(child): %v", err)
	}

	return stackedFixture{
		parent: parent, child: child,
		parentWorktree: parentWorktree, childWorktree: childWorktree, parentTip0: parentTip0, mainSHA: mainSHA,
	}
}

func advanceParent(t *testing.T, repoPath string, f stackedFixture) string {
	t.Helper()
	gittest.Commit(t, f.parentWorktree, "parent-fix.txt", "the fix\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "parent")
	return gittest.Run(t, repoPath, "rev-parse", "refs/heads/parent")
}

func baseObservation(f stackedFixture, parentTip string) plan.Observation {
	return plan.Observation{
		Worktrees: map[plan.BranchRef]string{
			branchRef("repo", "parent"): f.parentWorktree, branchRef("repo", "child"): f.childWorktree,
		},
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "parent"): {Number: 1, HeadRef: "parent", State: plan.Open},
			branchRef("repo", "child"):  {Number: 2, HeadRef: "child", State: plan.Open},
		},
		BranchTips: map[plan.BranchRef]plan.SHA{
			branchRef("repo", "parent"): plan.SHA(parentTip), branchRef("repo", plan.DefaultBaseBranch): plan.SHA(f.mainSHA),
		},
		Runs:     map[plan.TicketURL]plan.RunObservation{},
		MidMerge: map[plan.BranchRef]bool{},
		Settings: map[string]config.RepoSettings{"repo": {Stacking: true}},
	}
}

func advanceParentPastTheConflict(t *testing.T, repoPath string, f stackedFixture) string {
	t.Helper()
	gittest.Run(t, f.parentWorktree, "reset", "-q", "--hard", f.parentTip0)
	gittest.Commit(t, f.parentWorktree, "parent-fix2.txt", "a later fix that carries none of the clash\n")
	gittest.Run(t, repoPath, "push", "-q", "--force", "origin", "parent")
	return gittest.Run(t, f.parentWorktree, "rev-parse", "refs/heads/parent")
}

func dispositionAs(t *testing.T, store *storepkg.Store, ticketURL string, outcome plan.Outcome, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticketURL, err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: outcome, ExitCode: option.Some(0),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()

	t.Run("merges the advanced parent and the push step delivers it the same tick", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)

		obs := baseObservation(f, parentTip1)
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		childHead := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
		parents := strings.Fields(gittest.Run(t, f.childWorktree, "log", "-1", "--format=%P", childHead))
		if len(parents) != 2 {
			t.Fatalf("child HEAD has %d parents, want 2 (a merge commit): %v", len(parents), parents)
		}
		if _, err := os.Stat(filepath.Join(f.childWorktree, "parent-fix.txt")); err != nil {
			t.Errorf("child worktree does not contain the parent's fix after refresh: %v", err)
		}

		remoteChildTip := gittest.Run(t, filepath.Join(root, "remote.git"), "rev-parse", "refs/heads/child")
		if remoteChildTip != childHead {
			t.Errorf("remote child tip = %s, want %s: the merge commit must be pushed the same tick", remoteChildTip, childHead)
		}

		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(events, "refreshed", "") {
			t.Errorf("events = %+v, want a refreshed event", events)
		}
		if !hasEvent(events, "pushed", childHead) {
			t.Errorf("events = %+v, want a pushed event naming the merge commit %s", events, childHead)
		}

		pushes, err := store.LatestPushes(t.Context())
		if err != nil {
			t.Fatalf("LatestPushes(): %v", err)
		}
		if got := pushes[plan.TicketURL(f.child.URL)].BaseSHAAtPush; string(got) != parentTip1 {
			t.Errorf("child's recorded base sha = %s, want the advanced parent tip %s", got, parentTip1)
		}
	})

	t.Run("a git failure records a tick error naming the step and branch", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)
		childTip := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")
		unresolvable := strings.Repeat("0", 39) + "1"
		pushedAt := at.Add(time.Second)
		push := storepkg.Push{
			Ticket: plan.TicketURL(f.child.URL), Tip: plan.SHA(childTip), BaseSHA: plan.SHA(unresolvable),
			BaseBranch: "parent", At: pushedAt,
		}
		if err := store.RecordPush(t.Context(), push); err != nil {
			t.Fatalf("RecordPush(%q): %v", f.child.URL, err)
		}

		obs := baseObservation(f, parentTip1)
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }
		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})

		if err := lp.RunOnce(t.Context()); err == nil {
			t.Fatal("RunOnce = nil, want the refresh failure")
		}

		tickErr, err := store.LastError(t.Context())
		if err != nil || tickErr.IsNone() {
			t.Fatalf("LastError: last=%v err=%v", tickErr, err)
		}
		if want := "refresh: repo/child: "; !strings.HasPrefix(tickErr.Unwrap().Message, want) {
			t.Errorf("tick error = %q, want prefix %q", tickErr.Unwrap().Message, want)
		}
	})

	t.Run("merges an advanced main into a root row", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		advanceMain(t, root, "main-fix.txt", "someone else's landed work\n")
		gittest.Run(t, repoPath, "fetch", "-q", "origin", "main")
		mainTip1 := gittest.Run(t, repoPath, "rev-parse", "refs/remotes/origin/main")

		obs := baseObservation(f, f.parentTip0)
		obs.BranchTips[branchRef("repo", plan.DefaultBaseBranch)] = plan.SHA(mainTip1)
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		if _, err := os.Stat(filepath.Join(f.parentWorktree, "main-fix.txt")); err != nil {
			t.Errorf("parent worktree does not contain main's own advance after refresh: %v", err)
		}

		pushes, err := store.LatestPushes(t.Context())
		if err != nil {
			t.Fatalf("LatestPushes(): %v", err)
		}
		if got := pushes[plan.TicketURL(f.parent.URL)].BaseSHAAtPush; string(got) != mainTip1 {
			t.Errorf("parent's recorded base sha = %s, want the advanced main tip %s", got, mainTip1)
		}
	})

	t.Run("never touches a worktree with a live run", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)
		childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")

		obs := baseObservation(f, parentTip1)
		obs.Runs[plan.TicketURL(f.child.URL)] = plan.RunObservation{Alive: true}
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		if got := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD"); got != childTip0 {
			t.Errorf("child worktree HEAD = %s, want unchanged %s: a live run must never see a git write (inv. 4)",
				got, childTip0)
		}
		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if hasEvent(events, "refreshed", "") || hasEvent(events, "refresh_refused", "") {
			t.Errorf("events = %+v, want no refresh activity while a run is alive", events)
		}
	})

	t.Run("the verb records why it did nothing while a run is alive", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)

		obs := baseObservation(f, parentTip1)
		obs.Runs[plan.TicketURL(f.child.URL)] = plan.RunObservation{Alive: true}
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbRefresh, At: at.Add(time.Second),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, refresh): %v", f.child.URL, err)
		}

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(events, "refresh_refused", "a run is alive") {
			t.Errorf("events = %+v, want a refresh_refused naming the live run as why the manual refresh did nothing", events)
		}
	})

	t.Run("a refused fast-forward is not retried automatically, but the verb retries it", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)

		forcePushDivergentCommit(t, root, "child")
		gittest.Run(t, repoPath, "fetch", "-q", "origin")
		childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")

		obs := baseObservation(f, parentTip1)
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		if got := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD"); got != childTip0 {
			t.Errorf("child worktree HEAD = %s, want unchanged %s: a refused fast-forward must not touch it", got, childTip0)
		}

		facts, err := store.RefreshFacts(t.Context())
		if err != nil {
			t.Fatalf("RefreshFacts(): %v", err)
		}
		if !facts[plan.TicketURL(f.child.URL)].Refused {
			t.Fatalf("refresh facts = %+v, want %s refused", facts, f.child.URL)
		}

		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(events, "refresh_refused", "") {
			t.Errorf("events = %+v, want a refresh_refused event", events)
		}

		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("second RunOnce: %v", err)
		}
		eventsAfter, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
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

		gittest.Run(t, f.childWorktree, "push", "-q", "--force", "origin", "child")
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbRefresh, At: at.Add(time.Hour),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, refresh): %v", f.child.URL, err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("retry RunOnce: %v", err)
		}

		newHead := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
		if newHead == childTip0 {
			t.Error("the refresh verb must retry the fast-forward and merge, but the worktree is unchanged")
		}
		pending, err := store.PendingVerbIntents(t.Context(), plan.VerbRefresh)
		if err != nil {
			t.Fatalf("PendingVerbIntents(refresh): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending refresh intents = %+v, want none: consumed", pending)
		}
	})

	t.Run("auto refresh retries once the branch is resolved and pushed outside the app", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := conflictingAdvance(t, repoPath, store, f, at)

		obs := baseObservation(f, parentTip1)
		obs.BranchTips[branchRef("repo", "child")] = plan.SHA(gittest.Run(t, repoPath, "rev-parse", "refs/heads/child"))
		observe := func(ctx context.Context) (plan.Observation, error) {
			mid, err := git.MidMerge(ctx, f.childWorktree)
			if err != nil {
				return plan.Observation{}, err
			}
			obs.MidMerge[branchRef("repo", "child")] = mid
			return obs, nil
		}

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(events, "refresh_conflicted", "") {
			t.Fatalf("events = %+v, want a refresh_conflicted event", events)
		}

		clone := filepath.Join(t.TempDir(), "outside-clone")
		gittest.Run(t, ".", "clone", "-q", filepath.Join(root, "remote.git"), clone)
		gittest.Run(t, clone, "config", "user.name", "t")
		gittest.Run(t, clone, "config", "user.email", "t@example.com")
		gittest.Run(t, clone, "checkout", "-q", "child")
		gittest.Run(t, clone, "merge", "-q", "-X", "ours", "-m", "resolved by hand", "origin/parent")
		gittest.Run(t, clone, "push", "-q", "origin", "child")
		gittest.Run(t, repoPath, "fetch", "-q", "origin", "child")
		cloneHead := gittest.Run(t, clone, "rev-parse", "HEAD")
		obs.BranchTips[branchRef("repo", "child")] = plan.SHA(cloneHead)

		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbAbort, At: at.Add(time.Hour),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, abort): %v", f.child.URL, err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("abort RunOnce: %v", err)
		}

		eventsAfter, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(eventsAfter, "refreshed", "") {
			t.Errorf("events = %+v, want a refreshed event once the branch was resolved and pushed "+
				"outside the app, with no refresh verb", eventsAfter)
		}
	})

	t.Run("auto refresh retries once the base moves past the failed merge", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := conflictingAdvance(t, repoPath, store, f, at)
		childTip0 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")

		obs := baseObservation(f, parentTip1)
		obs.BranchTips[branchRef("repo", "child")] = plan.SHA(childTip0)
		observe := func(ctx context.Context) (plan.Observation, error) {
			mid, err := git.MidMerge(ctx, f.childWorktree)
			if err != nil {
				return plan.Observation{}, err
			}
			obs.MidMerge[branchRef("repo", "child")] = mid
			return obs, nil
		}

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbAbort, At: at.Add(time.Hour),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, abort): %v", f.child.URL, err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("abort RunOnce: %v", err)
		}

		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if hasEvent(events, "refreshed", "") {
			t.Fatalf("events = %+v, want no refreshed event yet: the base has not moved past the failed merge", events)
		}

		parentTip2 := advanceParentPastTheConflict(t, repoPath, f)
		obs.BranchTips[branchRef("repo", "parent")] = plan.SHA(parentTip2)

		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("retry RunOnce: %v", err)
		}

		eventsAfter, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(eventsAfter, "refreshed", "") {
			t.Errorf("events = %+v, want a refreshed event once the base moved past the failed merge, "+
				"with no refresh verb", eventsAfter)
		}
		if _, err := os.Stat(filepath.Join(f.childWorktree, "parent-fix2.txt")); err != nil {
			t.Errorf("child worktree does not contain the base's later fix after auto-refresh: %v", err)
		}
	})

	t.Run("auto refresh does not retry an unchanged conflict", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := conflictingAdvance(t, repoPath, store, f, at)
		childTip0 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/child")

		obs := baseObservation(f, parentTip1)
		obs.BranchTips[branchRef("repo", "child")] = plan.SHA(childTip0)
		observe := func(ctx context.Context) (plan.Observation, error) {
			mid, err := git.MidMerge(ctx, f.childWorktree)
			if err != nil {
				return plan.Observation{}, err
			}
			obs.MidMerge[branchRef("repo", "child")] = mid
			return obs, nil
		}

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbAbort, At: at.Add(time.Hour),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, abort): %v", f.child.URL, err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("abort RunOnce: %v", err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		conflicts := 0
		for _, e := range events {
			if e.Kind == "refresh_conflicted" {
				conflicts++
			}
		}
		if conflicts != 1 {
			t.Errorf("refresh_conflicted events = %d, want still 1: nothing has moved, so autoRefresh "+
				"must not retry the same merge", conflicts)
		}
	})

	t.Run("the verb records why it declined", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		at := testAt

		f := newStackedFixture(t, repoPath, store, at)
		parentTip1 := advanceParent(t, repoPath, f)

		obs := baseObservation(f, parentTip1)
		delete(obs.PRs, branchRef("repo", "parent"))
		observe := func(context.Context) (plan.Observation, error) { return obs, nil }

		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, runner.ProcessRunner{})

		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("automatic RunOnce: %v", err)
		}
		events, err := store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if hasEvent(events, "refresh_refused", "") {
			t.Errorf("events = %+v, want no refresh_refused from an automatic tick", events)
		}

		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: f.child.URL, Verb: plan.VerbRefresh, At: at.Add(time.Hour),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%q, refresh): %v", f.child.URL, err)
		}
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("verb RunOnce: %v", err)
		}

		events, err = store.Events(t.Context())
		if err != nil {
			t.Fatalf("Events(): %v", err)
		}
		if !hasEvent(events, "refresh_refused", f.parent.URL) {
			t.Errorf("events = %+v, want a refresh_refused naming the blocker %s", events, f.parent.URL)
		}

		pending, err := store.PendingVerbIntents(t.Context(), plan.VerbRefresh)
		if err != nil {
			t.Fatalf("PendingVerbIntents(refresh): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending refresh intents = %+v, want none: consumed", pending)
		}
	})

	t.Run("leaves an awaiting-input worktree untouched until it leaves that state", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		store := openStore(t)
		f := newStackedFixture(t, repoPath, store, testAt)
		dispositionAs(t, store, f.child.URL, plan.OutcomeAsked, testAt.Add(time.Second))
		parentTip1 := advanceParent(t, repoPath, f)
		childTip0 := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD")
		cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
		lp := newLoop(store, observing(baseObservation(f, parentTip1)), fixedClock(testAt.Add(time.Minute)), cfg, ws,
			runner.ProcessRunner{})

		runOnce(t, lp)
		if got := gittest.Run(t, f.childWorktree, "rev-parse", "HEAD"); got != childTip0 {
			t.Errorf("child HEAD while awaiting input = %s, want %s", got, childTip0)
		}
		if hasEventIn(t, store, "refreshed", "") || hasEventIn(t, store, "refresh_refused", "") {
			t.Error("Events() has refresh activity while awaiting input, want none")
		}
		dispositionAs(t, store, f.child.URL, plan.OutcomePush, testAt.Add(2*time.Second))
		runOnce(t, lp)
		if !hasEventIn(t, store, "refreshed", "") {
			t.Error("Events() has no refreshed once the ticket left awaiting input")
		}
	})

	t.Run("a conflicting refresh leaves the worktree mid-merge and the row refresh_conflicted", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		c := newConflictFixture(t, root, repoPath, testAt)

		c.tick(t)
		if !c.midMerge(t) {
			t.Fatal("child worktree has no MERGE_HEAD, want it left mid-merge")
		}
		c.tick(t)

		if row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL); row.State != "refresh_conflicted" {
			t.Errorf("child state = %q, want refresh_conflicted", row.State)
		}
	})

	t.Run("a hand resolution clears the conflict with no verb and is pushed", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		c := newConflictFixture(t, root, repoPath, testAt)
		parentTip1 := gittest.Run(t, repoPath, "rev-parse", "refs/heads/parent")
		c.tick(t)
		c.tick(t)

		gittest.Commit(t, c.f.childWorktree, "shared.txt", "both lines, reconciled\n")
		if c.midMerge(t) {
			t.Fatal("committing the resolution left MERGE_HEAD behind")
		}
		c.tick(t)

		if row := webtest.RowFor(t, renderPage(t, c.server), c.f.child.URL); row.State == "refresh_conflicted" {
			t.Errorf("child state after hand resolution = %q, want it cleared", row.State)
		}
		pushes, err := c.store.LatestPushes(t.Context())
		if err != nil {
			t.Fatalf("LatestPushes(): %v", err)
		}
		if got := pushes[plan.TicketURL(c.f.child.URL)].BaseSHAAtPush; string(got) != parentTip1 {
			t.Errorf("child's recorded base sha = %s, want the advanced parent tip %s", got, parentTip1)
		}
	})

	t.Run("a git failure that is not a conflict is a tick error", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		c := newConflictFixture(t, root, repoPath, testAt)
		gittest.Run(t, repoPath, "update-ref", "-d", "refs/remotes/origin/parent")

		if err := c.loop.RunOnce(t.Context()); err == nil {
			t.Error("RunOnce() error = nil, want the failed advance reported")
		}
		if hasEventIn(t, c.store, "refresh_conflicted", "") {
			t.Error("Events() has refresh_conflicted, want none: the worktree is not mid-merge")
		}
	})
}
