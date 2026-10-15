package store_test

import (
	"database/sql"
	"errors"
	"net/url"
	"slices"
	"syscall"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return cctest.Store(t)
}

func openStoreAt(t *testing.T, dsn string) *storepkg.Store {
	t.Helper()
	store, err := storepkg.OpenStore(dsn, storepkg.MaxOpenConns(4))
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

func TestOpenStoreDoesNotMigrateAClonedDatabase(t *testing.T) {
	t.Parallel()

	dsn := cctest.DSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	if _, err := db.ExecContext(t.Context(), "DROP TABLE goose_db_version"); err != nil {
		t.Fatalf("DROP TABLE goose_db_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	store := openStoreAt(t, dsn)
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatalf("cloned database has no schema: %v", err)
	}
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close verification connection: %v", err)
		}
	})
	var exists bool
	err = db.QueryRowContext(t.Context(), "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists)
	if err != nil {
		t.Fatalf("QueryRowContext(to_regclass).Scan: %v", err)
	}
	if exists {
		t.Error("OpenStore restored goose_db_version on a cloned database, want no migration")
	}
}

func TestOpenStoreMigratesAnUnmigratedDatabase(t *testing.T) {
	t.Parallel()
	dsn := cctest.UnmigratedDSN(t)
	store := openStoreAt(t, dsn)
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Errorf("UpsertTickets after migration: %v", err)
	}
}

func TestOpenStoreFailsFastOnAnUnreachableDatabase(t *testing.T) {
	t.Parallel()

	start := time.Now()
	_, err := storepkg.OpenStore("postgres://cc@127.0.0.1:1/cc?sslmode=disable")
	if err == nil {
		t.Fatal("OpenStore on a dead address succeeded")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("OpenStore error = %v, want the refused connection from the ping", err)
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Errorf("OpenStore took %v, want under the 5s ping timeout", elapsed)
	}
}

func TestConcurrentOpenStoresMigrateOnce(t *testing.T) {
	t.Parallel()

	dsn := cctest.UnmigratedDSN(t)
	const opens = 4
	errs := make(chan error, opens)
	for range opens {
		go func() {
			st, err := storepkg.OpenStore(dsn, storepkg.MaxOpenConns(4))
			if err == nil {
				err = st.Close()
			}
			errs <- err
		}()
	}
	for range opens {
		if err := <-errs; err != nil {
			t.Errorf("concurrent OpenStore: %v", err)
		}
	}
	store := openStoreAt(t, dsn)
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Errorf("UpsertTickets after concurrent migration: %v", err)
	}
}

func TestOpenStoreAppliesStatementTimeout(t *testing.T) {
	t.Parallel()

	u, err := url.Parse(cctest.DSN(t))
	if err != nil {
		t.Fatalf("url.Parse(test DSN): %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "100")
	u.RawQuery = q.Encode()

	db, err := storepkg.OpenStore(u.String(), storepkg.MaxOpenConns(4))
	if err != nil {
		t.Fatalf("OpenStore(timeout DSN): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.DB().ExecContext(t.Context(), `SELECT pg_sleep(2)`); err == nil {
		t.Error("a 2s query under a 100ms statement_timeout succeeded")
	}
}

func TestOpenStoreTwiceIsANoOp(t *testing.T) {
	t.Parallel()

	dsn := cctest.DSN(t)
	first := openStoreAt(t, dsn)
	if err := first.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatalf("first.UpsertTickets: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first.Close: %v", err)
	}

	second := openStoreAt(t, dsn)
	tickets, err := second.Tickets(t.Context())
	if err != nil {
		t.Fatalf("second.Tickets: %v", err)
	}
	if len(tickets) != 1 {
		t.Errorf("tickets = %d, want the first open's row to survive the second", len(tickets))
	}
}

func TestUpsertTicketsIsIdempotentOnTicketURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)

	first := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTickets(ctx, first); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	edited := []storepkg.Ticket{
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

func TestUpsertTicketWithNoBlockersRoundTripsAsEmptyArray(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dsn := cctest.DSN(t)
	store := openStoreAt(t, dsn)

	if err := store.UpsertTickets(ctx, []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
	}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	defer func() { _ = db.Close() }()

	var raw string
	if err := db.QueryRow(`SELECT blocked_by::text FROM tickets WHERE url = $1`, "sandbox://CC-1").Scan(&raw); err != nil {
		t.Fatalf("read blocked_by: %v", err)
	}
	if raw != "[]" {
		t.Errorf("blocked_by = %q, want the empty array literal, not JSON null", raw)
	}
}

func TestBlockedByRejectsInvalidJSONAtTheDatabase(t *testing.T) {
	t.Parallel()

	dsn := cctest.DSN(t)
	openStoreAt(t, dsn)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(
		`INSERT INTO tickets (url, repo, branch, blocked_by) VALUES ($1, $2, $3, $4)`,
		"sandbox://CC-1", "cc-sandbox", "cc-1", "not json",
	)
	if err == nil {
		t.Fatal("insert with invalid JSON in blocked_by succeeded, want a database error")
	}
}

func TestWithdrawTicketHidesItButKeepsItsHistory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	at := time.Now()
	if err := store.QueueLaunchIntent(ctx, storepkg.LaunchRequest{
		Ticket: ticket.URL, PromptHash: "hash-1", Group: "group-a", At: at,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}

	runID, err := store.InsertRunSkeleton(
		ctx, storepkg.RunSkeleton{Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1"})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	push := storepkg.Push{
		Ticket: plan.TicketURL(ticket.URL), Tip: "deadbeef", BaseSHA: "cafebabe", BaseBranch: "main", At: at,
	}
	if err := store.RecordPush(ctx, push); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}

	if err := store.WithdrawTicket(ctx, ticket.URL, at.Add(time.Hour), false); err != nil {
		t.Fatalf("WithdrawTicket: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("tickets = %+v, want none left after WithdrawTicket", tickets)
	}

	runIDs, err := store.RunIDsForTicket(ctx, ticket.URL)
	if err != nil {
		t.Fatalf("RunIDsForTicket: %v", err)
	}
	if len(runIDs) != 1 || runIDs[0] != runID {
		t.Errorf("run ids = %v, want the run to survive withdrawal", runIDs)
	}

	tips, err := store.LastPushedTips(ctx)
	if err != nil {
		t.Fatalf("LastPushedTips: %v", err)
	}
	if tips[plan.TicketURL(ticket.URL)] != "deadbeef" {
		t.Errorf("last pushed tips = %+v, want the push to survive withdrawal", tips)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if _, ok := memberships[plan.TicketURL(ticket.URL)]; !ok {
		t.Errorf("memberships = %+v, want the launch membership to survive withdrawal", memberships)
	}
}

func TestTicketEventsAfterReturnsOnlyThisTicketsNewerEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, e := range []storepkg.Event{
		{At: at, TicketURL: "sandbox://CC-1", Kind: storepkg.EventPushed},
		{At: at, TicketURL: "sandbox://CC-2", Kind: storepkg.EventPushFailed},
		{
			At: at.Add(-time.Hour), TicketURL: "sandbox://CC-1", Kind: storepkg.EventRefreshed,
			Detail: "late write, old time",
		},
		{At: at, Kind: storepkg.EventTickError},
	} {
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent(%s): %v", e.Kind, err)
		}
	}

	all, err := store.TicketEventsAfter(ctx, "sandbox://CC-1", 0, 100)
	if err != nil {
		t.Fatalf("TicketEventsAfter: %v", err)
	}
	if len(all) != 2 || all[0].Kind != storepkg.EventPushed || all[1].Kind != storepkg.EventRefreshed {
		t.Fatalf("TicketEventsAfter(CC-1, 0) = %+v, want first then second", all)
	}
	if all[1].Detail != "late write, old time" || !all[1].At.Equal(at.Add(-time.Hour)) {
		t.Errorf("second = %+v, want its detail and time round-tripped", all[1])
	}

	newer, err := store.TicketEventsAfter(ctx, "sandbox://CC-1", all[0].ID, 100)
	if err != nil {
		t.Fatalf("TicketEventsAfter: %v", err)
	}
	if len(newer) != 1 || newer[0].Kind != storepkg.EventRefreshed {
		t.Errorf("TicketEventsAfter(CC-1, %d) = %+v, want only second, whatever its time", all[0].ID, newer)
	}
}

func TestTicketEventsAfterPagesWithoutGapsOrRepeats(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	const ticketURL = "sandbox://CC-1"
	tickets := []storepkg.Ticket{{URL: ticketURL, Repo: "cc-sandbox", Branch: "cc-1"}}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	const total, pageSize = 7, 3
	var want []storepkg.EventKind
	kinds := []storepkg.EventKind{
		storepkg.EventPushRefused, storepkg.EventPushFailed, storepkg.EventPushed,
		storepkg.EventRefreshed, storepkg.EventRestacked, storepkg.EventTickError, storepkg.EventPRMerged,
	}
	for i := range total {
		kind := kinds[i]
		want = append(want, kind)
		if err := store.AppendEvent(ctx, storepkg.Event{At: at, TicketURL: ticketURL, Kind: kind}); err != nil {
			t.Fatalf("AppendEvent(%s): %v", kind, err)
		}
	}

	var got []storepkg.EventKind
	var afterID int64
	for pages := 0; ; pages++ {
		page, err := store.TicketEventsAfter(ctx, ticketURL, afterID, pageSize)
		if err != nil {
			t.Fatalf("TicketEventsAfter(%s, %d, %d): %v", ticketURL, afterID, pageSize, err)
		}
		if len(page) > pageSize {
			t.Fatalf("page %d has %d events, want at most %d", pages, len(page), pageSize)
		}
		for _, e := range page {
			got = append(got, e.Kind)
			afterID = e.ID
		}
		if len(page) < pageSize {
			break
		}
	}
	if !slices.Equal(want, got) {
		t.Errorf("paged kinds = %v, want %v", got, want)
	}
}

func TestWithdrawTicketLeavesEventsAlone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	at := time.Now()
	ticketEvent := storepkg.Event{At: at, TicketURL: ticket.URL, Kind: storepkg.EventPushed}
	if err := store.AppendEvent(ctx, ticketEvent); err != nil {
		t.Fatalf("AppendEvent(ticket): %v", err)
	}
	if err := store.AppendEvent(ctx, storepkg.Event{At: at, Kind: storepkg.EventTickError}); err != nil {
		t.Fatalf("AppendEvent(tick error): %v", err)
	}

	if err := store.WithdrawTicket(ctx, ticket.URL, at.Add(time.Hour), false); err != nil {
		t.Fatalf("WithdrawTicket: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events = %+v, want both the ticket-scoped and the fleet event left", events)
	}
}
