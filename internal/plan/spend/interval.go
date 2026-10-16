package spend

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

type Interval struct {
	Window                           agentlog.Window
	Start, End                       time.Time
	UtilizationStart, UtilizationEnd float64
	WeightUSD                        float64
}

type Weigher func(start, end time.Time) (float64, error)

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
				return nil, nil, fmt.Errorf("weigh %s interval: %w", reading.Window, err)
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
