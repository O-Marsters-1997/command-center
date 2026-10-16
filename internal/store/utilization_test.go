package store_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func recordReadings(t *testing.T, store *storepkg.Store, readings ...agentlog.Reading) {
	t.Helper()
	if err := store.RecordReadings(t.Context(), readings); err != nil {
		t.Fatalf("RecordReadings: %v", err)
	}
}

func latestReadings(t *testing.T, store *storepkg.Store) map[agentlog.Window]storepkg.Gauge {
	t.Helper()
	gauges, err := store.LatestReadings(t.Context())
	if err != nil {
		t.Fatalf("LatestReadings: %v", err)
	}
	return gauges
}

func TestStore_RecordReadings(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	resetsAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	reading := agentlog.Reading{
		Window: agentlog.FiveHour, Utilization: 0.05, ResetsAt: resetsAt, At: time.Date(2026, 8, 25, 10, 56, 6, 0, time.UTC),
	}
	recordReadings(t, store, reading)
	recordReadings(t, store, reading)

	gauges := latestReadings(t, store)
	got, ok := gauges[agentlog.FiveHour]
	if len(gauges) != 1 || !ok || got.Utilization != 0.05 || !got.ResetsAt.Equal(resetsAt) {
		t.Errorf("LatestReadings() after recording one reading twice = %+v, "+
			"want one five_hour gauge at 0.05 resetting %s", gauges, resetsAt)
	}
}

func TestStore_LatestReadings(t *testing.T) {
	t.Parallel()

	t.Run("the newest per window", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		recordReadings(t, store,
			agentlog.Reading{
				Window: agentlog.FiveHour, Utilization: 0.10,
				ResetsAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC), At: time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),
			},
			agentlog.Reading{
				Window: agentlog.FiveHour, Utilization: 0.42,
				ResetsAt: time.Date(2026, 8, 25, 15, 0, 0, 0, time.UTC), At: time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC),
			},
			agentlog.Reading{
				Window: agentlog.SevenDay, Utilization: 0.19,
				ResetsAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), At: time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC),
			},
		)

		gauges := latestReadings(t, store)
		if len(gauges) != 2 || gauges[agentlog.FiveHour].Utilization != 0.42 ||
			gauges[agentlog.SevenDay].Utilization != 0.19 {
			t.Errorf("LatestReadings() = %+v, want five_hour 0.42 and seven_day 0.19", gauges)
		}
	})

	t.Run("none stored", func(t *testing.T) {
		t.Parallel()
		if gauges := latestReadings(t, openStore(t)); len(gauges) != 0 {
			t.Errorf("LatestReadings() = %+v, want none", gauges)
		}
	})
}
