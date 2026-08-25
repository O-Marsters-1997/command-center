package cc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// mergeParentIntoMain is what merging the parent's pull request leaves behind: main carries the
// parent's commits and the parent's branch is gone from the remote (both repos delete on merge).
func mergeParentIntoMain(t *testing.T, repoPath string) {
	t.Helper()
	runGit(t, "-C", repoPath, "push", "-q", "origin", "parent:main")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "--delete", "parent")
	runGit(t, "-C", repoPath, "fetch", "-q", "--prune", "origin")
}

func mergedObservation(f stackedFixture, childBaseRef string) cc.Observation {
	obs := baseObservation(f, "")
	obs.PRs["parent"] = gh.PR{Number: 1, HeadRef: "parent", BaseRef: "main", State: gh.Merged}
	obs.PRs["child"] = gh.PR{Number: 2, HeadRef: "child", BaseRef: childBaseRef, State: gh.Open}
	delete(obs.BranchTips, "parent")
	return obs
}

func TestRetargetRepointsAnOpenDescendantAtMainWhenItsParentMerges(t *testing.T) {
	// The axis is the base GitHub reports the descendant's PR on: ours to re-point, or already
	// re-pointed by GitHub itself.
	for _, childBaseRef := range []string{"parent", "main"} {
		t.Run("github has it on "+childBaseRef, func(t *testing.T) {
			// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
			root, repoPath := repoWithOrigin(t)
			ghLog := installFakeGh(t, false)
			store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
			at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

			f := newStackedFixture(t, repoPath, store, at)
			childTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))
			mergeParentIntoMain(t, repoPath)
			mainSHA := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

			obs := mergedObservation(f, childBaseRef)
			observe := func(context.Context) (cc.Observation, error) { return obs, nil }
			cfg, ws := stackedConfigAndWorkspace(t, root)
			loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
			if err := loop.RunOnce(t.Context()); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}

			if got := ghLogLines(t, ghLog, "pr edit"); len(got) != 1 || got[0] != "pr edit child --base main" {
				t.Errorf("gh pr edit calls = %q, want exactly [pr edit child --base main]", got)
			}

			pushes, err := store.LatestPushes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			row := pushes[f.child.TicketURL]
			if row.BaseBranch != "main" {
				t.Errorf("child's recorded base = %q, want main", row.BaseBranch)
			}
			if row.PushedTip != childTip {
				t.Errorf("child's recorded pushed tip = %q, want %q unchanged: nothing was pushed",
					row.PushedTip, childTip)
			}
			if row.BaseSHAAtPush != mainSHA {
				t.Errorf("child's recorded base sha = %q, want origin/main's tip %q", row.BaseSHAAtPush, mainSHA)
			}

			events, err := store.Events(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !hasEvent(events, "retargeted", "child") {
				t.Errorf("events = %+v, want a retargeted event naming the child's branch", events)
			}
			if hasEvent(events, "pushed", "") {
				t.Errorf("events = %+v, want no pushed event: the same pushed tip against a new base "+
					"must leave plan.PushPlan selecting nothing", events)
			}
		})
	}
}

func TestASecondTickOverARetargetedRowAppendsNoDuplicatePushRow(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	mergeParentIntoMain(t, repoPath)

	obs := mergedObservation(f, "main")
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)

	firstTick := at.Add(time.Minute)
	if err := cc.NewLoop(store, observe, fixedClock(firstTick), cfg, ws, cc.ProcessRunner{}).
		RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if err := cc.NewLoop(store, observe, fixedClock(at.Add(time.Hour)), cfg, ws, cc.ProcessRunner{}).
		RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	pushes, err := store.LatestPushes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := pushes[f.child.TicketURL].PushedAt; !got.Equal(firstTick) {
		t.Errorf("child's latest push row is stamped %s, want the first tick's %s: an unchanged base "+
			"must not append a second row", got, firstTick)
	}
	if got := ghLogLines(t, ghLog, "pr edit"); len(got) != 1 {
		t.Errorf("gh pr edit calls = %q, want exactly one: the recorded base is already main", got)
	}
}

func TestRefreshOnARetargetedRowMergesOriginMainAndNeverTheDeletedParent(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	mergeParentIntoMain(t, repoPath)

	obs := mergedObservation(f, "main")
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("retarget RunOnce: %v", err)
	}

	// Someone else's work lands on main after the retarget, so the merge the refresh performs is
	// observable rather than an already-up-to-date no-op.
	advanceMain(t, root, "main-fix.txt", "someone else's landed work\n")
	runGit(t, "-C", repoPath, "fetch", "-q", "--prune", "origin")
	if err := store.QueueVerbIntent(t.Context(), f.child.TicketURL, plan.VerbRefresh, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("refresh RunOnce: %v", err)
	}

	if _, err := os.Stat(filepath.Join(f.childWorktree, "main-fix.txt")); err != nil {
		t.Errorf("child worktree does not contain main's later commit after refresh: %v", err)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "refreshed", "merged origin/child then origin/main") {
		t.Errorf("events = %+v, want a refreshed event naming origin/main, never the deleted parent", events)
	}
}

// TestAFailedRetargetRecordsAnEventAndNeverStallsTheTick covers the hazard the step's own
// precondition creates: a task whose recorded base stays non-main is a candidate on every later
// tick, so aborting the tick on a `gh pr edit` gh refuses would stall every other task's push,
// verdict and launch for as long as that one pull request stays un-editable.
func TestAFailedRetargetRecordsAnEventAndNeverStallsTheTick(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	ghLog := installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	mergeParentIntoMain(t, repoPath)
	if err := os.WriteFile(filepath.Join(filepath.Dir(mustLookPath(t, "gh")), "gh"),
		[]byte("#!/bin/sh\necho \"$*\" >> \""+ghLog+"\"\necho 'fake gh: pr edit refused' >&2\nexit 1\n"),
		0o755); err != nil {
		t.Fatal(err)
	}

	obs := mergedObservation(f, "parent")
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "retarget_failed", "pr edit refused") {
		t.Errorf("events = %+v, want a retarget_failed event naming gh's own refusal", events)
	}
	if hasEvent(events, "retargeted", "") {
		t.Errorf("events = %+v, want no retargeted event: gh refused", events)
	}

	pushes, err := store.LatestPushes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := pushes[f.child.TicketURL].BaseBranch; got != "parent" {
		t.Errorf("child's recorded base = %q, want parent unchanged: only a successful edit records one", got)
	}

	// The next tick retries, since a retarget is idempotent and nothing was recorded.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if got := ghLogLines(t, ghLog, "pr edit"); len(got) != 2 {
		t.Errorf("gh pr edit calls over two ticks = %q, want two: a refused retarget is retried", got)
	}
}

// TestARetargetedRowExpiresAgainIfMainAdvancesPastTheRetarget covers issue #85's fourth incident:
// retargetOne's re-point onto main is itself a base_sha_at_push, so it goes stale the same way a
// still-stacked row does the next time its base moves -- here, main moving again after the
// retarget, not just the parent branch retargetOne already accounted for.
func TestARetargetedRowExpiresAgainIfMainAdvancesPastTheRetarget(t *testing.T) {
	t.Parallel()

	const parentTip, childTip = "parent-tip", "child-tip"
	tasks := []cc.Task{
		{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	prs := map[string]gh.PR{
		"parent": {Number: 1, State: gh.Merged, BaseRef: "main"},
		"child": {
			Number: 2, State: gh.Open, HeadOid: childTip, BaseRef: "main",
			Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
		},
	}
	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}

	stateOfChild := func(t *testing.T, retargeted bool, observedMainTip string) string {
		t.Helper()
		ctx := t.Context()
		store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
		if err := store.UpsertTasks(ctx, tasks); err != nil {
			t.Fatal(err)
		}
		dispositionAsPushed(t, store, "sandbox://CHILD", at)
		if err := store.RecordPush(ctx, "sandbox://CHILD", childTip, "parent", parentTip, at); err != nil {
			t.Fatal(err)
		}
		if retargeted {
			if err := store.RecordPush(ctx, "sandbox://CHILD", childTip, "main", "main-tip", at); err != nil {
				t.Fatal(err)
			}
		}
		obs := cc.Observation{
			Worktrees:  map[string]string{"child": "/repos/child"},
			BranchTips: map[string]string{"repo//main": observedMainTip},
			PRs:        prs,
		}
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatal(err)
		}
		return rowState(t, renderPage(t, cc.NewServer(store, fixedClock(at), repos, nil, "")), "sandbox://CHILD")
	}

	if got := stateOfChild(t, false, "main-tip-later"); got != "base_moved" {
		t.Fatalf("child's state before the retarget = %q, want base_moved: the control this test rests on", got)
	}
	if got := stateOfChild(t, true, "main-tip"); got != "review_me" {
		t.Errorf("child's state right after the retarget = %q, want review_me: the retarget's own recorded "+
			"main tip still matches what's observed, so nothing has moved since", got)
	}
	if got := stateOfChild(t, true, "main-tip-later"); got != "base_moved" {
		t.Errorf("child's state = %q, want base_moved: main advanced again after the retarget, so this "+
			"main-based row is exactly as stale as a still-stacked row would be", got)
	}
}

func ghLogLines(t *testing.T, logPath, prefix string) []string {
	t.Helper()
	body, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.HasPrefix(line, prefix) {
			lines = append(lines, line)
		}
	}
	return lines
}

func advanceMain(t *testing.T, root, relPath, contents string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "main-clone")
	runGit(t, "clone", "-q", filepath.Join(root, "remote.git"), clone)
	commitFile(t, clone, relPath, contents)
	runGit(t, "-C", clone, "push", "-q", "origin", "HEAD:main")
}

// TestARetargetOntoMainWhoseContentConflictsEndsRefreshConflicted covers issue #85's fourth root
// cause: retargetOne used to record main's own current tip as the row's base_sha_at_push without
// ever merging main, so baseMoved read "not stale" forever and the board showed review_me over a
// pull request GitHub already called dirty.
func TestARetargetOntoMainWhoseContentConflictsEndsRefreshConflicted(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	commitFile(t, f.childWorktree, "parent.txt", "the child's rewrite\n")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "child")
	childTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))
	if err := store.RecordPush(t.Context(), f.child.TicketURL, childTip, "parent", f.parentTip0, at); err != nil {
		t.Fatal(err)
	}

	mergeParentIntoMain(t, repoPath)
	advanceMain(t, root, "parent.txt", "main's own rewrite\n")
	runGit(t, "-C", repoPath, "fetch", "-q", "--prune", "origin")
	mainSHA := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

	obs := mergedObservation(f, "parent")
	obs.BranchTips["repo//main"] = mainSHA
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "refresh_conflicted", "") {
		t.Errorf("events = %+v, want a refresh_conflicted event: retargeting onto main must merge main, "+
			"and this child's parent.txt conflicts with main's own", events)
	}
	if hasEvent(events, "refreshed", "") {
		t.Errorf("events = %+v, want no refreshed event: the merge conflicted", events)
	}

	mid, err := cc.MidMerge(t.Context(), f.childWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if !mid {
		t.Errorf("child worktree is not left mid-merge, so the next tick's observation cannot derive " +
			"refresh_conflicted and the board falls back to the pull request's own verdict")
	}
}

// squashParentIntoMain is what GitHub's squash-merge leaves behind, and what mergeParentIntoMain
// deliberately is not: main carries the parent's content under one new commit sharing no
// ancestry with the parent's own, and the branch is gone from the remote.
func squashParentIntoMain(t *testing.T, root, repoPath string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "squash-clone")
	runGit(t, "clone", "-q", filepath.Join(root, "remote.git"), clone)
	runGit(t, "-C", clone, "merge", "-q", "--squash", "origin/parent")
	runGit(t, "-C", clone, "commit", "-q", "-m", "the parent's work (#1)")
	runGit(t, "-C", clone, "push", "-q", "origin", "HEAD:main")
	runGit(t, "-C", repoPath, "push", "-q", "origin", "--delete", "parent")
	runGit(t, "-C", repoPath, "fetch", "-q", "--prune", "origin")
}

func isAncestor(t *testing.T, repoPath, commit, ref string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor", commit, ref)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	if err != nil {
		t.Fatalf("merge-base --is-ancestor %s %s: %v", commit, ref, err)
	}
	return true
}

// TestASquashMergedParentIsRestackedAwayInsteadOfMergedBack covers issue #89. A squash writes
// main a commit with no ancestry to the branch it came from, so merging main back into a
// descendant replays that descendant's own copies of the parent's commits against it and
// conflicts on every line either side touched. The parent's work has to be dropped, not merged.
func TestASquashMergedParentIsRestackedAwayInsteadOfMergedBack(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	childTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/child"))
	squashParentIntoMain(t, root, repoPath)
	mainSHA := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/main"))

	obs := mergedObservation(f, "parent")
	obs.PRs["parent"] = gh.PR{Number: 1, HeadRef: "parent", BaseRef: "main", State: gh.Merged, HeadOid: f.parentTip0}
	obs.BranchTips["repo//main"] = mainSHA
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("retarget RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "restacked", "restacked onto origin/main") {
		t.Fatalf("events = %+v, want a restacked event naming a restack onto origin/main", events)
	}
	if hasEvent(events, "refresh_conflicted", "") {
		t.Errorf("events = %+v, want no conflict: dropping the parent's commits is what avoids it", events)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD^")); got != mainSHA {
		t.Errorf("child's parent commit = %q, want main's squash %q: the branch sits on main now", got, mainSHA)
	}
	if isAncestor(t, f.childWorktree, f.parentTip0, "HEAD") {
		t.Errorf("the parent's own commit %s is still in the child's history; the squash already "+
			"carries that work, so replaying it is exactly what conflicts", f.parentTip0)
	}
	for _, name := range []string{"parent.txt", "child.txt"} {
		if _, err := os.Stat(filepath.Join(f.childWorktree, name)); err != nil {
			t.Errorf("child worktree is missing %s after the restack: %v", name, err)
		}
	}

	// The restack rewrote the branch, so the next tick's push has to lease rather than
	// fast-forward, or the work never reaches the pull request. This also pins the comparison
	// RestackedSinceLastPush makes: retargetOne stamps its push row and this restack with one
	// clock reading, so a strict > there leaves the branch stranded at its pre-restack tip.
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("push RunOnce: %v", err)
	}
	local := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD"))
	remote := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/child"))
	if remote != local {
		t.Errorf("origin/child = %q, want the restacked tip %q (it was %q before the restack)",
			remote, local, childTip)
	}
}

// TestABaseBranchRewrittenUnderARowIsRestackedOntoNotMergedBack is the cascade the restack
// itself creates: once the app rewrites one branch of a stack, every branch sitting on it is in
// the same position as a descendant of a squash, with its recorded base tip no longer reachable.
func TestABaseBranchRewrittenUnderARowIsRestackedOntoNotMergedBack(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	commitFile(t, f.parentWorktree, "parent.txt", "the parent's reworked take\n")
	runGit(t, "-C", f.parentWorktree, "reset", "-q", "--hard", "HEAD~2")
	commitFile(t, f.parentWorktree, "parent.txt", "the parent's reworked take\n")
	runGit(t, "-C", repoPath, "push", "-q", "--force", "origin", "parent")
	runGit(t, "-C", repoPath, "fetch", "-q", "--prune", "origin")
	newParentTip := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/parent"))

	obs := baseObservation(f, newParentTip)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "restacked", "restacked onto origin/parent") {
		t.Fatalf("events = %+v, want a restacked event naming a restack onto origin/parent", events)
	}
	if isAncestor(t, f.childWorktree, f.parentTip0, "HEAD") {
		t.Errorf("the parent's pre-rewrite tip %s is still in the child's history, so the child "+
			"carries both takes on parent.txt", f.parentTip0)
	}
	if got := strings.TrimSpace(runGitOutput(t, "-C", f.childWorktree, "rev-parse", "HEAD^")); got != newParentTip {
		t.Errorf("child's parent commit = %q, want the parent's rewritten tip %q", got, newParentTip)
	}
}

// TestARewriteTheAppDidNotPerformIsNeverForcePushed covers the licence the restack needs and
// must not hand out generally (issue #89). An agent's own amend or reset in its worktree leaves
// the branch exactly as diverged from origin as a restack does, and that is a push failed for a
// human to look at, not something to overwrite the pull request with.
func TestARewriteTheAppDidNotPerformIsNeverForcePushed(t *testing.T) {
	// Not t.Parallel(): installFakeGh and repoWithOrigin both use t.Setenv.
	root, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	f := newStackedFixture(t, repoPath, store, at)
	remoteBefore := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/child"))

	runGit(t, "-C", f.childWorktree, "reset", "-q", "--hard", "HEAD~1")
	commitFile(t, f.childWorktree, "child.txt", "the child's second thoughts\n")

	obs := baseObservation(f, f.parentTip0)
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }
	cfg, ws := stackedConfigAndWorkspace(t, root)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, cc.ProcessRunner{})
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "origin/child")); got != remoteBefore {
		t.Errorf("origin/child = %q, want %q untouched: the app never rewrote this branch, so the "+
			"commit that was there is the agent's to account for, not the app's to discard", got, remoteBefore)
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "push_failed", "") {
		t.Errorf("events = %+v, want a push_failed event: a diverged branch the app did not restack "+
			"is a human's problem", events)
	}
}
