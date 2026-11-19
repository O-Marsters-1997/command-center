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

	tests := []struct {
		name       string
		observe    loop.ObserveFunc
		wantErr    bool
		wantCounts string
	}{
		{
			name: "a tick that succeeds",
			observe: func(context.Context) (plan.Observation, error) {
				obs := plan.NewObservation(testAt)
				obs.Settings["owner/a"] = plan.RepoSettings{}
				obs.RepoErrors["owner/a"] = "carried forward"
				obs.Runs["sandbox://CC-1"] = plan.RunObservation{Alive: true}
				return obs, nil
			},
			wantCounts: "repos=1 live_command_runs=1",
		},
		{
			name:       "a tick that fails",
			observe:    func(context.Context) (plan.Observation, error) { return plan.Observation{}, errors.New("boom") },
			wantErr:    true,
			wantCounts: "repos=0 live_command_runs=0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			lp := newLoopWithDeps(loop.Deps{
				Logger: slog.New(slog.NewTextHandler(&logs, nil)), Store: openStore(t), Observe: tt.observe,
				Clock: fixedClock(testAt), Config: config.Config{}, Workspace: config.Workspace{},
				Runner: runner.ProcessRunner{},
			})

			if err := lp.RunOnce(t.Context()); (err != nil) != tt.wantErr {
				t.Fatalf("RunOnce error = %v, wantErr %t", err, tt.wantErr)
			}

			got := logs.String()
			if n := strings.Count(got, `msg="tick timing"`); n != 1 {
				t.Fatalf("tick timing lines = %d, want 1; log:\n%s", n, got)
			}
			fields := []string{"validate=", "observe=", "absorb=", "refresh=", "push=", "cut=", "removal=", tt.wantCounts}
			for _, field := range fields {
				if !strings.Contains(got, field) {
					t.Errorf("tick timing line lacks %q; log:\n%s", field, got)
				}
			}
		})
	}
}
