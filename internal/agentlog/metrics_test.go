package agentlog_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestParseMetrics(t *testing.T) {
	t.Parallel()

	cost := 8.288799200000001
	tests := []struct {
		name    string
		fixture string
		want    agentlog.RunMetrics
	}{
		{
			name:    "a settled run reads duration, turns and cost off the result line",
			fixture: "run27.jsonl",
			want: agentlog.RunMetrics{
				TokensIn: 9920890, TokensOut: 35983,
				Turns: 42, Duration: 561464 * time.Millisecond,
				CostUSD:      &cost,
				ToolCalls:    5,
				ToolFailures: 1,
				Model:        "claude-sonnet-5",
				Settled:      true,
			},
		},
		{
			name:    "a run with no result line sums partial usage and stays unsettled",
			fixture: "alive.jsonl",
			want: agentlog.RunMetrics{
				TokensIn:     66,
				TokensOut:    16,
				ToolCalls:    3,
				ToolFailures: 1,
			},
		},
		{
			name:    "a dialect that emits no cost settles with CostUSD nil, not zero",
			fixture: "no_cost.jsonl",
			want: agentlog.RunMetrics{
				TokensIn: 5, TokensOut: 3,
				Turns: 1, Duration: 500 * time.Millisecond,
				CostUSD:   nil,
				ToolCalls: 1,
				Model:     "free-dialect-1",
				Settled:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := agentlog.ParseMetrics(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ParseMetrics: %v", err)
			}
			assertMetrics(t, got, tt.want)
		})
	}
}

func TestParseMetricsRejectsUnusableLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "an absent log",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.jsonl") },
		},
		{
			name: "an empty log",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "empty.jsonl")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("write empty log: %v", err)
				}
				return path
			},
		},
		{
			name: "a log truncated before its first complete line",
			path: func(t *testing.T) string { return filepath.Join("testdata", "corrupt.jsonl") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := agentlog.ParseMetrics(tt.path(t)); err == nil {
				t.Error("ParseMetrics = nil error; want an error")
			}
		})
	}
}

func assertMetrics(t *testing.T, got, want agentlog.RunMetrics) {
	t.Helper()

	gotCost, wantCost := got.CostUSD, want.CostUSD
	got.CostUSD, want.CostUSD = nil, nil
	if got != want {
		t.Errorf("ParseMetrics = %+v; want %+v", got, want)
	}
	switch {
	case wantCost == nil && gotCost != nil:
		t.Errorf("CostUSD = %v; want nil", *gotCost)
	case wantCost != nil && gotCost == nil:
		t.Errorf("CostUSD = nil; want %v", *wantCost)
	case wantCost != nil && gotCost != nil && *gotCost != *wantCost:
		t.Errorf("CostUSD = %v; want %v", *gotCost, *wantCost)
	}
}
