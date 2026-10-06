package cc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func waitForTicks(t *testing.T, count *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count.Load() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("ticks = %d after 2s, want at least %d", count.Load(), want)
}

func TestLoopNudgeTicksImmediatelyAndCoalescesMidTick(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	var ticks atomic.Int32
	proceed := make(chan struct{})
	observe := func(context.Context) (plan.Observation, error) {
		n := ticks.Add(1)
		if n == 2 {
			<-proceed
		}
		return plan.Observation{}, nil
	}

	loop := cc.NewLoop(store, observe, cc.RealClock{}, cc.Config{}, cc.Workspace{}, runner.ProcessRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	waitForTicks(t, &ticks, 1)

	loop.Nudge()
	waitForTicks(t, &ticks, 2)

	for range 5 {
		loop.Nudge()
	}
	close(proceed)

	waitForTicks(t, &ticks, 3)
	time.Sleep(100 * time.Millisecond)
	if got := ticks.Load(); got != 3 {
		t.Fatalf("ticks = %d, want exactly 3: five mid-tick nudges must coalesce into one tick, not queue one each", got)
	}

	cancel()
	<-done
}

func TestLoopRunTicksOnTheInjectedClockWithoutSleeping(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	var ticks atomic.Int32
	observe := func(context.Context) (plan.Observation, error) {
		ticks.Add(1)
		return plan.Observation{}, nil
	}
	clock := newManualClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	loop := cc.NewLoop(store, observe, clock, cc.Config{}, cc.Workspace{}, runner.ProcessRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	waitForTicks(t, &ticks, 1)
	for want := int32(2); want <= 4; want++ {
		waitForWaiter(t, clock)
		clock.Advance(15 * time.Second)
		waitForTicks(t, &ticks, want)
	}

	cancel()
	<-done
}

func waitForWaiter(t *testing.T, clock *manualClock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clock.waiting() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("loop never waited on the clock")
}
