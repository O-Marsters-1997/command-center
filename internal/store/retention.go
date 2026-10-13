package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

const (
	consumedIntentRetention = 30 * 24 * time.Hour
	tickErrorRetention      = 30 * 24 * time.Hour
	readingRetention        = 90 * 24 * time.Hour
	intervalRetention       = 14 * 24 * time.Hour
	pruneBatch              = 1000
)

// PruneHistory deletes one batch per table of history past ADR 20's retention.
func (s *Store) PruneHistory(ctx context.Context, now time.Time) error {
	now = now.UTC()
	if _, err := s.q.PruneConsumedIntents(ctx, ccdb.PruneConsumedIntentsParams{
		Before: notNullTime(now.Add(-consumedIntentRetention)), Batch: pruneBatch,
	}); err != nil {
		return fmt.Errorf("prune consumed intents: %w", err)
	}
	if _, err := s.q.PruneTickErrors(ctx, ccdb.PruneTickErrorsParams{
		Before: now.Add(-tickErrorRetention), Batch: pruneBatch,
	}); err != nil {
		return fmt.Errorf("prune tick errors: %w", err)
	}
	if _, err := s.q.PruneReadings(ctx, ccdb.PruneReadingsParams{
		Before: now.Add(-readingRetention), Batch: pruneBatch,
	}); err != nil {
		return fmt.Errorf("prune readings: %w", err)
	}
	if _, err := s.q.PruneIntervals(ctx, ccdb.PruneIntervalsParams{
		Before: now.Add(-intervalRetention), Batch: pruneBatch,
	}); err != nil {
		return fmt.Errorf("prune intervals: %w", err)
	}
	return nil
}
