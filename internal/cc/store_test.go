package cc_test

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
)

func openStore(t *testing.T) *cc.Store { return openStoreAt(t, cctest.DSN(t)) }

func openStoreAt(t *testing.T, dsn string) *cc.Store {
	t.Helper()
	store, err := cc.OpenStore(dsn)
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

func gooseVersion(t *testing.T, dsn string) int64 {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
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

	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatalf("an empty database did not get the full schema: %v", err)
	}
	if got := gooseVersion(t, dsn); got != 1 {
		t.Errorf("goose version = %d, want 1", got)
	}
}

func TestOpenStoreTwiceIsANoOp(t *testing.T) {
	t.Parallel()

	dsn := cctest.DSN(t)
	first := openStoreAt(t, dsn)
	if err := first.UpsertTickets(t.Context(), []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openStoreAt(t, dsn)
	tickets, err := second.Tickets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Errorf("tickets = %d, want the first open's row to survive the second", len(tickets))
	}
	if got := gooseVersion(t, dsn); got != 1 {
		t.Errorf("goose version = %d, want 1", got)
	}
}

func TestUpsertTicketsIsIdempotentOnTicketURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

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

// TestDeleteTicketRemovesARowThatHasRunsAndPushes covers the reason DeleteTicket cannot be a
// plain `DELETE FROM tickets`: runs and pushes both hold a foreign key back to the ticket's url,
// and a merged ticket -- the only ticket this verb ever targets -- always has at least a run.
func TestDeleteTicketRemovesARowThatHasRunsAndPushes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	runID, err := store.InsertRunSkeleton(ctx, ticket.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := store.RecordSpawn(ctx, runID, 111, at, "runs/1.jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPush(ctx, ticket.URL, "deadbeef", "main", "cafebabe", at); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteTicket(ctx, ticket.URL); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want none left after DeleteTicket", tickets)
	}
}
