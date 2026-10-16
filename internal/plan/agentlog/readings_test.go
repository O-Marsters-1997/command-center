package agentlog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func TestParseReadings(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 25, 10, 56, 6, 28_000_000, time.UTC)
	tests := []struct {
		name    string
		fixture string
		want    []agentlog.Reading
	}{
		{
			name:    "reads each window's rate limit",
			fixture: "run27.jsonl",
			want: []agentlog.Reading{
				{Window: agentlog.FiveHour, Utilization: 0.05, ResetsAt: time.Unix(1787665200, 0).UTC(), At: at},
				{Window: agentlog.SevenDay, Utilization: 0.19, ResetsAt: time.Unix(1788159600, 0).UTC(), At: at},
			},
		},
		{name: "a log with no rate limit event is empty", fixture: "alive.jsonl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentlog.ParseReadings(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ParseReadings(%s) error = %v", tt.fixture, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseReadings(%s) mismatch (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}

	t.Run("an absent log is an error", func(t *testing.T) {
		t.Parallel()
		if _, err := agentlog.ParseReadings(filepath.Join(t.TempDir(), "nothing.jsonl")); err == nil {
			t.Error("ParseReadings(absent) error = nil, want one")
		}
	})
}
