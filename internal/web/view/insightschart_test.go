package view

import (
	"math"
	"slices"
	"testing"
)

func TestBuildInsightsChartHasNothingToDrawWithoutPoints(t *testing.T) {
	t.Parallel()

	if got := buildInsightsChart(InsightsResponse{WastePctWeek: 3}); len(got.Dots) != 0 {
		t.Errorf("dots = %d, want an empty chart", len(got.Dots))
	}
}

func TestBuildInsightsChartScalesPointsAndWasteOntoTheTicks(t *testing.T) {
	t.Parallel()

	chart := buildInsightsChart(InsightsResponse{
		Points: []insightsPointJSON{
			{Ticket: "A", PctWeek: 9, MergedAt: "m1"},
			{Ticket: "B", Title: "Bee", PctWeek: 2, MergedAt: "m2"},
		},
		WastePctWeek: 4,
	})

	var labels []string
	for _, tick := range chart.Ticks {
		labels = append(labels, tick.Label)
	}
	if want := []string{"0%", "5%", "10%"}; !slices.Equal(labels, want) {
		t.Errorf("tick labels = %v, want %v", labels, want)
	}
	if top := chart.Ticks[2].Y; top != insightsChartPadTop {
		t.Errorf("top tick y = %v, want %v", top, insightsChartPadTop)
	}
	if !chart.HasWaste || chart.WasteLabel != "4.00% week" {
		t.Errorf("waste = %v %q", chart.HasWaste, chart.WasteLabel)
	}
	if chart.Dots[1].Title != "Bee: 2.00% week (agent 0.00, resolve 0.00, follow-up 0.00), merged m2" {
		t.Errorf("title = %q", chart.Dots[1].Title)
	}
	if chart.Dots[0].Y >= chart.Dots[1].Y {
		t.Errorf("a 9%% point must sit above a 2%% point: %v vs %v", chart.Dots[0].Y, chart.Dots[1].Y)
	}
}

func TestRollingMedianTrailsAWindow(t *testing.T) {
	t.Parallel()

	got := rollingMedian([]float64{1, 5, 2, 8}, 4)
	want := []float64{1, 3, 2, 3.5}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("rollingMedian = %v, want %v", got, want)
		}
	}
	if got := rollingMedian([]float64{1, 2, 3, 4, 5}, 3); got[4] != 4 {
		t.Errorf("windowed median = %v, want last 4", got)
	}
}

func TestNiceTicksPicksA125LadderStep(t *testing.T) {
	t.Parallel()

	if got, want := niceTicks(9, 4), []float64{0, 5, 10}; !slices.Equal(got, want) {
		t.Errorf("niceTicks(9) = %v, want %v", got, want)
	}
	if got := niceTicks(0, 4); !slices.Equal(got, []float64{0}) {
		t.Errorf("niceTicks(0) = %v, want [0]", got)
	}
}
