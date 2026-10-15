package spend_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func eightPerHour(start, end time.Time) (float64, error) { return end.Sub(start).Hours() * 8, nil }

func TestIntervals(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reading := func(w agentlog.Window, utilization float64, at time.Time) agentlog.Reading {
		return agentlog.Reading{Window: w, Utilization: utilization, At: at}
	}
	tests := []struct {
		name          string
		readings      []agentlog.Reading
		previous      map[agentlog.Window]agentlog.Reading
		wantIntervals []spend.Interval
		wantLatest    map[agentlog.Window]agentlog.Reading
	}{
		{
			name:     "pairs a reading with the previous one for its window",
			readings: []agentlog.Reading{reading(agentlog.FiveHour, 0.30, at.Add(time.Hour))},
			previous: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.10, at)},
			wantIntervals: []spend.Interval{{
				Window: agentlog.FiveHour, Start: at, End: at.Add(time.Hour),
				UtilizationStart: 0.10, UtilizationEnd: 0.30, WeightUSD: 8,
			}},
			wantLatest: map[agentlog.Window]agentlog.Reading{
				agentlog.FiveHour: reading(agentlog.FiveHour, 0.30, at.Add(time.Hour)),
			},
		},
		{
			name:       "no previous reading closes nothing yet",
			readings:   []agentlog.Reading{reading(agentlog.SevenDay, 0.10, at)},
			wantLatest: map[agentlog.Window]agentlog.Reading{agentlog.SevenDay: reading(agentlog.SevenDay, 0.10, at)},
		},
		{
			name:       "an older reading is ignored",
			readings:   []agentlog.Reading{reading(agentlog.FiveHour, 0.15, at.Add(-time.Minute))},
			previous:   map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.20, at)},
			wantLatest: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.20, at)},
		},
		{
			name:     "a pair straddling a reset is skipped",
			readings: []agentlog.Reading{reading(agentlog.FiveHour, 0.10, at.Add(time.Hour))},
			previous: map[agentlog.Window]agentlog.Reading{agentlog.FiveHour: reading(agentlog.FiveHour, 0.95, at)},
			wantLatest: map[agentlog.Window]agentlog.Reading{
				agentlog.FiveHour: reading(agentlog.FiveHour, 0.10, at.Add(time.Hour)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			intervals, latest, err := spend.Intervals(tt.readings, tt.previous, eightPerHour)
			if err != nil {
				t.Fatalf("Intervals() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantIntervals, intervals); diff != "" {
				t.Errorf("Intervals() intervals mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantLatest, latest); diff != "" {
				t.Errorf("Intervals() latest mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
