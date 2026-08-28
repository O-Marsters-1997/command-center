package cc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func noOpObserve(context.Context) (cc.Observation, error) { return cc.Observation{}, nil }

func authoriseTask(t *testing.T, store *cc.Store, ticketURL, hash string, at time.Time) {
	t.Helper()
	if err := store.QueueLaunchIntent(t.Context(), ticketURL, hash, "group-"+ticketURL, at); err != nil {
		t.Fatal(err)
	}
}

func TestLoopCutsAndSpawnsAnEligibleTask(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}))
	authoriseTask(t, store, task.TicketURL, hash, at)

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	spawned := fake.spawns[0]
	if !strings.HasSuffix(spawned.WorktreePath, "wt-cc-1") {
		t.Errorf("worktree path = %q, want it to end in wt-cc-1", spawned.WorktreePath)
	}
	if spawned.SettingsPath != ws.SettingsPath {
		t.Errorf("settings path = %q, want %q", spawned.SettingsPath, ws.SettingsPath)
	}
	if _, err := os.Stat(spawned.PromptPath); err != nil {
		t.Errorf("prompt file was not written: %v", err)
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[task.TicketURL]
	if !ok {
		t.Fatal("no run recorded for sandbox://CC-1")
	}
	if summary.Pgid == nil || *summary.Pgid != 1 {
		t.Errorf("pgid = %v, want 1", summary.Pgid)
	}
	if summary.BaselineSHA == "" {
		t.Error("baseline_sha is empty")
	}
	if summary.HasOutcome {
		t.Error("a freshly spawned run must not already have an outcome")
	}
}

// TestLoopWritesTheComposedPromptAndTicketBody goldens the file written for the agent: the
// implement instruction the preview showed at authorisation, then the ticket's body.
func TestLoopWritesTheComposedPromptAndTicketBody(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}))
	authoriseTask(t, store, task.TicketURL, hash, at)

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}

	written, err := os.ReadFile(fake.spawns[0].PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, goldenPrompt, written)
}

// TestLoopNeverSpawnsOnAPromptHashMismatch covers issue #55's AC1: a member authorised against a
// hash the task no longer composes to sits queued forever, spawning nothing, however many ticks
// pass -- the tick refuses on the mismatch rather than composing around it.
func TestLoopNeverSpawnsOnAPromptHashMismatch(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	authoriseTask(t, store, task.TicketURL, plan.Hash("a prompt this task never composed to"), at)

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	for i := 0; i < 3; i++ {
		if err := loop.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	if len(fake.spawns) != 0 {
		t.Errorf("spawns = %d, want 0: a hash the task no longer composes to must never be spawned", len(fake.spawns))
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ran := latest[task.TicketURL]; ran {
		t.Error("no run should ever be recorded for a task whose authorised hash no longer matches")
	}
}

func TestLoopRecordsCutFailedWithoutClaimingAPgid(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, true)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}))
	authoriseTask(t, store, task.TicketURL, hash, at)

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 0 {
		t.Errorf("spawns = %d, want 0: a cut failure must never reach Spawn", len(fake.spawns))
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[task.TicketURL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeCutFailed {
		t.Errorf("summary = %+v, want cut_failed", summary)
	}
	if summary.Pgid != nil {
		t.Errorf("pgid = %v, want nil: a cut failure never claims a pgid", summary.Pgid)
	}
}

func TestLoopCapsLaunchesAtMaxAgentsMinusCurrentlyRunning(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"},
		{TicketURL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2"},
	}
	if err := store.UpsertTasks(t.Context(), tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, task := range tasks {
		hash := plan.Hash(plan.Compose(plan.Task{TicketURL: task.TicketURL}))
		authoriseTask(t, store, task.TicketURL, hash, at)
	}

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want exactly 1 (max_agents = 1)", len(fake.spawns))
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ran := latest["sandbox://CC-1"]; !ran {
		t.Error("sandbox://CC-1 (first in ticket order) should have launched")
	}
	if _, ran := latest["sandbox://CC-2"]; ran {
		t.Error("sandbox://CC-2 should have stayed queued: no free slot")
	}
}

func TestLoopDisposesADeadRunByCommitsAfterItsOwnBaseline(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv, which panics after t.Parallel().
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	runID, err := store.InsertRunSkeleton(t.Context(), task.TicketURL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(t.Context(), runID, 999, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	fake.canReap[999] = true
	fake.reapCode[999] = 0
	// alive defaults to false in the map (zero value), i.e. the run reads dead this tick.

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(30*time.Second)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[task.TicketURL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeFailed {
		t.Fatalf("summary = %+v, want failed (no commits after baseline)", summary)
	}
	if summary.ExitCode == nil || *summary.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", summary.ExitCode)
	}

	// A second run against the same branch, with a real commit after ITS OWN baseline: must
	// derive push, and the first run's now-stale baseline must not leak into this one.
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "agent work")
	newBaseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))
	runGit(t, "-C", worktreePath, "commit", "-q", "--allow-empty", "-m", "more agent work")

	runID2, err := store.InsertRunSkeleton(t.Context(), task.TicketURL, "agent", newBaseline, "hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), runID2, 1000, at, "/state/runs/2.jsonl"); err != nil {
		t.Fatal(err)
	}
	fake.canReap[1000] = true
	fake.reapCode[1000] = 0

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	latest, err = store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary = latest[task.TicketURL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
		t.Fatalf("summary = %+v, want push (one commit after its own baseline)", summary)
	}
}

func TestLoopAppliesAKillIntentThenDisposesTheNowDeadRun(t *testing.T) {
	// Not t.Parallel(): repoWithOrigin uses t.Setenv, which panics after t.Parallel().
	_, repoPath := repoWithOrigin(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, "-C", repoPath, "worktree", "add", "-b", "cc-1", worktreePath, "origin/main")
	baseline := strings.TrimSpace(runGitOutput(t, "-C", repoPath, "rev-parse", "refs/heads/cc-1"))

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	runID, err := store.InsertRunSkeleton(t.Context(), task.TicketURL, "agent", baseline, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(t.Context(), runID, 4242, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "kill", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	fake.alive[4242] = true
	fake.canReap[4242] = true
	fake.reapCode[4242] = 143

	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Minute)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.canceled) != 1 || fake.canceled[0] != 4242 {
		t.Errorf("canceled = %v, want [4242]", fake.canceled)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending kill intents = %+v, want none: consumed", pending)
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[task.TicketURL]
	if !summary.HasOutcome {
		t.Fatal("the killed run was not disposed of in the same tick")
	}
	if summary.ExitCode == nil || *summary.ExitCode != 143 {
		t.Errorf("exit code = %v, want 143", summary.ExitCode)
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func runGitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
