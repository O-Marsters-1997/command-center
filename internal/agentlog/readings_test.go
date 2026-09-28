package agentlog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestParseReadings(t *testing.T) {
	t.Parallel()

	got, err := agentlog.ParseReadings(filepath.Join("testdata", "run27.jsonl"))
	if err != nil {
		t.Fatalf("ParseReadings: %v", err)
	}

	at := time.Date(2026, 8, 25, 10, 56, 6, 28_000_000, time.UTC)
	want := []agentlog.Reading{
		{
			Window: agentlog.FiveHour, Utilization: 0.05,
			ResetsAt: time.Unix(1787665200, 0).UTC(), At: at,
		},
		{
			Window: agentlog.SevenDay, Utilization: 0.19,
			ResetsAt: time.Unix(1788159600, 0).UTC(), At: at,
		},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseReadings = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reading %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestParseReadingsOnALogWithNoRateLimitEventIsEmpty(t *testing.T) {
	t.Parallel()

	got, err := agentlog.ParseReadings(filepath.Join("testdata", "alive.jsonl"))
	if err != nil {
		t.Fatalf("ParseReadings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ParseReadings = %+v; want none", got)
	}
}

func TestParseReadingsOnAnAbsentLogIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := agentlog.ParseReadings(filepath.Join(t.TempDir(), "nothing.jsonl")); err == nil {
		t.Error("ParseReadings = nil error; want an error")
	}
}
