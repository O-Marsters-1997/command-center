package agentlog_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

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
			name:    "a settled run reads cost off the result line",
			fixture: "run27.jsonl",
			want: agentlog.RunMetrics{
				CostUSD: &cost,
				Settled: true,
				Requests: []agentlog.Request{
					{InputTokens: 2, CacheCreationTokens: 28560, CacheReadTokens: 24902, OutputTokens: 6},
					{InputTokens: 2, CacheCreationTokens: 138, CacheReadTokens: 125101, OutputTokens: 8},
					{InputTokens: 2, CacheCreationTokens: 241, CacheReadTokens: 176056, OutputTokens: 3},
					{InputTokens: 2, CacheCreationTokens: 2562, CacheReadTokens: 253508, OutputTokens: 4},
				},
			},
		},
		{
			name:    "a run with no result line keeps its requests and stays unsettled",
			fixture: "alive.jsonl",
			want: agentlog.RunMetrics{
				Requests: []agentlog.Request{
					{InputTokens: 10, CacheCreationTokens: 20, CacheReadTokens: 30, OutputTokens: 5},
					{InputTokens: 1, CacheCreationTokens: 2, CacheReadTokens: 3, OutputTokens: 4},
					{OutputTokens: 7},
				},
			},
		},
		{
			name:    "a dialect that emits no cost settles with CostUSD nil, not zero",
			fixture: "no_cost.jsonl",
			want: agentlog.RunMetrics{
				Settled:  true,
				Requests: []agentlog.Request{{OutputTokens: 3}},
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

func TestParseMetricsContextPerRequest(t *testing.T) {
	t.Parallel()

	got, err := agentlog.ParseMetrics(filepath.Join("testdata", "run27.jsonl"))
	if err != nil {
		t.Fatalf("ParseMetrics: %v", err)
	}

	want := []int64{53464, 125241, 176299, 256072}
	if len(got.Requests) != len(want) {
		t.Fatalf("got %d requests, want %d: %+v", len(got.Requests), len(want), got.Requests)
	}
	for i, r := range got.Requests {
		context := r.InputTokens + r.CacheCreationTokens + r.CacheReadTokens
		if context != want[i] {
			t.Errorf("request %d context = %d, want %d", i, context, want[i])
		}
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
			path: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "missing.jsonl")
			},
		},
		{
			name: "an empty log",
			path: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "empty.jsonl")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("write empty log: %v", err)
				}
				return path
			},
		},
		{
			name: "a log truncated before its first complete line",
			path: func(*testing.T) string { return filepath.Join("testdata", "corrupt.jsonl") },
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

	if got.Settled != want.Settled {
		t.Errorf("Settled = %v; want %v", got.Settled, want.Settled)
	}
	if !slices.Equal(got.Requests, want.Requests) || (got.Requests == nil) != (want.Requests == nil) {
		t.Errorf("Requests = %+v; want %+v", got.Requests, want.Requests)
	}
	gotCost, wantCost := got.CostUSD, want.CostUSD
	switch {
	case wantCost == nil && gotCost != nil:
		t.Errorf("CostUSD = %v; want nil", *gotCost)
	case wantCost != nil && gotCost == nil:
		t.Errorf("CostUSD = nil; want %v", *wantCost)
	case wantCost != nil && gotCost != nil && *gotCost != *wantCost:
		t.Errorf("CostUSD = %v; want %v", *gotCost, *wantCost)
	}
}
