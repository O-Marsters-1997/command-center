package loop_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/web"
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
		var closeProceed sync.Once
		t.Cleanup(func() { closeProceed.Do(func() { close(proceed) }) })
		observe := func(context.Context) (plan.Observation, error) {
			if ticks.Add(1) == 2 {
				<-proceed
			}
			return plan.Observation{}, nil
		}
		clock := newManualClock(testAt)
		lp := newLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		nudger := lp.Nudger()
		stop := runLoop(t.Context(), t, lp)

		synctest.Wait()
		if got := ticks.Load(); got != 1 {
			t.Fatalf("ticks after start = %d, want 1", got)
		}

		nudger.Nudge()
		synctest.Wait()
		if got := ticks.Load(); got != 2 {
			t.Fatalf("ticks after a nudge = %d, want 2", got)
		}

		for range 5 {
			nudger.Nudge()
		}
		closeProceed.Do(func() { close(proceed) })
		synctest.Wait()
		if got := ticks.Load(); got != 3 {
			t.Errorf("ticks = %d, want exactly 3: five mid-tick nudges must coalesce into one tick, not queue one each", got)
		}

		stop()
	})
}

func TestAVerbHandlersNudgeReachesTheLoopThroughItsNudger(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	ticks := make(chan struct{}, 8)
	observe := func(context.Context) (plan.Observation, error) {
		ticks <- struct{}{}
		return plan.Observation{}, nil
	}
	waitForTick := func(what string) {
		t.Helper()
		select {
		case <-ticks:
		case <-time.After(10 * time.Second):
			t.Fatalf("no tick %s", what)
		}
	}
	clock := newManualClock(testAt)
	lp := newLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
	server := web.NewServer(web.Deps{Logger: discardLog, Store: store, Clock: clock, Nudger: lp.Nudger()})
	stop := runLoop(t.Context(), t, lp)
	t.Cleanup(stop)

	waitForTick("after start")

	req := httptest.NewRequest(http.MethodPost, "/features/"+url.PathEscape("project:x")+"/import", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	waitForTick("after the handler queued an import")
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
		clock := newManualClock(testAt)
		lp := newLoop(store, observe, clock, config.Config{}, config.Workspace{}, runner.ProcessRunner{})
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
