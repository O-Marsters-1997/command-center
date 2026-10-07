package agentlog_test

import (
	"os"
	"path/filepath"
	"reflect"
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
				CostUSD: &cost,
				Model:   "claude-sonnet-5",
				Settled: true,
				Requests: []agentlog.Request{
					{ID: "req_011CePLyfpsLhMDxGkxLCMur", Thread: agentlog.MainThread,
						InputTokens: 2, CacheCreationTokens: 28560, CacheReadTokens: 24902, OutputTokens: 6},
					{ID: "req_011CePMHKBeWZNmiwDf34JiQ", Thread: agentlog.MainThread,
						InputTokens: 2, CacheCreationTokens: 138, CacheReadTokens: 125101, OutputTokens: 8},
					{ID: "req_011CePMx1Yb7eLszzYyMzgsr", Thread: agentlog.MainThread,
						InputTokens: 2, CacheCreationTokens: 241, CacheReadTokens: 176056, OutputTokens: 3},
					{ID: "req_011CePNusaftdEQ1jXDGD1ts", Thread: agentlog.MainThread,
						InputTokens: 2, CacheCreationTokens: 2562, CacheReadTokens: 253508, OutputTokens: 4},
				},
			},
		},
		{
			name:    "a run with no result line sums partial usage and stays unsettled",
			fixture: "alive.jsonl",
			want: agentlog.RunMetrics{
				TokensIn:  66,
				TokensOut: 16,
				Requests: []agentlog.Request{
					{ID: "r1", Thread: agentlog.MainThread,
						InputTokens: 10, CacheCreationTokens: 20, CacheReadTokens: 30, OutputTokens: 5},
					{ID: "r2", Thread: agentlog.MainThread,
						InputTokens: 1, CacheCreationTokens: 2, CacheReadTokens: 3, OutputTokens: 4},
					{ID: "r3", Thread: agentlog.MainThread, OutputTokens: 7},
				},
			},
		},
		{
			name:    "a dialect that emits no cost settles with CostUSD nil, not zero",
			fixture: "no_cost.jsonl",
			want: agentlog.RunMetrics{
				TokensIn: 5, TokensOut: 3,
				Turns: 1, Duration: 500 * time.Millisecond,
				CostUSD: nil,
				Model:   "free-dialect-1",
				Settled: true,
				Requests: []agentlog.Request{
					{ID: "r1", Thread: agentlog.MainThread, OutputTokens: 3},
				},
			},
		},
		{
			// A Task call's subagent writes its own requests into the same log, interleaved with
			// the main thread's. Each is attributed to the Task tool_use id that spawned it, not
			// to request order (acceptance: "attributes the subagent's requests to its tool_use
			// id").
			name:    "a Task call's requests are attributed to its tool_use id",
			fixture: "subagent.jsonl",
			want: agentlog.RunMetrics{
				TokensIn: 4, TokensOut: 4,
				Requests: []agentlog.Request{
					{ID: "r-main-1", Thread: agentlog.MainThread,
						InputTokens: 1, CacheCreationTokens: 0, CacheReadTokens: 0, OutputTokens: 1},
					{ID: "r-sub-1", Thread: "toolu_task1",
						InputTokens: 1, CacheCreationTokens: 0, CacheReadTokens: 0, OutputTokens: 1},
					{ID: "r-sub-2", Thread: "toolu_task1",
						InputTokens: 1, CacheCreationTokens: 0, CacheReadTokens: 0, OutputTokens: 1},
					{ID: "r-main-2", Thread: agentlog.MainThread,
						InputTokens: 1, CacheCreationTokens: 0, CacheReadTokens: 0, OutputTokens: 1},
				},
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

// TestParseMetricsContextPerRequest covers run27's own acceptance criterion: four main-thread
// requests, each carrying the context window (input + cache creation + cache read) the CLI billed
// that turn against.
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
		if r.Thread != agentlog.MainThread {
			t.Errorf("request %d thread = %q, want %q", i, r.Thread, agentlog.MainThread)
		}
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
	if !reflect.DeepEqual(got, want) {
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
