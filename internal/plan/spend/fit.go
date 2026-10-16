package spend

import (
	"math"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

const MinSamples = 5

const (
	trailingWindow = 7 * 24 * time.Hour
	outlierZ       = 2.5
)

type Result struct {
	Factor  float64
	Samples int
}

func Fit(samples []Interval, now time.Time) map[agentlog.Window]Result {
	cutoff := now.Add(-trailingWindow)
	byWindow := make(map[agentlog.Window][]Interval)
	for _, s := range samples {
		if s.End.Before(cutoff) {
			continue
		}
		byWindow[s.Window] = append(byWindow[s.Window], s)
	}

	results := make(map[agentlog.Window]Result, len(byWindow))
	for window, group := range byWindow {
		factor, ok := fitOne(group)
		if !ok {
			continue
		}
		results[window] = Result{Factor: factor, Samples: len(group)}
	}
	return results
}

func fitOne(samples []Interval) (float64, bool) {
	factor, ok := leastSquares(samples)
	if !ok {
		return 0, false
	}

	var sumSq float64
	for _, s := range samples {
		r := residual(s, factor)
		sumSq += r * r
	}
	stddev := math.Sqrt(sumSq / float64(len(samples)))
	if stddev == 0 {
		return factor, true
	}

	kept := make([]Interval, 0, len(samples))
	for _, s := range samples {
		if math.Abs(residual(s, factor)) <= outlierZ*stddev {
			kept = append(kept, s)
		}
	}
	if refit, ok := leastSquares(kept); ok {
		return refit, true
	}
	return factor, true
}

func residual(s Interval, factor float64) float64 {
	return (s.UtilizationEnd - s.UtilizationStart) - factor*s.WeightUSD
}

func leastSquares(samples []Interval) (float64, bool) {
	var num, den float64
	for _, s := range samples {
		x, y := s.WeightUSD, s.UtilizationEnd-s.UtilizationStart
		num += x * y
		den += x * x
	}
	if den == 0 {
		return 0, false
	}
	return num / den, true
}
