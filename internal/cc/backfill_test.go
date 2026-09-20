package cc_test

import (
	"fmt"
	"io/fs"
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

	if err := cc.BackfillMetrics(ctx, store, parser); err != nil {
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
	if err := cc.BackfillMetrics(ctx, store, secondPass); err != nil {
		t.Fatalf("BackfillMetrics second pass: %v", err)
	}
	if len(reparsed) != 1 || reparsed[0] != "/state/runs/pruned.jsonl" {
		t.Errorf("reparsed = %v, want only the still-unsettled pruned log", reparsed)
	}
}
