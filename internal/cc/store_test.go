package cc_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func openStore(t *testing.T, path string) *cc.Store {
	t.Helper()
	store, err := cc.OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

func gooseVersion(t *testing.T, path string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var v int64
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	return v
}

func TestOpenStoreMigratesAFreshDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cc.db")
	store := openStore(t, path)
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatalf("an empty database did not get the full schema: %v", err)
	}
	if got := gooseVersion(t, path); got != 2 {
		t.Errorf("goose version = %d, want 2", got)
	}
}

// TestOpenStoreAdoptsADatabaseThatPredatesGoose is the migration that matters: the live database
// already holds every table 0001 creates, so 0001 must apply as a no-op, goose must record it,
// and no row may move.
func TestOpenStoreAdoptsADatabaseThatPredatesGoose(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cc.db")
	schema, err := cc.Migration0001()
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (ticket_url, repo, branch) VALUES ('sandbox://CC-1', 'cc-sandbox', 'cc-1')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store := openStore(t, path)
	tickets, err := store.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != "sandbox://CC-1" {
		t.Errorf("tickets = %+v, want the pre-goose row untouched", tickets)
	}
	if got := gooseVersion(t, path); got != 2 {
		t.Errorf("goose version = %d, want 2 recorded against the adopted database", got)
	}
}

func TestOpenStoreTwiceIsANoOp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cc.db")
	first := openStore(t, path)
	if err := first.UpsertTickets(t.Context(), []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openStore(t, path)
	tickets, err := second.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Errorf("tickets = %d, want the first open's row to survive the second", len(tickets))
	}
	if got := gooseVersion(t, path); got != 2 {
		t.Errorf("goose version = %d, want 2", got)
	}
}

func TestUpsertTicketsIsIdempotentOnTicketURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	first := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTickets(ctx, first); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	edited := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-renamed"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTickets(ctx, edited); err != nil {
		t.Fatalf("UpsertTickets again: %v", err)
	}

	got, err := store.Tickets(ctx)
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tickets = %d, want 2 (upsert on url, not insert)", len(got))
	}
	if got[0].Branch != "cc-1-renamed" {
		t.Errorf("branch = %q, want the edited %q", got[0].Branch, "cc-1-renamed")
	}
	if len(got[1].BlockedBy) != 1 || got[1].BlockedBy[0] != "sandbox://CC-1" {
		t.Errorf("blocked_by = %v, want [sandbox://CC-1]", got[1].BlockedBy)
	}
}
