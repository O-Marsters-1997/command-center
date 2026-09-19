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

func TestFollowUpSpawnsAFreshRunInTheExistingWorktreeWithTheTypedPrompt(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntentWithPayload(
		t.Context(), ticket.URL, plan.VerbFollowUp, "fix the flaky assertion in TestThing", at,
	); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]gh.PR{},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	spawned := fake.spawns[0]
	if spawned.WorktreePath != worktreePath {
		t.Errorf("follow-up spawned in %q, want the existing worktree %q", spawned.WorktreePath, worktreePath)
	}
	if !strings.Contains(spawned.Prompt, "cc/skills/follow-up/SKILL.md") {
		t.Errorf("prompt = %q, want it to reference the follow-up skill", spawned.Prompt)
	}
	if !strings.Contains(spawned.Prompt, "fix the flaky assertion in TestThing") {
		t.Errorf("prompt = %q, want it to carry the typed instruction", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, "/implement") {
		t.Errorf("prompt = %q, want it never to compose the implement instruction", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, "diff -u") || strings.HasPrefix(spawned.Prompt, "---") {
		t.Errorf("prompt = %q, want no re-run diff preamble", spawned.Prompt)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[ticket.URL]
	if !ok {
		t.Fatal("no run recorded after follow-up")
	}
	if summary.Kind != "follow_up" {
		t.Errorf("kind = %q, want follow_up", summary.Kind)
	}

	promptOnDisk, err := os.ReadFile(filepath.Join(ws.RunsDir, "1.prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(promptOnDisk) != spawned.Prompt {
		t.Errorf("stored prompt = %q, want it to match the spawned prompt exactly: %q", promptOnDisk, spawned.Prompt)
	}

	pending, err := store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending follow-up intents = %+v, want none: consumed", pending)
	}
}

func TestFollowUpNeverTouchesAWorktreeWithALiveRun(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntentWithPayload(
		t.Context(), ticket.URL, plan.VerbFollowUp, "do the thing", at,
	); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		Worktrees: map[string]string{cc.BranchKey("repo", "cc-1"): worktreePath},
		Runs:      map[string]cc.RunObservation{ticket.URL: {Alive: true}},
	}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 0 {
		t.Fatalf("spawns = %d, want 0: a live run must never be spawned into again", len(fake.spawns))
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "follow_up_refused", "a run is alive") {
		t.Errorf("events = %+v, want a follow_up_refused naming the live run", events)
	}
}

func TestFollowUpRefusesWithNoWorktree(t *testing.T) {
	_, repoPath := repoWithOrigin(t)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntentWithPayload(
		t.Context(), ticket.URL, plan.VerbFollowUp, "do the thing", at,
	); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, filepath.Dir(repoPath), 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 0 {
		t.Fatalf("spawns = %d, want 0: follow-up never cuts a fresh worktree", len(fake.spawns))
	}
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "follow_up_refused", "no worktree") {
		t.Errorf("events = %+v, want a follow_up_refused naming the missing worktree", events)
	}
}
