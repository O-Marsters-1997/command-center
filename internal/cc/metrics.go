package cc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/usage"
)

// MetricsParser reads one run's own log into its settled totals — agentlog.ParseMetrics's own
// signature, injected so a test can substitute a fake without touching the filesystem.
type MetricsParser func(logPath string) (agentlog.RunMetrics, error)

// BackfillMetrics runs once at startup over every run RunsAwaitingMetricsBackfill still returns:
// every surviving log gets its metrics, readings and any intervals they close, all from the same
// bytes. Idempotent on that predicate for metrics, and on utilization_readings' own (at, window)
// constraint for readings, so a restart mid-backfill resumes rather than reparses
// (docs/adr/0015-run-metrics-are-captured-at-disposition-from-stdout.md).
func BackfillMetrics(ctx context.Context, store *Store, parser MetricsParser, claudeProjectsDir string) error {
	runs, err := store.RunsAwaitingMetricsBackfill(ctx)
	if err != nil {
		return err
	}
	// Loaded once for the whole backfill pass, not once per run, since every run's interval close
	// weighs the same transcripts directory.
	requests, err := usage.LoadRequests(claudeProjectsDir)
	if err != nil {
		return fmt.Errorf("load transcripts under %s: %w", claudeProjectsDir, err)
	}
	for _, run := range runs {
		metrics, err := parser(run.LogPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("backfill run %d metrics %s: %v", run.ID, run.LogPath, err)
			}
		} else if err := store.BackfillRunMetrics(ctx, run.ID, metrics); err != nil {
			return err
		}

		readings, err := agentlog.ParseReadings(run.LogPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("backfill run %d readings %s: %v", run.ID, run.LogPath, err)
			}
			continue
		}
		if err := store.recordReadingsAndIntervals(ctx, readings, requests); err != nil {
			return err
		}
	}
	return nil
}
