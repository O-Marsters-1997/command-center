package loop_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/spend"
)

// oneMillionInputTokensLine is one $6.40 (at the calibrated sonnet rate) assistant request, for a
// test to place at a chosen timestamp and request id.
func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

// TestRecordReadingsAndIntervalsFitsAKnownFactor drives the whole pipeline end to end: six
// readings an hour apart, a transcripts dir holding one $6.40 request per hour, and asserts
// FitFactors recovers the factor those figures imply (0.02 utilization rise per $6.40 spent).
func TestRecordReadingsAndIntervalsFitsAKnownFactor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	projectsDir := t.TempDir()
	project := filepath.Join(projectsDir, "proj")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}

	var lines string
	for i := range 5 {
		at := start.Add(time.Duration(i)*time.Hour + 30*time.Minute)
		lines += oneMillionInputTokensLine(at.Format(time.RFC3339), fmt.Sprintf("r%d", i)) + "\n"
	}
	if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := range 6 {
		at := start.Add(time.Duration(i) * time.Hour)
		reading := agentlog.Reading{
			Window: agentlog.FiveHour, Utilization: float64(i) * 0.02,
			ResetsAt: at.Add(5 * time.Hour), At: at,
		}
		if err := store.RecordReadingsAndIntervals(ctx, []agentlog.Reading{reading}, projectsDir); err != nil {
			t.Fatalf("RecordReadingsAndIntervals (reading %d): %v", i, err)
		}
	}

	now := start.Add(6 * time.Hour)
	fits, err := store.FitFactors(ctx, now)
	if err != nil {
		t.Fatalf("FitFactors: %v", err)
	}
	got, ok := fits[agentlog.FiveHour]
	if !ok {
		t.Fatal("FitFactors returned no result for five_hour")
	}
	if got.Samples != 5 {
		t.Errorf("Samples = %d; want 5", got.Samples)
	}
	const wantFactor = 0.02 / 6.40
	if diff := (got.Factor - wantFactor) / wantFactor; diff > 0.01 || diff < -0.01 {
		t.Errorf("Factor = %v; want within 1%% of %v", got.Factor, wantFactor)
	}
}

// TestFitFactorsOmitsAWindowBelowMinSamples covers the masthead's own "calibrating" reading: with
// fewer than spend.MinSamples trailing intervals, the window is simply absent.
func TestFitFactorsOmitsAWindowBelowMinSamples(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	for i := range spend.MinSamples - 1 {
		at := start.Add(time.Duration(i) * time.Hour)
		reading := agentlog.Reading{
			Window: agentlog.FiveHour, Utilization: float64(i) * 0.02,
			ResetsAt: at.Add(5 * time.Hour), At: at,
		}
		if err := store.RecordReadingsAndIntervals(ctx, []agentlog.Reading{reading}, t.TempDir()); err != nil {
			t.Fatalf("RecordReadingsAndIntervals (reading %d): %v", i, err)
		}
	}

	fits, err := store.FitFactors(ctx, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("FitFactors: %v", err)
	}
	if _, ok := fits[agentlog.FiveHour]; ok {
		t.Errorf("FitFactors returned a result below MinSamples: %+v", fits)
	}
}

// TestCCCostUSDSumsWithinEachWindowsOwnTrailingSpan covers the masthead's "cost_usd of cc runs in
// the window" input: a run inside the span counts, one before it does not, and each window uses
// its own span length.
func TestCCCostUSDSumsWithinEachWindowsOwnTrailingSpan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	recordRun := func(endedAt time.Time, costUSD float64) {
		t.Helper()
		runID, err := store.InsertRunSkeleton(ctx, "sandbox://CC-1", "agent", "deadbeef", fmt.Sprintf("hash-%v", endedAt))
		if err != nil {
			t.Fatal(err)
		}
		metrics := &agentlog.RunMetrics{CostUSD: &costUSD, Settled: true}
		if err := store.RecordDisposition(ctx, runID, plan.OutcomePush, nil, endedAt, metrics); err != nil {
			t.Fatal(err)
		}
	}

	recordRun(now.Add(-1*time.Hour), 2.00)    // inside both windows
	recordRun(now.Add(-6*time.Hour), 3.00)    // inside seven_day only
	recordRun(now.Add(-8*24*time.Hour), 4.00) // outside both

	got, err := store.CCCostUSD(ctx, now)
	if err != nil {
		t.Fatalf("CCCostUSD: %v", err)
	}
	if got[agentlog.FiveHour] != 2.00 {
		t.Errorf("five_hour = %v; want 2.00", got[agentlog.FiveHour])
	}
	if got[agentlog.SevenDay] != 5.00 {
		t.Errorf("seven_day = %v; want 5.00", got[agentlog.SevenDay])
	}
}
