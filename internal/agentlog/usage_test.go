package agentlog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// TestParseTranscriptUsageOnASettledRunReturnsItsOneCumulativeEntry covers a non-interactive `-p`
// run, which reports its whole spend as one entry off the result line rather than one per request.
func TestParseTranscriptUsageOnASettledRunReturnsItsOneCumulativeEntry(t *testing.T) {
	t.Parallel()

	got, err := agentlog.ParseTranscriptUsage(filepath.Join("testdata", "run27.jsonl"))
	if err != nil {
		t.Fatalf("ParseTranscriptUsage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ParseTranscriptUsage = %+v; want exactly one entry", got)
	}

	want := agentlog.RequestUsage{
		Model: "claude-sonnet-5",
		Input: 82, Output: 35983, CacheRead: 9858576, CacheCreate: 62232,
	}
	if got[0].Model != want.Model || got[0].Input != want.Input || got[0].Output != want.Output ||
		got[0].CacheRead != want.CacheRead || got[0].CacheCreate != want.CacheCreate {
		t.Errorf("ParseTranscriptUsage = %+v; want %+v", got[0], want)
	}

	const wantCostUSD = 8.288799200000001
	if got[0].CostUSD == nil || *got[0].CostUSD != wantCostUSD {
		t.Errorf("CostUSD = %v; want the result line's own %v", got[0].CostUSD, wantCostUSD)
	}
}

// TestParseTranscriptUsageOnAnInteractiveTranscriptSumsEveryRequest covers CC-313's acceptance
// criterion: a transcript with no result line -- an interactive session never settles one -- falls
// back to one entry per request instead of the single cumulative entry a settled run yields,
// deduped by request_id the way a live run's streamed usage lines already dedupe.
func TestParseTranscriptUsageOnAnInteractiveTranscriptSumsEveryRequest(t *testing.T) {
	t.Parallel()

	got, err := agentlog.ParseTranscriptUsage(filepath.Join("testdata", "alive.jsonl"))
	if err != nil {
		t.Fatalf("ParseTranscriptUsage: %v", err)
	}

	want := []agentlog.RequestUsage{
		{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Input: 10, Output: 5, CacheRead: 30, CacheCreate: 20},
		{At: time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC), Input: 1, Output: 4, CacheRead: 3, CacheCreate: 2},
		{At: time.Date(2026, 1, 1, 0, 0, 4, 0, time.UTC), Input: 0, Output: 7, CacheRead: 0, CacheCreate: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseTranscriptUsage = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}
