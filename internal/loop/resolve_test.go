package loop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func TestResolveSpawnsAgainstTheConflictSkillAndConsumesTheIntentOnce(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		Body: "Implement the new widget exactly as described here.",
	}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	spawned := fake.Spawns[0]
	if spawned.WorktreePath != worktreePath {
		t.Errorf("resolve spawned in %q, want the existing worktree %q", spawned.WorktreePath, worktreePath)
	}
	if spawned.SystemPromptPath != "" {
		t.Errorf("system prompt path = %q, want none: the single-shot warning is implement-only", spawned.SystemPromptPath)
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

func TestAResolveRunWithNoCommitsParksAsConflictResolved(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
		MidMerge:  map[string]bool{plan.BranchKey("repo", "cc-1"): true},
		HasStaged: map[string]bool{plan.BranchKey("repo", "cc-1"): true},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}

	pid := fake.NextPid
	fake.Alive[pid] = false
	fake.CanReap[pid] = true
	fake.ReapCode[pid] = 0

	if err := lp.RunOnce(t.Context()); err != nil {
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

	server := openServer(store, fixedClock(at), "")
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

	obs.MidMerge[plan.BranchKey("repo", "cc-1")] = false
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("third RunOnce: %v", err)
	}
	page = renderPage(t, server)
	if state := rowState(t, page, ticket.URL); state == "conflict_resolved" {
		t.Errorf("state = %q, want the row to leave conflict_resolved once the merge is committed", state)
	}
}

func TestReRunAfterAResolveRunReachesTheAgent(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	worktreePath := cutWorktree(t, repoPath, "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1", Body: "ticket body"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbResolve, at); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		Worktrees: map[string]string{plan.BranchKey("repo", "cc-1"): worktreePath}, PRs: map[string]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce (resolve): %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns after resolve = %d, want 1", len(fake.Spawns))
	}

	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbReRun, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce (re-run): %v", err)
	}
	if len(fake.Spawns) != 2 {
		t.Fatalf("spawns after re-run = %d, want 2: the re-run must still reach the agent", len(fake.Spawns))
	}

	reRunSpawn := fake.Spawns[1]
	if strings.HasPrefix(reRunSpawn.Prompt, "-") {
		t.Errorf("re-run's spawned prompt = %q, starts with '-': a CLI flag parser will refuse it "+
			"and the run never starts", reRunSpawn.Prompt)
	}
}

func TestReRunOnAConflictResolvedRowWithAGoneWorktreeCutsFreshAndUnsticksIt(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)
	worktreePath := cutWorktree(t, repoPath, "cc-1")
	runGit(t, "-C", repoPath, "worktree", "remove", "--force", worktreePath)

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
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
	if err := store.RecordDisposition(t.Context(), resolveRunID, plan.OutcomeFailed, &exitCode, at, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, plan.VerbReRun, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{Worktrees: map[string]string{}, PRs: map[string]plan.PR{}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	fake := runner.NewFake()
	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := loop.NewLoop(store, observe, fixedClock(at.Add(time.Second)), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(fake.Spawns))
	}
	if !strings.HasSuffix(fake.Spawns[0].WorktreePath, "wt-cc-1") {
		t.Errorf("re-run spawned in %q, want a freshly cut worktree", fake.Spawns[0].WorktreePath)
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

	pid := fake.NextPid
	fake.Alive[pid] = false
	fake.CanReap[pid] = true
	fake.ReapCode[pid] = 1

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	server := openServer(store, fixedClock(at.Add(time.Second)), "")
	page := renderPage(t, server)
	if state := rowState(t, page, ticket.URL); state == "conflict_resolved" {
		t.Fatalf("state = %q, want the row to have left conflict_resolved once the fresh run disposed", state)
	}
}
