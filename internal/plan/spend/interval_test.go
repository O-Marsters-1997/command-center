package spend_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

func TestIntervals(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reading := func(w agentlog.Window, utilization float64, at time.Time) agentlog.Reading {
		return agentlog.Reading{Window: w, Utilization: utilization, At: at}
	}
	requests := []agentlog.RequestUsage{
		{At: at.Add(30 * time.Minute), Model: "claude-sonnet-5", Input: 1, CostUSD: option.Some(8.0)},
	}
	tests := []struct {
		name          string
		readings      []agentlog.Reading
		previous      map[agentlog.Window]agentlog.Reading
		wantIntervals []spend.Interval
	}{
		{
			name:     "pairs a reading with the previous one for its window",
			readings: []agentlog.Reading{reading(agentlog.FiveHour, 0.30, at.Add(time.Hour))},
			previous: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.10, at)},
			wantIntervals: []spend.Interval{{
				Window: agentlog.FiveHour, Start: at, End: at.Add(time.Hour),
				UtilizationStart: 0.10, UtilizationEnd: 0.30, WeightUSD: 8,
			}},
		},
		{
			name:     "no previous reading closes nothing yet",
			readings: []agentlog.Reading{reading(agentlog.SevenDay, 0.10, at)},
		},
		{
			name:     "an older reading is ignored",
			readings: []agentlog.Reading{reading(agentlog.FiveHour, 0.15, at.Add(-time.Minute))},
			previous: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.20, at)},
		},
		{
			name:     "a pair straddling a reset is skipped",
			readings: []agentlog.Reading{reading(agentlog.FiveHour, 0.10, at.Add(time.Hour))},
			previous: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.95, at)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := spend.Intervals(tt.readings, tt.previous, requests)
			if diff := cmp.Diff(tt.wantIntervals, got); diff != "" {
				t.Errorf("Intervals() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
