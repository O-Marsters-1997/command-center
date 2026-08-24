package cc_test

import (
	"context"
	"errors"
	"os"
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
	advanceMain(t, root)
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

func TestARetargetedRowIsEvaluatedAsARootAndNeverReadsBaseMoved(t *testing.T) {
	t.Parallel()

	const parentTip, childTip = "parent-tip", "child-tip"
	tasks := []cc.Task{
		{TicketURL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{TicketURL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	// The parent's branch is gone and main has moved on, so the child's recorded base_sha can
	// never match again: nothing but the retarget's own push row retires the expiry.
	obs := cc.Observation{
		Worktrees:  map[string]string{"child": "/repos/child"},
		BranchTips: map[string]string{"main": "main-tip-later"},
		PRs: map[string]gh.PR{
			"parent": {Number: 1, State: gh.Merged, BaseRef: "main"},
			"child": {
				Number: 2, State: gh.Open, HeadOid: childTip, BaseRef: "main",
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "SUCCESS"}},
			},
		},
	}
	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}

	stateOfChild := func(t *testing.T, retargeted bool) string {
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
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatal(err)
		}
		return rowState(t, renderPage(t, cc.NewServer(store, fixedClock(at), repos, "")), "sandbox://CHILD")
	}

	if got := stateOfChild(t, false); got != "base_moved" {
		t.Fatalf("child's state before the retarget = %q, want base_moved: the control this test rests on", got)
	}
	if got := stateOfChild(t, true); got != "review_me" {
		t.Errorf("child's state after the retarget = %q, want review_me: a main-based row is a root, "+
			"so no stacked expiry applies to it", got)
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

func advanceMain(t *testing.T, root string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "main-clone")
	runGit(t, "clone", "-q", filepath.Join(root, "remote.git"), clone)
	commitFile(t, clone, "main-fix.txt", "someone else's landed work\n")
	runGit(t, "-C", clone, "push", "-q", "origin", "HEAD:main")
}
