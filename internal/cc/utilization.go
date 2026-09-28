package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

// RecordReadings writes every reading once each, relying on utilization_readings' own (at,
// window) unique constraint to dedupe two overlapping runs that logged the same rate_limit_event.
func (s *Store) RecordReadings(ctx context.Context, readings []agentlog.Reading) error {
	for _, r := range readings {
		err := s.q.RecordReading(ctx, ccdb.RecordReadingParams{
			At: r.At.UTC(), Window: string(r.Window),
			Utilization: r.Utilization, ResetsAt: r.ResetsAt.UTC(),
		})
		if err != nil {
			return fmt.Errorf("record %s reading at %s: %w", r.Window, r.At, err)
		}
	}
	return nil
}

// Gauge is the masthead's own view of one window's latest utilization.
type Gauge struct {
	Utilization float64
	ResetsAt    time.Time
}

// LatestReadings returns the newest reading for every window that has one, keyed by window. A
// window with no reading yet is simply absent, never a zero-valued Gauge. It is LatestReadingsFull
// trimmed to the masthead's own Gauge shape, rather than a second query over the same rows.
func (s *Store) LatestReadings(ctx context.Context) (map[agentlog.Window]Gauge, error) {
	readings, err := s.LatestReadingsFull(ctx)
	if err != nil {
		return nil, err
	}
	gauges := make(map[agentlog.Window]Gauge, len(readings))
	for window, r := range readings {
		gauges[window] = Gauge{Utilization: r.Utilization, ResetsAt: r.ResetsAt}
	}
	return gauges, nil
}

func spendPaused(gauges map[agentlog.Window]Gauge, limit5h int) bool {
	return limit5h > 0 && pctOf(gauges[agentlog.FiveHour]) >= limit5h
}
