package agentlog_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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
				Wall:    option.Some(561464 * time.Millisecond),
				Turns:   4, Input: 82, CacheWrite: 62232, CacheRead: 9858576, Output: 35983,
				Tools: map[string]int{"Bash": 2, "Read": 1, "Skill": 2},
				Requests: []agentlog.RequestUsage{
					{Input: 2, CacheCreate: 28560, CacheRead: 24902, Output: 6},
					{Input: 2, CacheCreate: 138, CacheRead: 125101, Output: 8},
					{Input: 2, CacheCreate: 241, CacheRead: 176056, Output: 3},
					{Input: 2, CacheCreate: 2562, CacheRead: 253508, Output: 4},
				},
			},
		},
		{
			name:    "a run with no result line keeps its requests and has no cost",
			fixture: "alive.jsonl",
			want: agentlog.RunMetrics{
				Turns: 3, Input: 11, CacheWrite: 22, CacheRead: 33, Output: 16,
				Tools: map[string]int{"Bash": 1, "Edit": 1, "Skill": 1},
				Requests: []agentlog.RequestUsage{
					{Input: 10, CacheCreate: 20, CacheRead: 30, Output: 5},
					{Input: 1, CacheCreate: 2, CacheRead: 3, Output: 4},
					{Output: 7},
				},
			},
		},
		{
			name:    "a dialect that emits no cost has no CostUSD, not zero",
			fixture: "no_cost.jsonl",
			want: agentlog.RunMetrics{
				Wall: option.Some(500 * time.Millisecond), Turns: 1, Input: 5, Output: 3, Tools: map[string]int{"Bash": 1},
				Requests: []agentlog.RequestUsage{{Output: 3}},
			},
		},
		{
			name:    "subagent requests are not turns but their tools are counted",
			fixture: "subagent.jsonl",
			want: agentlog.RunMetrics{
				Turns: 2, Input: 4, Output: 4, Tools: map[string]int{"Task": 1, "Bash": 1},
				Requests: slices.Repeat([]agentlog.RequestUsage{{Input: 1, Output: 1}}, 4),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentlog.ParseMetrics(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ParseMetrics(%s) error = %v", tt.fixture, err)
			}
			equalOption := cmpopts.EquateComparable(option.Option[float64]{}, option.Option[time.Duration]{})
			tokensOnly := cmpopts.IgnoreFields(agentlog.RequestUsage{}, "At", "Model")
			if diff := cmp.Diff(tt.want, got, equalOption, tokensOnly); diff != "" {
				t.Errorf("ParseMetrics(%s) mismatch (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		empty := filepath.Join(t.TempDir(), "empty.jsonl")
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatalf("write empty log: %v", err)
		}
		tests := []struct {
			name string
			path string
		}{
			{name: "an absent log", path: filepath.Join(t.TempDir(), "missing.jsonl")},
			{name: "an empty log", path: empty},
			{name: "a log truncated before its first complete line", path: filepath.Join("testdata", "corrupt.jsonl")},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if _, err := agentlog.ParseMetrics(tt.path); err == nil {
					t.Errorf("ParseMetrics(%s) error = nil, want one", tt.name)
				}
			})
		}
	})
}
