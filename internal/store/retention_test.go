package store_test

import (
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

func TestStore_PruneHistory(t *testing.T) {
	t.Parallel()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	db := openRawDB(t, dsn)

	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string { return "'" + now.Add(-d).Format(time.RFC3339) + "'" }
	day := 24 * time.Hour

	tickets := []storepkg.Ticket{{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	const (
		consumedIntent = "INSERT INTO intents (at, ticket_id, verb, consumed_at) VALUES "
		openIntent     = "INSERT INTO intents (at, ticket_id, verb) VALUES "
		reading        = `INSERT INTO utilization_readings (at, "window", utilization, resets_at) VALUES `
		interval       = `INSERT INTO utilization_intervals ("window", start_at, end_at, ` +
			`utilization_start, utilization_end, weight_usd) VALUES `
		run  = "INSERT INTO runs (ticket_id, kind, ended_at) VALUES "
		push = "INSERT INTO pushes (ticket_id, pushed_tip, base_branch, base_sha_at_push, pushed_at) VALUES "
	)
	for _, stmt := range []string{
		consumedIntent + "(" + ago(60*day) + ", 'sandbox://CC-1', 'launch', " + ago(31*day) + ")",
		consumedIntent + "(" + ago(60*day) + ", 'sandbox://CC-1', 'launch', " + ago(29*day) + ")",
		openIntent + "(" + ago(60*day) + ", 'sandbox://CC-1', 'launch')",

		"INSERT INTO events (at, kind) VALUES (" + ago(31*day) + ", 'tick_error')",
		"INSERT INTO events (at, kind) VALUES (" + ago(29*day) + ", 'tick_error')",
		"INSERT INTO events (at, kind) VALUES (" + ago(400*day) + ", 'pr_merged')",

		reading + "(" + ago(200*day) + ", 'five_hour', 0.1, " + ago(199*day) + ")",
		reading + "(" + ago(91*day) + ", 'five_hour', 0.2, " + ago(90*day) + ")",
		reading + "(" + ago(89*day) + ", 'five_hour', 0.3, " + ago(88*day) + ")",
		reading + "(" + ago(100*day) + ", 'seven_day', 0.4, " + ago(99*day) + ")",
		reading + "(" + ago(150*day) + ", 'seven_day', 0.5, " + ago(149*day) + ")",

		interval + "('five_hour', " + ago(16*day) + ", " + ago(15*day) + ", 0, 0.1, 1)",
		interval + "('five_hour', " + ago(14*day-time.Hour) + ", " + ago(13*day) + ", 0, 0.1, 1)",

		run + "('sandbox://CC-1', 'agent', " + ago(500*day) + ")",
		push + "('sandbox://CC-1', 'a', 'main', 'b', " + ago(500*day) + ")",
	} {
		mustExec(t.Context(), t, db, stmt)
	}

	if err := store.PruneHistory(t.Context(), now); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"consumed intents keep the recent and the unconsumed", "SELECT count(*) FROM intents", 2},
		{"tick errors keep the recent", "SELECT count(*) FROM events WHERE kind = 'tick_error'", 1},
		{"other event kinds survive", "SELECT count(*) FROM events WHERE kind = 'pr_merged'", 1},
		{
			"five_hour readings keep the recent and drop the old",
			`SELECT count(*) FROM utilization_readings WHERE "window" = 'five_hour'`, 1,
		},
		{
			"a window's only reading survives past the limit",
			`SELECT count(*) FROM utilization_readings WHERE "window" = 'seven_day'`, 1,
		},
		{"intervals keep those ending within 14 days", "SELECT count(*) FROM utilization_intervals", 1},
		{"runs survive", "SELECT count(*) FROM runs", 1},
		{"pushes survive", "SELECT count(*) FROM pushes", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got int
			if err := db.QueryRow(tt.query).Scan(&got); err != nil {
				t.Fatalf("query %q: %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("after PruneHistory, %s = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}
