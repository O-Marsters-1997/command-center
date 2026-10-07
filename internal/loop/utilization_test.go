package loop_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestRecordReadingsDedupesOverlappingRuns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	at := time.Date(2026, 8, 25, 10, 56, 6, 0, time.UTC)
	resetsAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	reading := agentlog.Reading{Window: agentlog.FiveHour, Utilization: 0.05, ResetsAt: resetsAt, At: at}

	// Two overlapping runs each parse the same rate_limit_event out of their own log.
	if err := store.RecordReadings(ctx, []agentlog.Reading{reading}); err != nil {
		t.Fatalf("RecordReadings (first run): %v", err)
	}
	if err := store.RecordReadings(ctx, []agentlog.Reading{reading}); err != nil {
		t.Fatalf("RecordReadings (second run): %v", err)
	}

	gauges, err := store.LatestReadings(ctx)
	if err != nil {
		t.Fatalf("LatestReadings: %v", err)
	}
	if len(gauges) != 1 {
		t.Fatalf("gauges = %+v; want exactly one reading stored", gauges)
	}
	got := gauges[agentlog.FiveHour]
	if got.Utilization != 0.05 || !got.ResetsAt.Equal(resetsAt) {
		t.Errorf("gauge = %+v; want utilization 0.05 resetting at %s", got, resetsAt)
	}
}

func TestLatestReadingsReturnsTheNewestPerWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	older := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.10,
		ResetsAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
		At:       time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),
	}
	newer := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.42,
		ResetsAt: time.Date(2026, 8, 25, 15, 0, 0, 0, time.UTC),
		At:       time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC),
	}
	weekly := agentlog.Reading{
		Window: agentlog.SevenDay, Utilization: 0.19,
		ResetsAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		At:       time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC),
	}

	if err := store.RecordReadings(ctx, []agentlog.Reading{older, newer, weekly}); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}

	gauges, err := store.LatestReadings(ctx)
	if err != nil {
		t.Fatalf("LatestReadings: %v", err)
	}
	if len(gauges) != 2 {
		t.Fatalf("gauges = %+v; want one per window", gauges)
	}
	if got := gauges[agentlog.FiveHour]; got.Utilization != 0.42 {
		t.Errorf("five_hour gauge = %+v; want the newer reading (0.42)", got)
	}
	if got := gauges[agentlog.SevenDay]; got.Utilization != 0.19 {
		t.Errorf("seven_day gauge = %+v; want 0.19", got)
	}
}

func TestLatestReadingsWithNoneStoredIsEmpty(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	gauges, err := store.LatestReadings(t.Context())
	if err != nil {
		t.Fatalf("LatestReadings: %v", err)
	}
	if len(gauges) != 0 {
		t.Errorf("gauges = %+v; want none", gauges)
	}
}
