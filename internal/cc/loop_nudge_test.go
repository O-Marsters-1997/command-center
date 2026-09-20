package cc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
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
	observe := func(context.Context) (cc.Observation, error) {
		n := ticks.Add(1)
		if n == 2 {
			<-proceed
		}
		return cc.Observation{}, nil
	}

	loop := cc.NewLoop(store, observe, time.Now, cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
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
