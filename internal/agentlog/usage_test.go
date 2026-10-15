package agentlog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestParseTranscriptUsage(t *testing.T) {
	t.Parallel()

	cost := 8.288799200000001
	tests := []struct {
		name    string
		fixture string
		want    []agentlog.RequestUsage
	}{
		{
			name:    "a settled run returns its one cumulative entry",
			fixture: "run27.jsonl",
			want: []agentlog.RequestUsage{{
				At:    time.Date(2026, 8, 25, 11, 22, 17, 492_000_000, time.UTC),
				Model: "claude-sonnet-5",
				Input: 82, Output: 35983, CacheRead: 9858576, CacheCreate: 62232,
				CostUSD: &cost,
			}},
		},
		{
			name:    "an interactive transcript returns every request",
			fixture: "alive.jsonl",
			want: []agentlog.RequestUsage{
				{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Input: 10, Output: 5, CacheRead: 30, CacheCreate: 20},
				{At: time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC), Input: 1, Output: 4, CacheRead: 3, CacheCreate: 2},
				{At: time.Date(2026, 1, 1, 0, 0, 4, 0, time.UTC), Output: 7},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentlog.ParseTranscriptUsage(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ParseTranscriptUsage(%s) error = %v", tt.fixture, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTranscriptUsage(%s) mismatch (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}
}
