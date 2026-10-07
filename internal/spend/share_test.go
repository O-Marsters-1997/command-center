package spend_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func TestPct(t *testing.T) {
	t.Parallel()
	tests := []struct {
		utilization float64
		want        int
	}{{0, 0}, {0.004, 0}, {0.005, 1}, {0.42, 42}, {1, 100}}
	for _, tt := range tests {
		if got := spend.Pct(tt.utilization); got != tt.want {
			t.Errorf("Pct(%v) = %d, want %d", tt.utilization, got, tt.want)
		}
	}
}

func TestPaused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		utilization float64
		limit       int
		want        bool
	}{
		{"no limit never pauses", 0.99, 0, false},
		{"below the limit", 0.79, 80, false},
		{"at the limit", 0.80, 80, true},
		{"above the limit", 0.95, 80, true},
		{"no reading yet", 0, 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := spend.Paused(tt.utilization, tt.limit); got != tt.want {
				t.Errorf("Paused(%v, %d) = %v, want %v", tt.utilization, tt.limit, got, tt.want)
			}
		})
	}
}

func TestShare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		totalPct        int
		fit             spend.Result
		ccUSD           float64
		wantPct         int
		wantCalibrating bool
	}{
		{"no fit is calibrating", 40, spend.Result{}, 5, 0, true},
		{"below MinSamples is calibrating", 40, spend.Result{Factor: 0.01, Samples: spend.MinSamples - 1}, 5, 0, true},
		{"trusted fit scales cc dollars", 40, spend.Result{Factor: 0.02, Samples: spend.MinSamples}, 5, 10, false},
		{"share clamps to the window total", 40, spend.Result{Factor: 0.02, Samples: spend.MinSamples}, 100, 40, false},
		{"negative factor clamps to zero", 40, spend.Result{Factor: -0.02, Samples: spend.MinSamples}, 5, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotPct, gotCal := spend.Share(tt.totalPct, tt.fit, tt.ccUSD)
			if gotPct != tt.wantPct || gotCal != tt.wantCalibrating {
				t.Errorf("Share = (%d, %v), want (%d, %v)", gotPct, gotCal, tt.wantPct, tt.wantCalibrating)
			}
		})
	}
}

func TestKindPctWeek(t *testing.T) {
	t.Parallel()
	agent, resolve, followUp, total := spend.KindPctWeek(1, 2, 3, 0.5)
	if agent != 50 || resolve != 100 || followUp != 150 || total != 300 {
		t.Errorf("KindPctWeek(1,2,3,0.5) = %v %v %v %v, want 50 100 150 300", agent, resolve, followUp, total)
	}
}
