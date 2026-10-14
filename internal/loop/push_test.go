package loop_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func cutWorktree(t *testing.T, repoPath, branch string) string {
	t.Helper()
	worktreePath := filepath.Join(t.TempDir(), "wt-"+branch)
	gittest.Run(t, repoPath, "worktree", "add", "-b", branch, worktreePath, "origin/main")
	return worktreePath
}

// dispositionAsPushed records a run whose disposition is already known to be push, so these
// tests can drive pushPushable directly without re-exercising spawn/dispose (loop_launch_test.go
// already covers that).
func dispositionAsPushed(t *testing.T, store *storepkg.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	spawn := storepkg.Spawn{RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl"}
	if err := store.RecordSpawn(t.Context(), spawn); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

func countEvents(events []storepkg.Event, kind storepkg.EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func hasEvent(events []storepkg.Event, kind storepkg.EventKind, detailSubstring string) bool {
	for _, e := range events {
		if e.Kind == kind && strings.Contains(e.Detail, detailSubstring) {
			return true
		}
	}
	return false
}

func TestPushPushableRefusesAPolicyHitAndNeverPushes(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, ".github/workflows/x.yml", "name: x\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
		t.Error("cc-1 must not exist on the remote: the diff hit the push policy")
	}

	facts, err := store.PushFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := facts[plan.TicketURL(ticket.URL)]
	if !f.Refused || f.RefusedPath != ".github/workflows/x.yml" {
		t.Errorf("push facts = %+v, want refused naming .github/workflows/x.yml", f)
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "push_refused", ".github/workflows/x.yml") {
		t.Errorf("events = %+v, want a push_refused event naming the path", events)
	}

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	events, err = store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := countEvents(events, "push_refused"); got != 1 {
		t.Errorf("push_refused events after a second tick = %d, want 1: no auto-retry", got)
	}
}

func TestPushPushablePushesAndCreatesAPROnceThenStaysIdempotent(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
		t.Fatal("cc-1 was not pushed to the remote")
	}
	calls := forge.Calls("Create")
	if len(calls) != 1 || calls[0].Repo != worktreePath || calls[0].Base != "main" || calls[0].Draft {
		t.Errorf("Create calls = %+v, want one non-draft PR based on main", calls)
	}

	tips, err := store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[plan.TicketURL(ticket.URL)] == "" {
		t.Error("no pushes row recorded")
	}

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if got := len(forge.Calls("Create")); got != 1 {
		t.Errorf("Create calls after a second tick = %d, want 1", got)
	}
}

func TestPushPushableAdoptsAnExistingOpenPRRatherThanDuplicating(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "cc-1"): {Number: 7, HeadRef: "cc-1", State: plan.Open}},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if !gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
		t.Error("cc-1 was not pushed to the remote")
	}
	if got := len(forge.Calls("Create")); got != 0 {
		t.Errorf("Create calls = %d, want 0: an open PR must be adopted, never duplicated", got)
	}

	tips, err := store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[plan.TicketURL(ticket.URL)] == "" {
		t.Error("no pushes row recorded despite adopting the existing PR")
	}
}

func TestPushFailureIsNotRetriedAutomaticallyButRetryPushBypassesTheGate(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}
	failing := failingCreate{forge}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "cc-1"): worktreePath}, PRs: map[plan.BranchRef]plan.PR{},
	}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(failing))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !gittest.HasRef(t, filepath.Join(root, "remote.git"), "refs/heads/cc-1") {
		t.Fatal("the push itself must still succeed even though pr create fails")
	}
	if got := len(forge.Calls("Create")); got != 1 {
		t.Fatalf("Create calls = %d, want 1 (the failed attempt)", got)
	}

	facts, err := store.PushFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !facts[plan.TicketURL(ticket.URL)].Failed {
		t.Fatal("push facts must record the failure")
	}
	tips, err := store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[plan.TicketURL(ticket.URL)] != "" {
		t.Error("no pushes row until the PR is actually opened")
	}

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if got := len(forge.Calls("Create")); got != 1 {
		t.Errorf("Create calls after an automatic tick = %d, want 1: no auto-retry", got)
	}

	lp = newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := store.QueueVerbIntent(t.Context(), ticket.URL, "retry-push", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry RunOnce: %v", err)
	}
	if got := len(forge.Calls("Create")); got != 2 {
		t.Fatalf("Create calls after retry-push = %d, want 2", got)
	}
	tips, err = store.LastPushedTips(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tips[plan.TicketURL(ticket.URL)] == "" {
		t.Error("pushes row missing after a successful retry-push")
	}

	pending, err := store.PendingVerbIntents(t.Context(), "retry-push")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending retry-push intents = %+v, want none: consumed", pending)
	}
}

func TestPushPushableSkipsATicketWhoseBranchWasRemoved(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")
	gittest.Run(t, repoPath, "push", "-q", "origin", "cc-1")

	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, ticket.URL, at)
	// Already fully pushed and recorded, mirroring a merged/base_gone row that has since been
	// torn down: RecordPush's own tip must match, or PushPlan would select it regardless.
	tip := gittest.Run(t, repoPath, "rev-parse", "refs/heads/cc-1")
	push := storepkg.Push{
		Ticket: plan.TicketURL(ticket.URL), Tip: plan.SHA(tip), BaseSHA: plan.SHA(tip), BaseBranch: "main", At: at,
	}
	if err := store.RecordPush(t.Context(), push); err != nil {
		t.Fatal(err)
	}

	// The worktree and the branch are both gone -- tp remove --force's real effect.
	gittest.Run(t, repoPath, "worktree", "remove", "--force", worktreePath)
	gittest.Run(t, repoPath, "branch", "-D", "cc-1")

	obs := plan.Observation{Worktrees: map[plan.BranchRef]string{}, PRs: map[plan.BranchRef]plan.PR{}}
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(&recordingForge{}))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce must not fail once a pushed ticket's branch has been removed: %v", err)
	}
	// A second tick too: the hazard is that every future tick would fail, not just the first.
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
}
