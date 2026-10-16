package loop_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func (f *pushFixture) seedDraftGate(t *testing.T) {
	t.Helper()
	f.Ticket.BlockedBy = []string{"sandbox://PLA-40"}
	blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	storetest.Tickets(t, f.Store, f.Ticket, blocker)
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	storetest.Pushed(t, f.Store, storetest.PushedOpts{
		URL: f.Ticket.URL, Tip: "cc-1-tip", Base: "main", BaseTip: "main-tip", At: testAt,
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

func TestDraftGate(t *testing.T) {
	t.Parallel()

	t.Run("readies the PR exactly once across ticks", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedDraftGate(t)
		*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
		f.tick(t)
		f.Obs.PRs[branchRef("repo", "cc-1")] = plan.PR{
			Number: 1, State: plan.Open, HeadOid: "cc-1-tip", Checks: ciCheck("SUCCESS"),
		}
		for range 9 {
			f.tick(t)
		}

		if got := len(f.Forge.Calls("Ready")); got != 1 {
			t.Errorf("Ready calls across ten ticks = %d, want 1", got)
		}
	})

	t.Run("a failed Ready is retried next tick without a verb", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedDraftGate(t)
		f.reset(testAt, runner.ProcessRunner{}, loop.WithForge(failingReady{f.Forge}))
		*f.Obs = draftConsumerPR(plan.Merged, ciCheck("SUCCESS"), "cc-1-tip")
		f.tick(t)

		failed := f.events(t, "draft_ready_failed")
		if len(failed) != 1 || failed[0].TicketURL != f.Ticket.URL ||
			!strings.Contains(failed[0].Detail, fmt.Sprint(errForge)) {
			t.Errorf("draft_ready_failed events = %+v, want one naming %q", failed, errForge)
		}

		f.reset(testAt, runner.ProcessRunner{})
		f.tick(t)

		if got := len(f.Forge.Calls("Ready")); got != 2 {
			t.Errorf("Ready calls after the retry = %d, want 2", got)
		}
		if ready := f.events(t, "draft_ready"); len(ready) != 1 || ready[0].TicketURL != f.Ticket.URL {
			t.Errorf("draft_ready events = %+v, want one for the retry", ready)
		}
	})

	t.Run("push opens a draft PR for a ticket with a gating edge", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		gittest.Commit(t, f.Worktree, "agent.txt", "agent was here\n")
		f.Ticket.BlockedBy = []string{"sandbox://PLA-40"}
		blocker := storepkg.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
		storetest.Tickets(t, f.Store, f.Ticket, blocker)
		dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
		f.tick(t)

		if got := f.Forge.Calls("Create"); len(got) != 1 || !got[0].Draft {
			t.Errorf("Create calls = %+v, want one draft", got)
		}
	})
}
