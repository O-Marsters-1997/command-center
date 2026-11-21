package loop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

type recordingNotifier struct{ titles []string }

func (n *recordingNotifier) Notify(_ context.Context, title, _ string) error {
	n.titles = append(n.titles, title)
	return nil
}

func (f *pushFixture) seedAutoCIFailed(t *testing.T, autoRounds int) {
	t.Helper()
	for range autoRounds {
		if _, _, err := f.Store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
			Ticket: f.Ticket.URL, Kind: plan.RunKindFollowUp, BaselineSHA: "older-tip", PromptHash: "h",
			Cause: plan.CauseCIFailed,
		}); err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
	}
	dispositionAsPushed(t, f.Store, f.Ticket.URL, testAt)
	storetest.Pushed(t, f.Store, storetest.PushedOpts{
		URL: f.Ticket.URL, Tip: "cc-1-tip", Base: "main", BaseTip: "main-tip", At: testAt,
	})
	checks := map[string]plan.CheckState{
		"CI": {Status: "COMPLETED", Conclusion: "FAILURE", DetailsURL: ghtest.FailedRunURL, Attempt: 2},
	}
	*f.Obs = draftConsumerPR(plan.Merged, checks, "cc-1-tip")
	f.Obs.Worktrees = map[plan.BranchRef]string{branchRef("repo", "cc-1"): f.Worktree}
}

func TestAutoFollowUp_CIFailed(t *testing.T) {
	t.Parallel()

	t.Run("starts one follow-up with the failed log and never a second for the same tip", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.Forge.Logs = map[string]string{ghtest.FailedRunURL: "boom\n"}
		f.seedAutoCIFailed(t, 0)
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)
		f.reset(testAt, fake)
		f.tick(t)

		spawns := fake.Spawns()
		if len(spawns) != 1 {
			t.Fatalf("spawns across two ticks and a restart = %d, want 1", len(spawns))
		}
		for _, want := range []string{"CI is failing on this branch", "## Failed CI log: CI", "boom"} {
			if !strings.Contains(spawns[0].Prompt, want) {
				t.Errorf("prompt = %q, want it to mention %q", spawns[0].Prompt, want)
			}
		}
		in, err := f.Store.PlanInput(t.Context())
		if err != nil {
			t.Fatalf("PlanInput: %v", err)
		}
		if got := in.Auto[plan.TicketURL(f.Ticket.URL)]; got.Rounds != 1 || got.LastTip == "" {
			t.Errorf("auto history = %+v, want one round with a tip", got)
		}
	})

	t.Run("notifies once on parking, across ticks and a restart", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedAutoCIFailed(t, plan.MaxAutoRounds)
		notes := &recordingNotifier{}
		f.reset(testAt, &runnertest.Runner{}, loop.WithNotifier(notes))
		f.tick(t)
		f.tick(t)
		f.reset(testAt, &runnertest.Runner{}, loop.WithNotifier(notes))
		f.tick(t)

		if len(notes.titles) != 1 {
			t.Errorf("notifications = %v, want exactly one", notes.titles)
		}
		if got := len(f.events(t, storepkg.EventParked)); got != 1 {
			t.Errorf("parked events = %d, want 1", got)
		}
	})

	t.Run("starts nothing once the rounds are spent", func(t *testing.T) {
		t.Parallel()
		f := newPushFixture(t)
		f.seedAutoCIFailed(t, plan.MaxAutoRounds)
		fake := &runnertest.Runner{}
		f.reset(testAt, fake)
		f.tick(t)

		if got := len(fake.Spawns()); got != 0 {
			t.Errorf("spawns = %d, want 0", got)
		}
	})
}
