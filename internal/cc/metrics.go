package cc

import (
	"context"
	"errors"
	"io/fs"
	"log"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// MetricsParser reads one run's own log into its settled totals — agentlog.ParseMetrics's own
// signature, injected so a test can substitute a fake without touching the filesystem.
type MetricsParser func(logPath string) (agentlog.RunMetrics, error)

// BackfillMetrics runs once at startup over every run RunsAwaitingMetricsBackfill still returns.
// Idempotent on that predicate, so a restart mid-backfill resumes rather than reparses
// (docs/adr/0015-run-metrics-are-captured-at-disposition-from-stdout.md).
func BackfillMetrics(ctx context.Context, store *Store, parser MetricsParser) error {
	runs, err := store.RunsAwaitingMetricsBackfill(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		metrics, err := parser(run.LogPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("backfill run %d metrics %s: %v", run.ID, run.LogPath, err)
			}
			continue
		}
		if err := store.BackfillRunMetrics(ctx, run.ID, metrics); err != nil {
			return err
		}
	}
	return nil
}
