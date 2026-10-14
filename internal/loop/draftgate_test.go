package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

// draftGateFixture is a consumer ticket with a cross-repo gating blocker, pushed and open, ready
// for applyDraftGate to decide over -- the shape every test below starts from.
type draftGateFixture struct {
	store *storepkg.Store
	cfg   config.Config
	ws    config.Workspace
	at    time.Time
	tip   string
}

func newDraftGateFixture(t *testing.T) draftGateFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(config.CheckoutPath(root, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}

	store := openStore(t)
	consumer := storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		BlockedBy: []string{"sandbox://PLA-40"},
	}
	blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{consumer, blocker}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, consumer.URL, at)
	const tip = "cc-1-tip"
	push := storepkg.Push{Ticket: plan.TicketURL(consumer.URL), Tip: tip, BaseSHA: "main-tip", BaseBranch: "main", At: at}
	if err := store.RecordPush(t.Context(), push); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{DataDir: root}
	ws := config.Workspace{RunsDir: t.TempDir(), SettingsPath: filepath.Join(t.TempDir(), "agent.json")}
	return draftGateFixture{store: store, cfg: cfg, ws: ws, at: at, tip: tip}
}

func ciCheck(conclusion string) map[string]plan.CheckState {
	return map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: conclusion}}
}

func draftConsumerPR(
	blockerState plan.PRState, consumerChecks map[string]plan.CheckState, tip string,
) plan.Observation {
	return plan.Observation{
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "cc-1"): {
				Number: 1, State: plan.Open, IsDraft: true, HeadOid: plan.SHA(tip), Checks: consumerChecks,
			},
			branchRef("services", "pla-40"): {State: blockerState},
		},
		BranchTips: map[plan.BranchRef]plan.SHA{loop.MainTipKey("repo"): "main-tip"},
		Settings:   map[string]config.RepoSettings{"repo": {Checks: verdict.Predicate{Success: "CI"}}},
	}
}

func TestDraftGateStaysDraftWhileGatingBlockerIsOpen(t *testing.T) {
	t.Parallel()
	f := newDraftGateFixture(t)
	forge := &recordingForge{}

	obs := draftConsumerPR(plan.Open, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := len(forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker is still open", got)
	}
}

func TestDraftGateStaysDraftWhileVerdictIsNotGreen(t *testing.T) {
	t.Parallel()
	f := newDraftGateFixture(t)
	forge := &recordingForge{}

	obs := draftConsumerPR(plan.Merged, ciCheck("FAILURE"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := len(forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the consumer's own CI has not gone green", got)
	}
}

func TestDraftGateUnDraftsOnceAndCallsReadyExactlyOnce(t *testing.T) {
	t.Parallel()
	f := newDraftGateFixture(t)
	forge := &recordingForge{}

	obs := draftConsumerPR(plan.Open, ciCheck("FAILURE"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{}, loop.WithForge(forge))

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if got := len(forge.Calls("Ready")); got != 0 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 0", got)
	}

	// Blocker merges, but the consumer's own CI is still red.
	obs = draftConsumerPR(plan.Merged, ciCheck("FAILURE"), f.tip)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := len(forge.Calls("Ready")); got != 0 {
		t.Fatalf("pr ready calls after tick 2 = %d, want 0", got)
	}

	// Both halves now hold: this is the tick that must call `gh pr ready`.
	obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), f.tip)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	if got := len(forge.Calls("Ready")); got != 1 {
		t.Fatalf("pr ready calls after tick 3 = %d, want exactly 1", got)
	}

	// GitHub now reports the PR as ready, as a real observe would from here on.
	obs.PRs[branchRef("repo", "cc-1")] = plan.PR{
		Number: 1, State: plan.Open, IsDraft: false, HeadOid: plan.SHA(f.tip), Checks: ciCheck("SUCCESS"),
	}
	for i := range 9 {
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("tick %d: %v", i+4, err)
		}
	}
	if got := len(forge.Calls("Ready")); got != 1 {
		t.Errorf("pr ready calls across ten ticks = %d, want exactly 1", got)
	}
}

func TestDraftGateClosedBlockerNeverReadies(t *testing.T) {
	t.Parallel()
	f := newDraftGateFixture(t)
	forge := &recordingForge{}

	obs := draftConsumerPR(plan.Closed, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{}, loop.WithForge(forge))

	for i := range 5 {
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
	}
	if got := len(forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker's PR closed unmerged", got)
	}

	server := openServer(f.store, fixedClock(f.at), "")
	page := renderPage(t, server)
	state := rowState(t, page, "sandbox://CC-1")
	if state == "base_gone" {
		t.Error("state = base_gone, which is reserved for a stacking parent, never a gating edge")
	}
}

func TestDraftGateReadyFailureIsRetriedNextTickWithoutAVerb(t *testing.T) {
	t.Parallel()
	f := newDraftGateFixture(t)
	forge := &recordingForge{}

	obs := draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{},
		loop.WithForge(failingReady{forge}))

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if got := len(forge.Calls("Ready")); got != 1 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 1 (the failed attempt)", got)
	}

	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "draft_ready_failed", "fake gh: pr ready failed") {
		t.Errorf("events = %+v, want a draft_ready_failed event naming the failure", events)
	}

	lp = newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := len(forge.Calls("Ready")); got != 2 {
		t.Errorf("pr ready calls after tick 2 = %d, want 2 (retried automatically)", got)
	}

	events, err = f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "draft_ready", "") {
		t.Errorf("events = %+v, want a draft_ready event for the successful retry", events)
	}
}

func TestDraftGateReadyFailurePreservesGhStderr(t *testing.T) {
	f := newDraftGateFixture(t)
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'fake gh: pr ready failed' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	obs := draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), f.tip)
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }
	lp := newLoop(f.store, observe, fixedClock(f.at), f.cfg, f.ws, runner.ProcessRunner{})
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	events, err := f.store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "draft_ready_failed", "fake gh: pr ready failed") {
		t.Errorf("events = %+v, want gh stderr in draft_ready_failed", events)
	}
}

func TestDraftPRCountsAsOpenForASameRepoDependent(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "parent"): "/repos/parent"},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "parent"): {Number: 1, State: plan.Open, IsDraft: true}},
		Settings:  map[string]config.RepoSettings{"repo": {Stacking: true}},
	}
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}

	server := openServer(store, fixedClock(at), "")
	page := renderPage(t, server)

	if stack := rowCellAt(t, page, "sandbox://CHILD", 4); stack != "L2 ← parent" {
		t.Errorf("child's stack = %q, want it based on parent: a draft PR is still OPEN for unlock purposes", stack)
	}
	if state := rowState(t, page, "sandbox://CHILD"); state != "ready" {
		t.Errorf("child's state = %q, want ready", state)
	}
}

func TestPushOneOpensADraftPRForATicketWithAGatingEdge(t *testing.T) {
	t.Parallel()
	root, repoPath := repoWithOrigin(t)
	forge := &recordingForge{}

	worktreePath := cutWorktree(t, repoPath, "cc-1")
	gittest.Commit(t, worktreePath, "agent.txt", "agent was here\n")

	store := openStore(t)
	consumer := storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1",
		BlockedBy: []string{"sandbox://PLA-40"},
	}
	blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{consumer, blocker}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, consumer.URL, at)

	obs := plan.NewObservation(at)
	obs.Worktrees[branchRef("repo", "cc-1")] = worktreePath
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := forge.Calls("Create"); len(got) != 1 || !got[0].Draft {
		t.Errorf("forge Create calls = %+v, want one draft PR for a ticket with a gating edge", got)
	}
}

func TestPushOneOpensANonDraftPRWithNoGatingEdge(t *testing.T) {
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

	obs := plan.NewObservation(at)
	obs.Worktrees[branchRef("repo", "cc-1")] = worktreePath
	observe := func(context.Context) (plan.Observation, error) { return obs, nil }

	cfg, ws := testConfigAndWorkspace(t, root, 0, nil)
	lp := newLoop(store, observe, fixedClock(at), cfg, ws, runner.ProcessRunner{}, loop.WithForge(forge))
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := forge.Calls("Create"); len(got) != 1 || got[0].Draft {
		t.Errorf("forge Create calls = %+v, want one non-draft PR for a plain ticket", got)
	}
}
