package spend_test

import (
	"math"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func TestFitRecoversAKnownFactorThroughContamination(t *testing.T) {
	t.Parallel()

	const trueFactor = 0.0025
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	var samples []spend.Interval
	start := now.Add(-6 * 24 * time.Hour)
	for i := 1; i <= 20; i++ {
		weight := float64(i) * 10
		end := start.Add(time.Hour)
		samples = append(samples, spend.Interval{
			Window: agentlog.FiveHour, Start: start, End: end,
			UtilizationStart: 0, UtilizationEnd: trueFactor * weight,
			WeightUSD: weight,
		})
		start = end
	}

	contaminated := []spend.Interval{
		{Window: agentlog.FiveHour, Start: start, End: start.Add(time.Hour), WeightUSD: 5, UtilizationEnd: 0.9},
		{Window: agentlog.FiveHour, Start: start, End: start.Add(time.Hour), WeightUSD: 400, UtilizationEnd: 0},
	}
	samples = append(samples, contaminated...)

	results := spend.Fit(samples, now)
	got, ok := results[agentlog.FiveHour]
	if !ok {
		t.Fatal("Fit returned no result for five_hour")
	}
	if diff := math.Abs(got.Factor-trueFactor) / trueFactor; diff > 0.05 {
		t.Errorf("Factor = %v, want within 5%% of %v (off by %.1f%%)", got.Factor, trueFactor, diff*100)
	}
}

func TestFitOmitsAWindowBelowTheTrailingSevenDays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	stale := spend.Interval{
		Window: agentlog.SevenDay, Start: now.Add(-30 * 24 * time.Hour), End: now.Add(-8 * 24 * time.Hour),
		WeightUSD: 100, UtilizationEnd: 0.5,
	}

	results := spend.Fit([]spend.Interval{stale}, now)
	if _, ok := results[agentlog.SevenDay]; ok {
		t.Errorf("Fit returned a result for a window with only stale samples: %+v", results)
	}
}
