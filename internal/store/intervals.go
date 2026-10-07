package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

const (
	fiveHourDuration = 5 * time.Hour
	sevenDayDuration = 7 * 24 * time.Hour
)

// LatestReadingsFull returns the newest reading for every window that has one, keyed by
// window, with each reading's own At.
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

// RecordReadingsAndIntervals writes readings, then closes the interval each completes
// against the window's previous stored reading, weighing transcripts under projectsDir. Weighing
// happens at reading time because the claude CLI prunes transcripts later.
func (s *Store) RecordReadingsAndIntervals(ctx context.Context, readings []agentlog.Reading, projectsDir string) error {
	if len(readings) == 0 {
		return nil
	}
	requests, err := agentlog.LoadRequests(projectsDir)
	if err != nil {
		return fmt.Errorf("load transcripts under %s: %w", projectsDir, err)
	}
	return s.RecordReadingsAndIntervalsFrom(ctx, readings, requests)
}

// RecordReadingsAndIntervalsFrom is RecordReadingsAndIntervals over transcripts already loaded.
func (s *Store) RecordReadingsAndIntervalsFrom(
	ctx context.Context, readings []agentlog.Reading, requests []agentlog.RequestUsage,
) error {
	if len(readings) == 0 {
		return nil
	}
	previous, err := s.LatestReadingsFull(ctx)
	if err != nil {
		return err
	}
	weigh := func(start, end time.Time) (float64, error) { return spend.SumWeight(requests, start, end), nil }
	intervals, _, err := spend.Intervals(readings, previous, weigh)
	if err != nil {
		return fmt.Errorf("build utilization intervals: %w", err)
	}
	if err := s.RecordReadings(ctx, readings); err != nil {
		return err
	}
	for _, interval := range intervals {
		err := s.q.RecordInterval(ctx, ccdb.RecordIntervalParams{
			Window: string(interval.Window), StartAt: interval.Start.UTC(), EndAt: interval.End.UTC(),
			UtilizationStart: interval.UtilizationStart, UtilizationEnd: interval.UtilizationEnd,
			WeightUsd: interval.WeightUSD,
		})
		if err != nil {
			return fmt.Errorf("record interval %s %s-%s: %w", interval.Window, interval.Start, interval.End, err)
		}
	}
	return nil
}

// FitFactors returns each window's least-squares dollars-to-utilization factor over the
// trailing seven days. A window below spend.MinSamples is absent.
func (s *Store) FitFactors(ctx context.Context, now time.Time) (map[agentlog.Window]spend.Result, error) {
	rows, err := s.q.IntervalsSince(ctx, now.Add(-sevenDayDuration))
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

// CCCostUSD returns cc's own runs' cost_usd within each window's trailing span as of now.
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
