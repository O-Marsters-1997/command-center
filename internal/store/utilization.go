package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

// RecordReadings deduplicates readings from overlapping runs by time and window.
func (s *Store) RecordReadings(ctx context.Context, readings []agentlog.Reading) error {
	rows := make([]readingRow, len(readings))
	for i, r := range readings {
		rows[i] = readingRow{At: r.At.UTC(), Window: string(r.Window), Utilization: r.Utilization, ResetsAt: r.ResetsAt.UTC()}
	}
	encoded, err := encodeJSON("readings", rows)
	if err != nil {
		return err
	}
	if err := s.q.RecordReadings(ctx, encoded); err != nil {
		return fmt.Errorf("record %d readings: %w", len(readings), err)
	}
	return nil
}

type readingRow struct {
	At          time.Time `json:"at"`
	Window      string    `json:"window"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// Gauge holds the latest utilization and reset time for one window.
type Gauge struct {
	Utilization float64
	ResetsAt    time.Time
}

// LatestReadings omits windows with no reading.
func (s *Store) LatestReadings(ctx context.Context) (map[agentlog.Window]Gauge, error) {
	readings, err := s.LatestReadingsFull(ctx)
	if err != nil {
		return nil, fmt.Errorf("read latest readings: %w", err)
	}
	gauges := make(map[agentlog.Window]Gauge, len(readings))
	for window, r := range readings {
		gauges[window] = Gauge{Utilization: r.Utilization, ResetsAt: r.ResetsAt}
	}
	return gauges, nil
}
