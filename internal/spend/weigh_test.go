package spend_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func TestSumWeight(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exact := 123.45
	cost := option.Some(exact)
	settled := agentlog.RequestUsage{At: at, Model: "claude-sonnet-5", Input: 1, CostUSD: cost}
	outside := agentlog.RequestUsage{At: at.Add(time.Hour), Model: "claude-sonnet-5", Input: 1, CostUSD: cost}

	tests := []struct {
		name     string
		requests []agentlog.RequestUsage
		want     float64
	}{
		{name: "a settled request counts its own exact cost", requests: []agentlog.RequestUsage{settled}, want: exact},
		{name: "a request outside the window counts nothing", requests: []agentlog.RequestUsage{outside}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := spend.SumWeight(tt.requests, at, at.Add(time.Minute)); got != tt.want {
				t.Errorf("SumWeight(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
