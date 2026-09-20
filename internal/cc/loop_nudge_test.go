package cc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// waitForTicks polls count for up to two seconds, failing the test if it never reaches want.
// tickPeriod is fifteen seconds, so a bounded, sub-second wait is itself proof that whatever
// caused count to advance was not the loop's own ticker.
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

// TestLoopNudgeTicksImmediatelyAndCoalescesMidTick drives Run for real, over the real tickPeriod
// ticker, and proves both nudge behaviours ADR 14 specifies: a nudge wakes an immediate tick
// rather than waiting out tickPeriod, and several nudges arriving while a tick is already running
// coalesce into the one tick the buffered-by-one, non-blocking channel can carry rather than
// queuing one tick per nudge.
func TestLoopNudgeTicksImmediatelyAndCoalescesMidTick(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	var ticks atomic.Int32
	proceed := make(chan struct{})
	observe := func(context.Context) (cc.Observation, error) {
		n := ticks.Add(1)
		if n == 2 {
			<-proceed // hold tick 2 "in flight" so the nudges below land mid-tick.
		}
		return cc.Observation{}, nil
	}

	loop := cc.NewLoop(store, observe, time.Now, cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	waitForTicks(t, &ticks, 1) // Run's own first tick, before it ever waits.

	loop.Nudge()
	waitForTicks(t, &ticks, 2) // well under the 15s tickPeriod, or this times out.

	for range 5 {
		loop.Nudge() // every one of these lands while tick 2 sits blocked on proceed.
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
