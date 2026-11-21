package loop_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestKill(t *testing.T) {
	t.Parallel()

	killLiveRun := func(t *testing.T, v storepkg.VerbRequest) (*runFixture, *runnertest.Runner) {
		t.Helper()
		f := newRunFixture(t)
		f.spawn(t, 4242, f.baseline, "/state/runs/1.jsonl")
		v.Ticket = f.ticket.URL
		v.Verb = "kill"
		storetest.Verb(t, f.store, v)
		fake := &runnertest.Runner{}
		fake.Run(4242)
		f.tick(t, f.worktreeObs(), fake)
		return f, fake
	}
	pendingKills := func(t *testing.T, f *runFixture) []storepkg.VerbIntent {
		t.Helper()
		pending, err := f.store.PendingVerbIntents(t.Context(), "kill")
		if err != nil {
			t.Fatalf("PendingVerbIntents(kill): %v", err)
		}
		return pending
	}

	t.Run("cancels a live run and disposes it in the same tick", func(t *testing.T) {
		t.Parallel()
		f, fake := killLiveRun(t, storepkg.VerbRequest{At: testAt.Add(time.Second)})

		if diff := cmp.Diff([]int{4242}, fake.Canceled()); diff != "" {
			t.Errorf("canceled mismatch (-want +got):\n%s", diff)
		}
		if pending := pendingKills(t, f); len(pending) != 0 {
			t.Errorf("PendingVerbIntents(kill) = %+v, want none", pending)
		}
		if got := f.latest(t); got.Outcome.IsNone() || got.ExitCode != option.Some(143) {
			t.Errorf("killed run = outcome %v, exit %v, want disposed with exit 143 in the same tick", got.Outcome, got.ExitCode)
		}
	})

	t.Run("an intent still held is left alone", func(t *testing.T) {
		t.Parallel()
		f, fake := killLiveRun(t, storepkg.VerbRequest{At: testAt, HoldUntil: testAt.Add(time.Minute)})

		if got := fake.Canceled(); len(got) != 0 {
			t.Errorf("canceled = %v, want none", got)
		}
		if pending := pendingKills(t, f); len(pending) != 1 {
			t.Errorf("PendingVerbIntents(kill) = %+v, want 1", pending)
		}
	})

	t.Run("an intent whose hold has passed is consumed", func(t *testing.T) {
		t.Parallel()
		f, fake := killLiveRun(t, storepkg.VerbRequest{At: testAt, HoldUntil: testAt.Add(10 * time.Second)})

		if diff := cmp.Diff([]int{4242}, fake.Canceled()); diff != "" {
			t.Errorf("canceled mismatch (-want +got):\n%s", diff)
		}
		if pending := pendingKills(t, f); len(pending) != 0 {
			t.Errorf("PendingVerbIntents(kill) = %+v, want none", pending)
		}
	})
}

func TestKillOverdue(t *testing.T) {
	t.Parallel()

	f := newLoopFixture(t)
	url := f.Tickets[0].URL
	launch := storepkg.LaunchRequest{Ticket: url, PromptHash: composeHash(url), Group: "g", At: testAt, Workflow: "v1"}
	if err := f.Store.QueueLaunchIntent(t.Context(), launch); err != nil {
		t.Fatalf("QueueLaunchIntent(%q): %v", url, err)
	}
	obs := plan.Observation{
		Settings:  map[string]plan.RepoSettings{"repo": {VerifyCommand: []string{"just", "test"}}},
		Worktrees: map[plan.BranchRef]string{},
	}
	clock := newManualClock(testAt)
	lp := newLoop(f.Store, observing(obs), clock, f.Cfg, f.WS, f.Fake, loop.WithForge(f.Forge))

	runOnce(t, lp)
	if got := f.Latest(t)[url].Kind; got != plan.RunKindCommand {
		t.Fatalf("first run kind = %q, want %q", got, plan.RunKindCommand)
	}

	clock.Advance(plan.CommandDeadline)
	runOnce(t, lp)
	if got := f.Fake.Canceled(); len(got) != 0 {
		t.Errorf("canceled at the deadline = %v, want none", got)
	}

	clock.Advance(time.Second)
	runOnce(t, lp)
	if got := len(f.Fake.Canceled()); got != 1 {
		t.Errorf("canceled past the deadline = %d runs, want 1", got)
	}
	if got := f.Latest(t)[url].Outcome; got != option.Some(plan.OutcomeFailed) {
		t.Errorf("killed run outcome = %v, want failed in the same tick", got)
	}
}
