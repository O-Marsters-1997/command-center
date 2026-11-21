package loop_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestRunOnceLogsOneTimingLine(t *testing.T) {
	t.Parallel()

	runTick := func(t *testing.T, observe loop.ObserveFunc) (logs string, err error) {
		t.Helper()
		var buf bytes.Buffer
		lp := newLoopWithDeps(loop.Deps{
			Logger: slog.New(slog.NewTextHandler(&buf, nil)), Store: openStore(t), Observe: observe,
			Clock: fixedClock(testAt), Config: config.Config{}, Workspace: config.Workspace{},
			Runner: runner.ProcessRunner{},
		})
		err = lp.RunOnce(t.Context())
		return buf.String(), err
	}
	checkTimingLine := func(t *testing.T, got, wantCounts string) {
		t.Helper()
		if n := strings.Count(got, `msg="tick timing"`); n != 1 {
			t.Fatalf("tick timing lines = %d, want 1; log:\n%s", n, got)
		}
		fields := []string{"validate=", "observe=", "absorb=", "refresh=", "push=", "cut=", "removal=", wantCounts}
		for _, field := range fields {
			if !strings.Contains(got, field) {
				t.Errorf("tick timing line lacks %q; log:\n%s", field, got)
			}
		}
	}

	t.Run("a tick that succeeds", func(t *testing.T) {
		t.Parallel()
		got, err := runTick(t, func(context.Context) (plan.Observation, error) {
			obs := plan.NewObservation(testAt)
			obs.Settings["owner/a"] = plan.RepoSettings{}
			obs.RepoErrors["owner/a"] = "carried forward"
			obs.Runs["sandbox://CC-1"] = plan.RunObservation{Alive: true}
			return obs, nil
		})
		if err != nil {
			t.Fatalf("RunOnce error = %v", err)
		}
		checkTimingLine(t, got, "repos=1 live_command_runs=1")
	})

	t.Run("a tick that fails", func(t *testing.T) {
		t.Parallel()
		got, err := runTick(t, func(context.Context) (plan.Observation, error) {
			return plan.Observation{}, errors.New("boom")
		})
		if err == nil {
			t.Fatal("RunOnce error = nil, want the observe failure")
		}
		checkTimingLine(t, got, "repos=0 live_command_runs=0")
	})
}
