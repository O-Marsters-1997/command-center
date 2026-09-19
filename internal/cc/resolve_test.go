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
)

func TestResolveSpawnsAgainstTheConflictSkillAndConsumesTheIntentOnce(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := cc.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		Body: "Implement the new widget exactly as described here.",
	}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
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
		t.Errorf("resolve spawned in %q, want the existing worktree %q", spawned.WorktreePath, worktreePath)
	}
	if !strings.Contains(spawned.Prompt, "cc/skills/resolve-merge-conflict/SKILL.md") {
		t.Errorf("prompt = %q, want it to reference the resolve skill", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, "/implement") {
		t.Errorf("prompt = %q, want it never to compose the implement instruction", spawned.Prompt)
	}
	if strings.Contains(spawned.Prompt, ticket.Body) {
		t.Errorf("prompt = %q, want it never to carry the ticket's own body: "+
			"resolve is reconciling a conflict, not implementing the ticket", spawned.Prompt)
	}

	pending, err := store.PendingVerbIntents(t.Context(), plan.VerbResolve)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending resolve intents = %+v, want none: consumed", pending)
	}
}

func TestResolveNeverTouchesAWorktreeWithALiveRun(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
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
	if !hasEvent(events, "resolve_refused", "a run is alive") {
		t.Errorf("events = %+v, want a resolve_refused naming the live run", events)
	}
}

func TestAResolveRunWithNoCommitsParksAsConflictResolved(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
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
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}

	pid := fake.nextPid
	fake.alive[pid] = false
	fake.canReap[pid] = true
	fake.reapCode[pid] = 0

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary := latest[ticket.URL]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeFailed {
		t.Fatalf("summary = %+v, want failed (zero commits after baseline)", summary)
	}

	server := cc.NewServer(store, fixedClock(at), cfg.Repos, "")
	page := renderPage(t, server)
	if state := rowState(t, page, ticket.URL); state != "conflict_resolved" {
		t.Fatalf("child's state = %q, want conflict_resolved", state)
	}
	row := rowHTML(t, page, ticket.URL)
	if !strings.Contains(row, "nothing committed") {
		t.Errorf("row does not name the resolution as unread, want a reason about nothing committed:\n%s", row)
	}
	if !strings.Contains(row, `value="`+plan.VerbCommitResolution+`"`) {
		t.Errorf("row does not offer commit-resolution, want the verb that commits and pushes it:\n%s", row)
	}
}

// TestReRunOnAConflictResolvedRowWithAGoneWorktreeCutsFreshAndUnsticksIt covers issue #198:
// ConflictResolved's own escape once its worktree is gone, relaunched as a plain agent run
// rather than another resolve attempt.
func TestReRunOnAConflictResolvedRowWithAGoneWorktreeCutsFreshAndUnsticksIt(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	runGit(t, "-C", repoPath, "worktree", "remove", "--force", worktreePath)

	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolveRunID, err := store.InsertRunSkeleton(t.Context(), ticket.URL, "resolve", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(t.Context(), resolveRunID, 111, at, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), resolveRunID, plan.OutcomeFailed, &exitCode, at); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbReRun, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{Worktrees: map[string]string{}, PRs: map[string]gh.PR{}}
	observe := func(context.Context) (cc.Observation, error) { return obs, nil }

	fake := newFakeRunner()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	loop := cc.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.spawns))
	}
	if !strings.HasSuffix(fake.spawns[0].WorktreePath, "wt-cc-1") {
		t.Errorf("re-run spawned in %q, want a freshly cut worktree", fake.spawns[0].WorktreePath)
	}

	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := latest[ticket.URL]
	if !ok || summary.ID == resolveRunID {
		t.Fatalf("summary = %+v, want a second run row distinct from the resolve run", summary)
	}
	if summary.Kind != "agent" {
		t.Errorf("kind = %q, want agent: the fresh relaunch is sourced fresh, not another resolve attempt", summary.Kind)
	}

	pid := fake.nextPid
	fake.alive[pid] = false
	fake.canReap[pid] = true
	fake.reapCode[pid] = 1

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	server := cc.NewServer(store, fixedClock(at.Add(time.Second)), cfg.Repos, "")
	page := renderPage(t, server)
	if state := rowState(t, page, ticket.URL); state == "conflict_resolved" {
		t.Fatalf("state = %q, want the row to have left conflict_resolved once the fresh run disposed", state)
	}
}
