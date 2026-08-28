package cc_test

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func seedOneTicket(t *testing.T, store *cc.Store) {
	t.Helper()
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
}

func TestInsertRunSkeletonThenRecordSpawnPopulatesTheRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)

	runID, err := store.InsertRunSkeleton(ctx, "sandbox://CC-1", "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}

	before, err := store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		t.Fatalf("PendingRunsAwaitingDisposition: %v", err)
	}
	if len(before) != 0 {
		t.Errorf("pending = %+v, want none before a pgid is recorded", before)
	}

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}

	pending, err := store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		t.Fatalf("PendingRunsAwaitingDisposition: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	got := pending[0]
	if got.TicketID != "sandbox://CC-1" || got.Pgid != 4242 || got.BaselineSHA != "deadbeef" {
		t.Errorf("pending run = %+v", got)
	}
	if !got.ProcStartedAt.Equal(startedAt) {
		t.Errorf("proc_started_at = %s, want %s", got.ProcStartedAt, startedAt)
	}

	latest, err := store.LatestRunsByTicket(ctx)
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	summary, ok := latest["sandbox://CC-1"]
	if !ok {
		t.Fatal("LatestRunsByTicket has no entry for sandbox://CC-1")
	}
	if summary.LogPath != "/state/runs/1.jsonl" || summary.HasOutcome {
		t.Errorf("summary = %+v, want the log path set and no outcome yet", summary)
	}
}

func TestRecordDispositionMarksTheRunFailedOrPush(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)

	runID, err := store.InsertRunSkeleton(ctx, "sandbox://CC-1", "agent", "deadbeef", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.RecordSpawn(ctx, runID, 4242, startedAt, "/state/runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}

	endedAt := startedAt.Add(30 * time.Second)
	exitCode := 0
	if err := store.RecordDisposition(ctx, runID, plan.OutcomeFailed, &exitCode, endedAt); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}

	pending, err := store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %+v, want none once disposed", pending)
	}

	latest, err := store.LatestRunsByTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	summary := latest["sandbox://CC-1"]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeFailed {
		t.Errorf("summary = %+v, want outcome failed", summary)
	}
	if summary.ExitCode == nil || *summary.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", summary.ExitCode)
	}
	if summary.EndedAt == nil || !summary.EndedAt.Equal(endedAt) {
		t.Errorf("ended_at = %v, want %s", summary.EndedAt, endedAt)
	}
}

func TestInsertCutFailedRunNeverClaimsAPgid(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if _, err := store.InsertCutFailedRun(ctx, "sandbox://CC-1", "hash-1", at); err != nil {
		t.Fatalf("InsertCutFailedRun: %v", err)
	}

	pending, err := store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %+v, want none: a cut failure never claims a pgid", pending)
	}

	latest, err := store.LatestRunsByTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	summary := latest["sandbox://CC-1"]
	if !summary.HasOutcome || summary.Outcome != plan.OutcomeCutFailed || summary.Pgid != nil {
		t.Errorf("summary = %+v, want cut_failed with no pgid", summary)
	}
}

func TestVerbIntentsQueuePendConsume(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(ctx, "sandbox://CC-1", "kill", at); err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}

	pending, err := store.PendingVerbIntents(ctx, "kill")
	if err != nil {
		t.Fatalf("PendingVerbIntents: %v", err)
	}
	if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
		t.Fatalf("pending = %+v, want one kill intent for sandbox://CC-1", pending)
	}

	if err := store.ConsumeVerbIntent(ctx, pending[0].ID, at.Add(time.Second)); err != nil {
		t.Fatalf("ConsumeVerbIntent: %v", err)
	}

	after, err := store.PendingVerbIntents(ctx, "kill")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("pending = %+v, want none once consumed", after)
	}
}

func TestActiveLaunchHashesReturnsTheAuthorisedHashPerTicket(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	hashes, err := store.ActiveLaunchHashes(ctx)
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	if hashes["sandbox://CC-1"] != "hash-1" {
		t.Errorf("hashes = %+v, want sandbox://CC-1 -> hash-1", hashes)
	}
}

func TestPendingIntentsByTicketKeysUnconsumedVerbsByTicket(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	seedOneTicket(t, store)
	second := cc.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{second}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	queued := []struct{ ticket, verb string }{
		{"sandbox://CC-1", "kill"},
		{"sandbox://CC-1", "close-pr"},
		{"sandbox://CC-2", "re-run"},
	}
	for i, q := range queued {
		if err := store.QueueVerbIntent(ctx, q.ticket, q.verb, at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("QueueVerbIntent %s %s: %v", q.ticket, q.verb, err)
		}
	}

	byTicket, err := store.PendingIntentsByTicket(ctx)
	if err != nil {
		t.Fatalf("PendingIntentsByTicket: %v", err)
	}
	want := map[string][]string{
		"sandbox://CC-1": {"kill", "close-pr"},
		"sandbox://CC-2": {"re-run"},
	}
	if !maps.EqualFunc(byTicket, want, slices.Equal) {
		t.Fatalf("byTicket = %+v, want %+v", byTicket, want)
	}

	consumed, err := store.PendingVerbIntents(ctx, "kill")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeVerbIntent(ctx, consumed[0].ID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	after, err := store.PendingIntentsByTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want["sandbox://CC-1"] = []string{"close-pr"}
	if !maps.EqualFunc(after, want, slices.Equal) {
		t.Fatalf("after = %+v, want %+v", after, want)
	}
}
