package cc_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// backfillFixtureRun disposes a run with no metrics -- the shape every pre-migration run is in --
// and returns its id.
func backfillFixtureRun(t *testing.T, store *cc.Store, ticketURL, logPath string) int64 {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), ticketURL, "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(t.Context(), runID, 4242, at, logPath); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordDisposition(t.Context(), runID, plan.OutcomePush, nil, at, nil); err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestBackfillMetricsPopulatesSurvivingLogsAndLeavesPrunedOnesNull(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	seedOneTicket(t, store)

	survivingRun := backfillFixtureRun(t, store, "sandbox://CC-1", "/state/runs/survives.jsonl")
	prunedRun := backfillFixtureRun(t, store, "sandbox://CC-1", "/state/runs/pruned.jsonl")

	parser := func(logPath string) (agentlog.RunMetrics, error) {
		if logPath == "/state/runs/pruned.jsonl" {
			return agentlog.RunMetrics{}, fmt.Errorf("open agent log %s: %w", logPath, fs.ErrNotExist)
		}
		return agentlog.RunMetrics{TokensIn: 10, TokensOut: 5, Settled: true}, nil
	}

	if err := cc.BackfillMetrics(ctx, store, parser, t.TempDir()); err != nil {
		t.Fatalf("BackfillMetrics: %v", err)
	}

	survived := readRunMetrics(t, dsn, survivingRun)
	if survived.TokensIn.Int64 != 10 || !survived.MetricsSettled.Valid || !survived.MetricsSettled.Bool {
		t.Errorf("surviving run metrics = %+v, want tokens_in 10, settled true", survived)
	}

	pruned := readRunMetrics(t, dsn, prunedRun)
	if pruned.TokensIn.Valid || pruned.MetricsSettled.Valid {
		t.Errorf("pruned run metrics = %+v, want every column NULL", pruned)
	}

	// Safe to run twice: the surviving run is already settled, so a second pass must not reparse
	// it, though the still-NULL pruned run remains a candidate every time its log stays missing.
	var reparsed []string
	secondPass := func(logPath string) (agentlog.RunMetrics, error) {
		reparsed = append(reparsed, logPath)
		return agentlog.RunMetrics{}, fmt.Errorf("open agent log %s: %w", logPath, fs.ErrNotExist)
	}
	if err := cc.BackfillMetrics(ctx, store, secondPass, t.TempDir()); err != nil {
		t.Fatalf("BackfillMetrics second pass: %v", err)
	}
	if len(reparsed) != 1 || reparsed[0] != "/state/runs/pruned.jsonl" {
		t.Errorf("reparsed = %v, want only the still-unsettled pruned log", reparsed)
	}
}

func TestBackfillMetricsPopulatesRunRequests(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)

	runID := backfillFixtureRun(t, store, "sandbox://CC-1", "/state/runs/survives.jsonl")
	parser := func(logPath string) (agentlog.RunMetrics, error) {
		return agentlog.RunMetrics{
			TokensIn: 3, TokensOut: 1, Settled: true,
			Requests: []agentlog.Request{
				{ID: "r1", Thread: agentlog.MainThread, InputTokens: 2, CacheReadTokens: 1, OutputTokens: 1},
			},
		}, nil
	}
	if err := cc.BackfillMetrics(ctx, store, parser, t.TempDir()); err != nil {
		t.Fatalf("BackfillMetrics: %v", err)
	}

	got, err := store.RunRequestsForRun(ctx, runID)
	if err != nil {
		t.Fatalf("RunRequestsForRun: %v", err)
	}
	if len(got) != 1 || got[0].RequestID != "r1" || got[0].InputTokens != 2 {
		t.Errorf("RunRequestsForRun = %+v, want one backfilled row for r1", got)
	}
}

// TestBackfillMetricsAlsoExtractsReadingsFromTheSameLog covers the ticket's "backfill fills them
// from surviving logs": readings come from the very same bytes BackfillMetrics already opens for
// metrics, so a real log on disk is what proves the wiring rather than a fake MetricsParser.
func TestBackfillMetricsAlsoExtractsReadingsFromTheSameLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)

	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	line := `{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{` +
		`"five_hour":{"utilization":0.05,"resetsAt":1787665200}}}}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	backfillFixtureRun(t, store, "sandbox://CC-1", logPath)

	noMetrics := func(logPath string) (agentlog.RunMetrics, error) {
		return agentlog.RunMetrics{}, fmt.Errorf("open agent log %s: %w", logPath, fs.ErrNotExist)
	}
	if err := cc.BackfillMetrics(ctx, store, noMetrics, t.TempDir()); err != nil {
		t.Fatalf("BackfillMetrics: %v", err)
	}

	gauges, err := store.LatestReadings(ctx)
	if err != nil {
		t.Fatalf("LatestReadings: %v", err)
	}
	if got := gauges[agentlog.FiveHour]; got.Utilization != 0.05 {
		t.Errorf("five_hour gauge = %+v; want utilization 0.05", got)
	}
}
