package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func (f *pushFixture) seedDraftGate(t *testing.T) {
	t.Helper()
	f.Ticket.BlockedBy = []string{"sandbox://PLA-40"}
	blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	storetest.Tickets(t, f.Store, f.Ticket, blocker)
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	const tip = "cc-1-tip"
	storetest.Pushed(t, f.Store, storetest.PushedOpts{
		URL: f.Ticket.URL, Tip: tip, Base: "main", BaseTip: "main-tip", At: testAt,
	})
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
		BranchTips: map[plan.BranchRef]plan.SHA{branchRef("repo", plan.DefaultBaseBranch): "main-tip"},
		Settings:   map[string]config.RepoSettings{"repo": {Checks: verdict.Predicate{Success: "CI"}}},
	}
}

func TestDraftGateStaysDraftWhileGatingBlockerIsOpen(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	*f.Obs = draftConsumerPR(plan.Open, ciCheck("SUCCESS"), "cc-1-tip")
	f.tick(t)

	if got := len(f.Forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker is still open", got)
	}
}

func TestDraftGateStaysDraftWhileVerdictIsNotGreen(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("FAILURE"), "cc-1-tip")
	f.tick(t)

	if got := len(f.Forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the consumer's own CI has not gone green", got)
	}
}

func TestDraftGateUnDraftsOnceAndCallsReadyExactlyOnce(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	*f.Obs = draftConsumerPR(plan.Open, ciCheck("FAILURE"), "cc-1-tip")
	f.tick(t)
	if got := len(f.Forge.Calls("Ready")); got != 0 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 0", got)
	}

	// Blocker merges, but the consumer's own CI is still red.
	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("FAILURE"), "cc-1-tip")
	f.tick(t)
	if got := len(f.Forge.Calls("Ready")); got != 0 {
		t.Fatalf("pr ready calls after tick 2 = %d, want 0", got)
	}

	// Both halves now hold: this is the tick that must call `gh pr ready`.
	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
	f.tick(t)
	if got := len(f.Forge.Calls("Ready")); got != 1 {
		t.Fatalf("pr ready calls after tick 3 = %d, want exactly 1", got)
	}

	// GitHub now reports the PR as ready, as a real observe would from here on.
	f.Obs.PRs[branchRef("repo", "cc-1")] = plan.PR{
		Number: 1, State: plan.Open, IsDraft: false, HeadOid: "cc-1-tip", Checks: ciCheck("SUCCESS"),
	}
	for range 9 {
		f.tick(t)
	}
	if got := len(f.Forge.Calls("Ready")); got != 1 {
		t.Errorf("pr ready calls across ten ticks = %d, want exactly 1", got)
	}
}

func TestDraftGateClosedBlockerNeverReadies(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	*f.Obs = draftConsumerPR(plan.Closed, ciCheck("SUCCESS"), "cc-1-tip")
	for range 5 {
		f.tick(t)
	}
	if got := len(f.Forge.Calls("Ready")); got != 0 {
		t.Errorf("pr ready calls = %d, want 0: the gating blocker's PR closed unmerged", got)
	}

	server := openServer(f.Store, fixedClock(testAt), "")
	page := renderPage(t, server)
	row := webtest.RowFor(t, page, "sandbox://CC-1")
	if row.State == "base_gone" {
		t.Errorf("state = base_gone, reserved for a stacking parent, not a gating edge:\n%s", row.Row.HTML())
	}
}

func TestDraftGateReadyFailureIsRetriedNextTickWithoutAVerb(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	f.seedDraftGate(t)
	f.reset(testAt, runner.ProcessRunner{}, loop.WithForge(failingReady{f.Forge}))
	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
	f.tick(t)
	if got := len(f.Forge.Calls("Ready")); got != 1 {
		t.Fatalf("pr ready calls after tick 1 = %d, want 1 (the failed attempt)", got)
	}

	events := f.events(t, "draft_ready_failed")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL ||
		!strings.Contains(events[0].Detail, "fake gh: pr ready failed") {
		t.Errorf("events = %+v, want a draft_ready_failed event naming the failure", events)
	}

	f.reset(testAt, runner.ProcessRunner{})
	f.tick(t)
	if got := len(f.Forge.Calls("Ready")); got != 2 {
		t.Errorf("pr ready calls after tick 2 = %d, want 2 (retried automatically)", got)
	}

	events = f.events(t, "draft_ready")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL {
		t.Errorf("events = %+v, want a draft_ready event for the successful retry", events)
	}
}

func TestDraftGateReadyFailurePreservesGhStderr(t *testing.T) {
	f := newPushFixture(t)
	f.seedDraftGate(t)
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'fake gh: pr ready failed' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
	f.Loop = newLoop(f.Store, func(context.Context) (plan.Observation, error) { return *f.Obs, nil },
		fixedClock(testAt), f.cfg, f.ws, runner.ProcessRunner{})
	f.tick(t)
	events := f.events(t, "draft_ready_failed")
	if len(events) != 1 || events[0].TicketURL != f.Ticket.URL ||
		!strings.Contains(events[0].Detail, "fake gh: pr ready failed") {
		t.Errorf("events = %+v, want gh stderr in draft_ready_failed", events)
	}
}

func TestDraftPRCountsAsOpenForASameRepoDependent(t *testing.T) {
	t.Parallel()

	f := newPushFixture(t)
	store := f.Store
	tickets := []storepkg.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	storetest.Tickets(t, store, tickets...)

	obs := plan.Observation{
		Worktrees: map[plan.BranchRef]string{branchRef("repo", "parent"): "/repos/parent"},
		PRs:       map[plan.BranchRef]plan.PR{branchRef("repo", "parent"): {Number: 1, State: plan.Open, IsDraft: true}},
		Settings:  map[string]config.RepoSettings{"repo": {Stacking: true}},
	}
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	server := openServer(store, fixedClock(testAt), "")
	page := renderPage(t, server)

	child := webtest.RowFor(t, page, "sandbox://CHILD")
	if stack := child.Stack.Text(); stack != "L2 ← parent" {
		t.Errorf("child's stack = %q, want parent base (draft PR is open):\n%s", stack, child.Row.HTML())
	}
	if state := child.State; state != "ready" {
		t.Errorf("child's state = %q, want ready:\n%s", state, child.Row.HTML())
	}
}

func TestPushOneOpensADraftPRForATicketWithAGatingEdge(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	f.Ticket.BlockedBy = []string{"sandbox://PLA-40"}
	blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	storetest.Tickets(t, f.Store, f.Ticket, blocker)
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.tick(t)

	if got := f.Forge.Calls("Create"); len(got) != 1 || !got[0].Draft {
		t.Errorf("forge Create calls = %+v, want one draft PR for a ticket with a gating edge", got)
	}
}

func TestPushOneOpensANonDraftPRWithNoGatingEdge(t *testing.T) {
	t.Parallel()
	f := newPushFixture(t)
	gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	f.tick(t)

	if got := f.Forge.Calls("Create"); len(got) != 1 || got[0].Draft {
		t.Errorf("forge Create calls = %+v, want one non-draft PR for a plain ticket", got)
	}
}
