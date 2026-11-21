package spend

import (
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

func Intervals(
	readings []agentlog.Reading, previous map[agentlog.Window]agentlog.Reading, requests []agentlog.RequestUsage,
) []Interval {
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
			intervals = append(intervals, Interval{
				Window: reading.Window, Start: prev.At, End: reading.At,
				UtilizationStart: prev.Utilization, UtilizationEnd: reading.Utilization,
				WeightUSD: SumWeight(requests, prev.At, reading.At),
			})
		}
		latest[reading.Window] = reading
	}
	return intervals
}
