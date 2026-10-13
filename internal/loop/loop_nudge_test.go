package loop_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func runLoop(ctx context.Context, t *testing.T, lp *loop.Loop) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- lp.Run(ctx) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run = %v, want nil on cancellation", err)
		}
	}
}

func TestLoopNudgeTicksImmediatelyAndCoalescesMidTick(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	synctest.Test(t, func(t *testing.T) {
		var ticks atomic.Int32
		proceed := make(chan struct{})
		observe := func(context.Context) (plan.Observation, error) {
			if ticks.Add(1) == 2 {
				<-proceed
			}
			return plan.Observation{}, nil
		}
		clock := newManualClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
		lp := loop.NewLoop(discardLog, store,observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		stop := runLoop(t.Context(), t, lp)

		synctest.Wait()
		if got := ticks.Load(); got != 1 {
			t.Fatalf("ticks after start = %d, want 1", got)
		}

		lp.Nudge()
		synctest.Wait()
		if got := ticks.Load(); got != 2 {
			t.Fatalf("ticks after a nudge = %d, want 2", got)
		}

		for range 5 {
			lp.Nudge()
		}
		close(proceed)
		synctest.Wait()
		if got := ticks.Load(); got != 3 {
			t.Errorf("ticks = %d, want exactly 3: five mid-tick nudges must coalesce into one tick, not queue one each", got)
		}

		stop()
	})
}

func TestLoopRunTicksOnTheInjectedClockWithoutSleeping(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	synctest.Test(t, func(t *testing.T) {
		var ticks atomic.Int32
		observe := func(context.Context) (plan.Observation, error) {
			ticks.Add(1)
			return plan.Observation{}, nil
		}
		clock := newManualClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
		lp := loop.NewLoop(discardLog, store,observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		stop := runLoop(t.Context(), t, lp)

		synctest.Wait()
		if got := ticks.Load(); got != 1 {
			t.Fatalf("ticks after start = %d, want 1", got)
		}
		for want := int32(2); want <= 4; want++ {
			clock.Advance(15 * time.Second)
			synctest.Wait()
			if got := ticks.Load(); got != want {
				t.Fatalf("ticks after advancing the clock = %d, want %d", got, want)
			}
		}

		stop()
	})
}
