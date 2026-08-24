package cc_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestReRunHandsTheNewRunADiffPreambleWhenTheStoredPromptDiffers(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)
	writeSeamFile(t, root, "one", "seam one content")

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1", Seams: []string{"one"}}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	planTask := plan.Task{TicketURL: task.TicketURL, Seams: task.Seams}
	hash := plan.Hash(plan.Compose(planTask, []string{"seam one content"}))
	authoriseTask(t, store, task.TicketURL, hash, at)

	obs := &cc.Observation{Worktrees: map[string]string{}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return *obs, nil }

	fake := newFakeRunner()
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns after first run = %d, want 1", len(fake.spawns))
	}
	obs.Worktrees["cc-1"] = fake.spawns[0].WorktreePath

	writeSeamFile(t, root, "one", "seam one content, edited")
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if len(fake.spawns) != 2 {
		t.Fatalf("spawns after re-run = %d, want 2", len(fake.spawns))
	}
	reRunSpawn := fake.spawns[1]

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	newRunID := latest[task.TicketURL].ID

	newPrompt, err := os.ReadFile(reRunSpawn.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	wantComposition := plan.Compose(planTask, []string{"seam one content, edited"})
	if !strings.HasPrefix(string(newPrompt), wantComposition) {
		t.Errorf("new .prompt file = %q, want it to start with the fresh composition %q", newPrompt, wantComposition)
	}
	if strings.Contains(string(newPrompt), "-seam one content\n") {
		t.Errorf("new .prompt file = %q, must not contain the diff preamble", newPrompt)
	}

	if reRunSpawn.Prompt == string(newPrompt) {
		t.Error("re-run's spawned prompt has no preamble, want the before/after diff prepended")
	}
	if !strings.Contains(reRunSpawn.Prompt, "-seam one content") ||
		!strings.Contains(reRunSpawn.Prompt, "+seam one content, edited") {
		t.Errorf("re-run's spawned prompt = %q, want a unified diff of the seam change", reRunSpawn.Prompt)
	}
	if !strings.HasSuffix(reRunSpawn.Prompt, string(newPrompt)) {
		t.Errorf("re-run's spawned prompt = %q, want the fresh composition after the diff preamble", reRunSpawn.Prompt)
	}

	diffPath := filepath.Join(ws.RunsDir, fmt.Sprintf("%d.diff", newRunID))
	diffOnDisk, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("diff file not written: %v", err)
	}
	if !strings.Contains(string(diffOnDisk), "-seam one content") {
		t.Errorf("diff file = %q, want the removed line", diffOnDisk)
	}
}

func TestReRunWithNoStoredPromptDegradesToNoDiff(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTasks(t.Context(), []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	firstRunID, err := store.InsertRunSkeleton(t.Context(), task.TicketURL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), firstRunID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	if err := store.RecordDisposition(t.Context(), firstRunID, plan.OutcomeFailed, &exitCode, at); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), task.TicketURL, "re-run", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{"cc-1": worktreePath}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: a missing stored prompt must never fail the re-run", len(fake.spawns))
	}
	spawned := fake.spawns[0]
	newPrompt, err := os.ReadFile(spawned.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if spawned.Prompt != string(newPrompt) {
		t.Errorf("spawned prompt = %q, want exactly the plain composition (no diff preamble)", spawned.Prompt)
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	newRunID := latest[task.TicketURL].ID
	diffPath := filepath.Join(ws.RunsDir, fmt.Sprintf("%d.diff", newRunID))
	if _, err := os.Stat(diffPath); !os.IsNotExist(err) {
		t.Errorf("diff file exists at %s, want none written when there is nothing to diff", diffPath)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "re_run_no_diff", "") {
		t.Error("no re_run_no_diff event logged for the missing stored prompt")
	}
}
