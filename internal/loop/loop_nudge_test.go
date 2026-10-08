package loop_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func waitUntil(t *testing.T, done func() bool, failure string) {
	t.Helper()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	deadline := time.After(2 * time.Second)
	for !done() {
		select {
		case <-poll.C:
		case <-deadline:
			t.Fatal(failure)
		}
	}
}

func holdsFor(window time.Duration, holds func() bool) bool {
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	end := time.After(window)
	for holds() {
		select {
		case <-poll.C:
		case <-end:
			return true
		}
	}
	return false
}

func waitForTicks(t *testing.T, count *atomic.Int32, want int32) {
	t.Helper()
	waitUntil(t, func() bool { return count.Load() >= want }, fmt.Sprintf("ticks never reached %d within 2s", want))
}

func waitForWaiters(t *testing.T, clock *manualClock, want int) {
	t.Helper()
	waitUntil(t, func() bool { return clock.waiting() >= want }, "loop never waited on the clock")
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

	clock := newManualClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	lp := loop.NewLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = lp.Run(ctx); close(done) }()

	waitForTicks(t, &ticks, 1)

	lp.Nudge()
	waitForTicks(t, &ticks, 2)

	for range 5 {
		lp.Nudge()
	}
	close(proceed)

	waitForTicks(t, &ticks, 3)
	waitForWaiters(t, clock, 3)
	if !holdsFor(100*time.Millisecond, func() bool { return ticks.Load() == 3 }) {
		t.Fatalf("ticks = %d, want exactly 3: five mid-tick nudges must coalesce into one tick, not queue one each",
			ticks.Load())
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
	lp := loop.NewLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = lp.Run(ctx); close(done) }()

	waitForTicks(t, &ticks, 1)
	for want := int32(2); want <= 4; want++ {
		waitForWaiters(t, clock, 1)
		clock.Advance(15 * time.Second)
		waitForTicks(t, &ticks, want)
	}

	cancel()
	<-done
}
