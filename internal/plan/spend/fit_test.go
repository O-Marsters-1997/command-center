package spend_test

import (
	"math"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

func TestFit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	t.Run("recovers a known factor through contamination", func(t *testing.T) {
		t.Parallel()
		const trueFactor = 0.0025
		var samples []spend.Interval
		start := now.Add(-6 * 24 * time.Hour)
		for i := 1; i <= 20; i++ {
			weight := float64(i) * 10
			end := start.Add(time.Hour)
			samples = append(samples, spend.Interval{
				Window: agentlog.FiveHour, Start: start, End: end,
				UtilizationEnd: trueFactor * weight, WeightUSD: weight,
			})
			start = end
		}
		samples = append(samples,
			spend.Interval{
				Window: agentlog.FiveHour, Start: start, End: start.Add(time.Hour),
				WeightUSD: 5, UtilizationEnd: 0.9,
			},
			spend.Interval{Window: agentlog.FiveHour, Start: start, End: start.Add(time.Hour), WeightUSD: 400},
		)

		got, ok := spend.Fit(samples, now)[agentlog.FiveHour]
		if !ok {
			t.Fatal("Fit() has no five_hour result, want one")
		}
		if off := math.Abs(got.Factor-trueFactor) / trueFactor; off > 0.05 {
			t.Errorf("Fit() five_hour factor = %v, want within 5%% of %v", got.Factor, trueFactor)
		}
	})

	t.Run("omits a window with only samples older than seven days", func(t *testing.T) {
		t.Parallel()
		stale := spend.Interval{
			Window: agentlog.SevenDay, Start: now.Add(-30 * 24 * time.Hour), End: now.Add(-8 * 24 * time.Hour),
			WeightUSD: 100, UtilizationEnd: 0.5,
		}
		if got, ok := spend.Fit([]spend.Interval{stale}, now)[agentlog.SevenDay]; ok {
			t.Errorf("Fit(stale) seven_day = %+v, want no result", got)
		}
	})
}
