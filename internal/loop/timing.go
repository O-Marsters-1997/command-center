package loop

import (
	"context"
	"log/slog"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type tickTiming struct {
	Validate, Observe, Absorb, Refresh, Push, Cut, Removal time.Duration
	Repos                                                  int
	LiveCommandRuns                                        int
}

func observedRepos(obs plan.Observation) int {
	names := map[string]bool{}
	for _, m := range []map[string]string{obs.SettingsErrors, obs.RepoErrors} {
		for name := range m {
			names[name] = true
		}
	}
	for name := range obs.Settings {
		names[name] = true
	}
	return len(names)
}

func (tt tickTiming) log(ctx context.Context, logger *slog.Logger) {
	logger.InfoContext(ctx, "tick timing",
		"validate", tt.Validate, "observe", tt.Observe, "absorb", tt.Absorb,
		"refresh", tt.Refresh, "push", tt.Push, "cut", tt.Cut, "removal", tt.Removal,
		"repos", tt.Repos, "live_command_runs", tt.LiveCommandRuns)
}

func (l *Loop) timed(d *time.Duration, f func() error) error {
	start := l.clock.Now()
	defer func() { *d = l.clock.Now().Sub(start) }()
	return f()
}
