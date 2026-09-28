package usage

import (
	"math"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// MinSamples is the fewest trailing samples a window needs before its factor is trusted; below
// it, the gauge reads "calibrating" rather than a split nobody should act on yet.
const MinSamples = 5

const (
	trailingWindow = 7 * 24 * time.Hour
	outlierZ       = 2.5
)

// Result is one window's fit: the least-squares factor and how many trailing samples fed it.
type Result struct {
	Factor  float64
	Samples int
}

// Fit finds each window's least-squares-through-the-origin factor relating a sample's dollar
// weight to its utilization rise, over samples ending within the trailing seven days of now, with
// a second pass dropping any sample whose residual sits more than outlierZ standard deviations out.
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

// leastSquares fits factor for y = factor*x through the origin: factor = sum(x*y)/sum(x*x).
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
