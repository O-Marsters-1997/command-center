// Package spend fits the subscription's utilization windows to what was actually spent: Intervals
// samples between consecutive readings, and Fit finds each window's dollars-per-utilization
// factor through the origin.
package spend

import (
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

type Interval struct {
	Window                           agentlog.Window
	Start, End                       time.Time
	UtilizationStart, UtilizationEnd float64
	WeightUSD                        float64
}

type Weigher func(start, end time.Time) (float64, error)

// Intervals pairs each reading with the previous one for its window, and returns the latest
// reading per window for the caller to persist as the next call's previous.
func Intervals(
	readings []agentlog.Reading, previous map[agentlog.Window]agentlog.Reading, weigh Weigher,
) ([]Interval, map[agentlog.Window]agentlog.Reading, error) {
	sorted := slices.Clone(readings)
	slices.SortFunc(sorted, func(a, b agentlog.Reading) int { return a.At.Compare(b.At) })

	latest := make(map[agentlog.Window]agentlog.Reading, len(previous))
	maps.Copy(latest, previous)

	var intervals []Interval
	for _, reading := range sorted {
		prev, ok := latest[reading.Window]
		if ok && !reading.At.After(prev.At) {
			continue
		}
		if ok && reading.Utilization >= prev.Utilization {
			weight, err := weigh(prev.At, reading.At)
			if err != nil {
				return nil, nil, err
			}
			intervals = append(intervals, Interval{
				Window: reading.Window, Start: prev.At, End: reading.At,
				UtilizationStart: prev.Utilization, UtilizationEnd: reading.Utilization,
				WeightUSD: weight,
			})
		}
		latest[reading.Window] = reading
	}
	return intervals, latest, nil
}
