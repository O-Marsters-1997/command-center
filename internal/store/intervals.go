package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

const (
	fiveHourDuration = 5 * time.Hour
	sevenDayDuration = 7 * 24 * time.Hour
)

// LatestReadingsFull omits windows with no reading and retains each reading's timestamp.
func (s *Store) LatestReadingsFull(ctx context.Context) (map[agentlog.Window]agentlog.Reading, error) {
	rows, err := s.q.LatestReadingsFull(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest readings: %w", err)
	}
	readings := make(map[agentlog.Window]agentlog.Reading, len(rows))
	for _, row := range rows {
		readings[agentlog.Window(row.Window)] = agentlog.Reading{
			Window: agentlog.Window(row.Window), At: row.At,
			Utilization: row.Utilization, ResetsAt: row.ResetsAt,
		}
	}
	return readings, nil
}

// RecordReadingsAndIntervals weighs transcripts at reading time because the claude CLI prunes them later.
func (s *Store) RecordReadingsAndIntervals(ctx context.Context, readings []agentlog.Reading, projectsDir string) error {
	if len(readings) == 0 {
		return nil
	}
	requests, err := agentlog.LoadRequests(projectsDir)
	if err != nil {
		return fmt.Errorf("load transcripts under %s: %w", projectsDir, err)
	}
	previous, err := s.LatestReadingsFull(ctx)
	if err != nil {
		return fmt.Errorf("read previous readings: %w", err)
	}
	weigh := func(start, end time.Time) (float64, error) { return spend.SumWeight(requests, start, end), nil }
	intervals, _, err := spend.Intervals(readings, previous, weigh)
	if err != nil {
		return fmt.Errorf("build utilization intervals: %w", err)
	}
	if err := s.RecordReadings(ctx, readings); err != nil {
		return fmt.Errorf("record readings: %w", err)
	}
	rows := make([]intervalRow, len(intervals))
	for i, interval := range intervals {
		rows[i] = intervalRow{
			Window: string(interval.Window), StartAt: interval.Start.UTC(), EndAt: interval.End.UTC(),
			UtilizationStart: interval.UtilizationStart, UtilizationEnd: interval.UtilizationEnd,
			WeightUSD: interval.WeightUSD,
		}
	}
	encoded, err := encodeJSON("intervals", rows)
	if err != nil {
		return err
	}
	if err := s.q.RecordIntervals(ctx, encoded); err != nil {
		return fmt.Errorf("record %d intervals: %w", len(intervals), err)
	}
	return nil
}

type intervalRow struct {
	Window           string    `json:"window"`
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	UtilizationStart float64   `json:"utilization_start"`
	UtilizationEnd   float64   `json:"utilization_end"`
	WeightUSD        float64   `json:"weight_usd"`
}

// FitFactors omits windows below spend.MinSamples in the trailing seven days.
func (s *Store) FitFactors(ctx context.Context, now time.Time) (map[agentlog.Window]spend.Result, error) {
	rows, err := s.q.ListIntervalsSince(ctx, now.Add(-sevenDayDuration))
	if err != nil {
		return nil, fmt.Errorf("select intervals: %w", err)
	}
	samples := make([]spend.Interval, len(rows))
	for i, row := range rows {
		samples[i] = spend.Interval{
			Window: agentlog.Window(row.Window), Start: row.StartAt, End: row.EndAt,
			UtilizationStart: row.UtilizationStart, UtilizationEnd: row.UtilizationEnd,
			WeightUSD: row.WeightUsd,
		}
	}
	return spend.Fit(samples, now), nil
}

// CCCostUSD includes only cc's own runs within each window's trailing span.
func (s *Store) CCCostUSD(ctx context.Context, now time.Time) (map[agentlog.Window]float64, error) {
	row, err := s.q.CCCostSince(ctx, ccdb.CCCostSinceParams{
		FiveHourSince: notNullTime(now.Add(-fiveHourDuration)),
		SevenDaySince: notNullTime(now.Add(-sevenDayDuration)),
	})
	if err != nil {
		return nil, fmt.Errorf("select cc cost: %w", err)
	}
	return map[agentlog.Window]float64{
		agentlog.FiveHour: row.FiveHourUsd,
		agentlog.SevenDay: row.SevenDayUsd,
	}, nil
}
