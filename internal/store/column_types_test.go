package store_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

const lastIndexMigration = 32

func migrateTo(t *testing.T, dsn string, version int64) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(t.Context(), version); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
	return db
}

func mustExec(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func TestColumnTypeMigrationsConvertLegacyRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	db := migrateTo(t, dsn, lastIndexMigration)
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	mustExec(ctx, t, db, `INSERT INTO tickets (url, repo, branch, synced_at) VALUES
		('https://x/1', 'r', 'b1', ''), ('https://x/2', 'r', 'b2', $1)`, at.Format(time.RFC3339))
	mustExec(ctx, t, db, `INSERT INTO intents (at, ticket_id, verb, payload) VALUES
		($1, 'https://x/1', 'follow-up', 'fix "the" flaky test'),
		($1, 'https://x/1', 'edit_ticket', '{"branch":"nb","blocked_by":["https://x/2"]}'),
		($1, 'https://x/1', 'launch', '{"prompt_hash":"h","group":"g"}')`, at)
	mustExec(ctx, t, db, `INSERT INTO events (at, ticket_id, kind, detail) VALUES ($1, NULL, 'pushed', NULL)`, at)
	mustExec(ctx, t, db, `INSERT INTO meta (key, value) VALUES ('checking_ticks', '{"https://x/1":2}')`)

	store := openStoreAt(t, dsn)

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byURL := ticketsByURLForTest(t, tickets)
	if got := byURL["https://x/1"].SyncedAt; !got.IsZero() {
		t.Errorf("legacy empty synced_at read back %v, want the zero time", got)
	}
	if got := byURL["https://x/2"].SyncedAt; !got.Equal(at) {
		t.Errorf("legacy synced_at = %v, want %v", got, at)
	}

	followUps, err := store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(followUps) != 1 || followUps[0].Payload != `fix "the" flaky test` {
		t.Errorf("follow-up intents = %+v, want the legacy prompt decoded", followUps)
	}
	edits, err := store.PendingEditTicketIntents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].Branch != "nb" || len(edits[0].BlockedBy) != 1 {
		t.Errorf("edit intents = %+v", edits)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Detail != "" {
		t.Errorf("events = %+v, want the NULL detail backfilled to empty", events)
	}
	ticks, err := store.CheckingTicks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ticks["https://x/1"] != 2 {
		t.Errorf("checking ticks = %v", ticks)
	}
}

func TestImportedTicketReadsBackSyncedAtAsTheImportClock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	tickets := []storepkg.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://x/1", Number: 1, Title: "t"}, Repo: "r", Source: "github"},
	}
	if err := store.ImportTickets(ctx, "project:x", tickets, now); err != nil {
		t.Fatal(err)
	}
	got, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].SyncedAt.Equal(now) {
		t.Errorf("Tickets() = %+v, want synced_at %v", got, now)
	}
}

func TestFollowUpIntentPayloadRoundTripsAsJSON(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prompt := "line one\nline \"two\""
	if err := store.QueueVerbIntentWithPayload(ctx, storepkg.VerbRequest{
		Ticket: "https://x/1", Verb: plan.VerbFollowUp, Payload: prompt, At: at,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Payload != prompt {
		t.Errorf("intents = %+v, want payload %q", got, prompt)
	}
}

func TestMigratedSchemaHasTypedColumnsAndNamedConstraints(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	openStoreAt(t, dsn)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	columns := []struct{ table, column, dataType, nullable string }{
		{"meta", "value", "jsonb", "NO"},
		{"tickets", "synced_at", "timestamp with time zone", "YES"},
		{"intents", "payload", "jsonb", "YES"},
		{"events", "detail", "text", "NO"},
	}
	for _, c := range columns {
		var dataType, nullable string
		err := db.QueryRowContext(ctx, `
			SELECT data_type, is_nullable FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			c.table, c.column).Scan(&dataType, &nullable)
		if err != nil {
			t.Fatalf("%s.%s: %v", c.table, c.column, err)
		}
		if dataType != c.dataType || nullable != c.nullable {
			t.Errorf("%s.%s = %s nullable=%s, want %s nullable=%s",
				c.table, c.column, dataType, nullable, c.dataType, c.nullable)
		}
	}

	constraints := []struct{ table, name string }{
		{"repos", "repos_state_check"},
		{"repos", "repos_refusal_check"},
		{"projects", "projects_hue_check"},
		{"projects", "projects_name_check"},
		{"meta", "meta_pkey"},
	}
	for _, c := range constraints {
		var found bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_constraint
			WHERE conrelid = to_regclass($1) AND conname = $2)`, c.table, c.name).Scan(&found)
		if err != nil || !found {
			t.Errorf("constraint %s on %s missing (err %v)", c.name, c.table, err)
		}
	}
}
