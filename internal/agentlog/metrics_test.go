package agentlog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/option"
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
				CostUSD: option.Some(cost),
				Requests: []agentlog.Request{
					{InputTokens: 2, CacheCreationTokens: 28560, CacheReadTokens: 24902, OutputTokens: 6},
					{InputTokens: 2, CacheCreationTokens: 138, CacheReadTokens: 125101, OutputTokens: 8},
					{InputTokens: 2, CacheCreationTokens: 241, CacheReadTokens: 176056, OutputTokens: 3},
					{InputTokens: 2, CacheCreationTokens: 2562, CacheReadTokens: 253508, OutputTokens: 4},
				},
			},
		},
		{
			name:    "a run with no result line keeps its requests and has no cost",
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
			name:    "a dialect that emits no cost has no CostUSD, not zero",
			fixture: "no_cost.jsonl",
			want: agentlog.RunMetrics{
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
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(func(a, b option.Option[float64]) bool {
				return a == b
			})); diff != "" {
				t.Errorf("ParseMetrics() mismatch (-want +got):\n%s", diff)
			}
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
