package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

const sonnetMillionInputUSD = 6.40

func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

func recordFiveHourReadings(t *testing.T, store *storepkg.Store, start time.Time, n int, projectsDir string) {
	t.Helper()
	for i := range n {
		at := start.Add(time.Duration(i) * time.Hour)
		reading := agentlog.Reading{
			Window: agentlog.FiveHour, Utilization: float64(i) * 0.02, ResetsAt: at.Add(5 * time.Hour), At: at,
		}
		if err := store.RecordReadingsAndIntervals(t.Context(), []agentlog.Reading{reading}, projectsDir); err != nil {
			t.Fatalf("RecordReadingsAndIntervals(reading %d): %v", i, err)
		}
	}
}

func TestStore_FitFactors(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	t.Run("fits a known factor from recorded intervals", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		projectsDir := t.TempDir()
		project := filepath.Join(projectsDir, "proj")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", project, err)
		}
		var lines string
		for i := range 5 {
			at := start.Add(time.Duration(i)*time.Hour + 30*time.Minute)
			lines += oneMillionInputTokensLine(at.Format(time.RFC3339), fmt.Sprintf("r%d", i)) + "\n"
		}
		if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(lines), 0o644); err != nil {
			t.Fatalf("WriteFile(session.jsonl): %v", err)
		}
		recordFiveHourReadings(t, store, start, 6, projectsDir)

		fits, err := store.FitFactors(t.Context(), start.Add(6*time.Hour))
		if err != nil {
			t.Fatalf("FitFactors: %v", err)
		}
		got, ok := fits[agentlog.FiveHour]
		if !ok {
			t.Fatalf("FitFactors() = %+v, want a five_hour result", fits)
		}
		const wantFactor = 0.02 / sonnetMillionInputUSD
		if got.Samples != 5 || got.Factor < wantFactor*0.99 || got.Factor > wantFactor*1.01 {
			t.Errorf("FitFactors()[five_hour] = %d samples, factor %v, want 5 samples within 1%% of %v",
				got.Samples, got.Factor, wantFactor)
		}
	})

	t.Run("omits a window below MinSamples", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		recordFiveHourReadings(t, store, start, spend.MinSamples-1, t.TempDir())

		fits, err := store.FitFactors(t.Context(), start.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("FitFactors: %v", err)
		}
		if _, ok := fits[agentlog.FiveHour]; ok {
			t.Errorf("FitFactors() = %+v, want no five_hour result below MinSamples", fits)
		}
	})
}

func TestStore_CCCostUSD(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, run := range []struct {
		endedAt time.Time
		cost    float64
	}{
		{now.Add(-1 * time.Hour), 2.00},
		{now.Add(-6 * time.Hour), 3.00},
		{now.Add(-8 * 24 * time.Hour), 4.00},
	} {
		runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: "sandbox://CC-1", Kind: "agent", BaselineSHA: "deadbeef", PromptHash: fmt.Sprintf("hash-%v", run.endedAt),
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
		if err := store.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.None[int](),
			Cost: option.Some(run.cost), EndedAt: run.endedAt,
		}); err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}
	}

	got, err := store.CCCostUSD(ctx, now)
	if err != nil {
		t.Fatalf("CCCostUSD: %v", err)
	}
	want := map[agentlog.Window]float64{agentlog.FiveHour: 2.00, agentlog.SevenDay: 5.00}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CCCostUSD(%s) mismatch (-want +got):\n%s", now, diff)
	}
}
