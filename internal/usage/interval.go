// Package usage fits the subscription's utilization windows to what was actually spent: Intervals
// samples between consecutive readings, and Fit finds each window's dollars-per-utilization
// factor through the origin (CC-313).
package usage

import (
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// Interval is one window's utilization rise between two consecutive readings, and every dollar
// spent, across every transcript, in that same span.
type Interval struct {
	Window                           agentlog.Window
	Start, End                       time.Time
	UtilizationStart, UtilizationEnd float64
	WeightUSD                        float64
}

// Weigher sums the dollar cost of every request across every transcript in [start, end).
type Weigher func(start, end time.Time) (float64, error)

// Intervals pairs each reading with the previous one recorded for its window, closing one
// Interval per pair and weighing every transcript in that span, and returns the latest reading
// per window alongside them for the caller to persist as the next call's previous.
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
		// Utilization only ever falls at the window's own reset, so a fallen reading means the
		// reset landed between the two: the true rise is unknowable, so the pair is skipped.
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
