package store_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return cctest.Store(t)
}

func openStoreAt(t *testing.T, dsn string, opts ...storepkg.Option) *storepkg.Store {
	t.Helper()
	store, err := storepkg.OpenStore(dsn, append([]storepkg.Option{storepkg.MaxOpenConns(4)}, opts...)...)
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

func openRawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close raw database: %v", err)
		}
	})
	return db
}

func gooseVersion(t *testing.T, dsn string) int64 {
	t.Helper()
	var v int64
	if err := openRawDB(t, dsn).QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	return v
}

func latestMigration(t *testing.T) int64 {
	t.Helper()
	paths, err := fs.Glob(storepkg.Migrations(), "migrations/*.sql")
	if err != nil || len(paths) == 0 {
		t.Fatalf("list migrations: %v (found %d)", err, len(paths))
	}
	var latest int64
	for _, path := range paths {
		prefix, _, _ := strings.Cut(filepath.Base(path), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("migration %s has no numeric prefix: %v", path, err)
		}
		latest = max(latest, version)
	}
	return latest
}

func TestOpenStore(t *testing.T) {
	t.Parallel()

	t.Run("does not migrate a cloned database", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		if got, want := gooseVersion(t, dsn), latestMigration(t); got != want {
			t.Fatalf("cloned goose version = %d, want %d", got, want)
		}
		raw := openRawDB(t, dsn)
		if _, err := raw.ExecContext(t.Context(), "DROP TABLE goose_db_version"); err != nil {
			t.Fatalf("DROP TABLE goose_db_version: %v", err)
		}
		store := openStoreAt(t, dsn)
		if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		}); err != nil {
			t.Fatalf("UpsertTickets on the cloned schema: %v", err)
		}
		var exists bool
		err := raw.QueryRowContext(t.Context(), "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists)
		if err != nil {
			t.Fatalf("QueryRowContext(to_regclass).Scan: %v", err)
		}
		if exists {
			t.Error("OpenStore(cloned) restored goose_db_version, want no migration")
		}
	})

	t.Run("migrates an unmigrated database", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.UnmigratedDSN(t)
		openStoreAt(t, dsn)
		if got, want := gooseVersion(t, dsn), latestMigration(t); got != want {
			t.Errorf("OpenStore(unmigrated) goose version = %d, want %d", got, want)
		}
	})

	t.Run("concurrent opens migrate once", func(t *testing.T) {
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
				t.Errorf("concurrent OpenStore() error = %v", err)
			}
		}
		if got, want := gooseVersion(t, dsn), latestMigration(t); got != want {
			t.Errorf("goose version = %d, want %d", got, want)
		}
	})

	t.Run("opening twice is a no-op", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		first, err := storepkg.OpenStore(dsn, storepkg.MaxOpenConns(4))
		if err != nil {
			t.Fatalf("OpenStore(first): %v", err)
		}
		applied := gooseVersion(t, dsn)
		if err := first.UpsertTickets(t.Context(), []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		}); err != nil {
			t.Fatalf("first.UpsertTickets: %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("first.Close: %v", err)
		}

		second := openStoreAt(t, dsn)
		if got := gooseVersion(t, dsn); got != applied {
			t.Errorf("goose version after the second open = %d, want %d unchanged", got, applied)
		}
		if got := readTickets(t, second); len(got) != 1 {
			t.Errorf("Tickets() after the second open = %+v, want the first open's row", got)
		}
	})

	t.Run("applies the DSN's statement timeout", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("url.Parse(test DSN): %v", err)
		}
		q := u.Query()
		q.Set("statement_timeout", "100")
		u.RawQuery = q.Encode()
		store := openStoreAt(t, u.String())

		lock, err := openRawDB(t, dsn).BeginTx(context.WithoutCancel(t.Context()), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		t.Cleanup(func() {
			if err := lock.Rollback(); err != nil {
				t.Errorf("roll back the lock: %v", err)
			}
		})
		if _, err := lock.ExecContext(t.Context(), "LOCK TABLE tickets IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatalf("LOCK TABLE tickets: %v", err)
		}

		_, err = store.Tickets(t.Context())
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "57014" {
			t.Errorf("Tickets() behind a held lock error = %v, want query_canceled (57014)", err)
		}
	})

	t.Run("an unreachable database is refused", func(t *testing.T) {
		t.Parallel()
		_, err := storepkg.OpenStore("postgres://cc@127.0.0.1:1/cc?sslmode=disable")
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Errorf("OpenStore(dead address) error = %v, want ECONNREFUSED", err)
		}
	})
}

func TestStore_UpsertTickets(t *testing.T) {
	t.Parallel()

	t.Run("is idempotent on the ticket url", func(t *testing.T) {
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
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-renamed", BlockedBy: []string{}},
			{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		}
		if err := store.UpsertTickets(ctx, edited); err != nil {
			t.Fatalf("UpsertTickets again: %v", err)
		}
		if diff := cmp.Diff(edited, readTickets(t, store)); diff != "" {
			t.Errorf("Tickets() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no blockers is stored as an empty array", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		store := openStoreAt(t, dsn)
		if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		var raw string
		err := openRawDB(t, dsn).QueryRow(`SELECT blocked_by::text FROM tickets WHERE url = $1`, "sandbox://CC-1").Scan(&raw)
		if err != nil {
			t.Fatalf("read blocked_by: %v", err)
		}
		if raw != "[]" {
			t.Errorf("tickets.blocked_by = %q, want the empty array literal, not JSON null", raw)
		}
	})
}

func TestStore_WithdrawTicket(t *testing.T) {
	t.Parallel()

	t.Run("hides the ticket but keeps its history", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		at := testNow
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
		if err := store.RecordPush(ctx, storepkg.Push{
			Ticket: plan.TicketURL(ticket.URL), Tip: "deadbeef", BaseSHA: "cafebabe", BaseBranch: "main", At: at,
		}); err != nil {
			t.Fatalf("RecordPush: %v", err)
		}

		withdrawnRuns, err := store.WithdrawTicket(ctx, ticket.URL, at.Add(time.Hour), false)
		if err != nil {
			t.Fatalf("WithdrawTicket: %v", err)
		}
		if diff := cmp.Diff([]plan.RunID{plan.RunID(runID)}, withdrawnRuns); diff != "" {
			t.Errorf("WithdrawTicket() run ids mismatch (-want +got):\n%s", diff)
		}

		if got := readTickets(t, store); len(got) != 0 {
			t.Errorf("Tickets() after WithdrawTicket = %+v, want none", got)
		}
		if diff := cmp.Diff([]int64{runID}, runIDsFor(t, store, ticket.URL)); diff != "" {
			t.Errorf("RunIDsForTicket() after WithdrawTicket mismatch (-want +got):\n%s", diff)
		}
		tips, err := store.LastPushedTips(ctx)
		if err != nil {
			t.Fatalf("LastPushedTips: %v", err)
		}
		if got := tips[plan.TicketURL(ticket.URL)]; got != "deadbeef" {
			t.Errorf("LastPushedTips()[CC-1] after WithdrawTicket = %q, want deadbeef", got)
		}
		memberships, err := store.LaunchMemberships(ctx)
		if err != nil {
			t.Fatalf("LaunchMemberships: %v", err)
		}
		if _, ok := memberships[plan.TicketURL(ticket.URL)]; !ok {
			t.Errorf("LaunchMemberships() after WithdrawTicket = %+v, want CC-1's membership kept", memberships)
		}
	})

	t.Run("leaves events alone", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		for _, e := range []storepkg.Event{
			{At: testNow, TicketURL: ticket.URL, Kind: storepkg.EventPushed},
			{At: testNow, Kind: storepkg.EventTickError},
		} {
			if err := store.AppendEvent(ctx, e); err != nil {
				t.Fatalf("AppendEvent(%s): %v", e.Kind, err)
			}
		}
		if _, err := store.WithdrawTicket(ctx, ticket.URL, testNow.Add(time.Hour), false); err != nil {
			t.Fatalf("WithdrawTicket: %v", err)
		}
		events, err := store.Events(ctx)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("Events() after WithdrawTicket = %+v, want both events kept", events)
		}
	})
}

func TestInTx(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	boom := errors.New("boom")

	err := s.CreateProjectThenCancel(ctx, cancel, boom)
	if !errors.Is(err, boom) || errors.Is(err, context.Canceled) {
		t.Errorf("inTx(cancelled mid fn) error = %v, want boom alone", err)
	}
	if got := readProjects(t, s); len(got) != 0 {
		t.Errorf("Projects() after a cancelled transaction = %+v, want the insert rolled back", got)
	}
}

func TestStore_HasEvent(t *testing.T) {
	t.Parallel()
	dsn := cctest.DSN(t)
	st := openStoreAt(t, dsn)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	storetest.Tickets(t, st, ticket)
	at := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	obs := plan.NewObservation(at)
	obs.PRs[ticket.Ref()] = plan.PR{Number: 41, State: plan.Merged, MergedAt: at}
	lp := loop.New(loop.Deps{
		Logger: slog.New(slog.DiscardHandler), Store: st, Clock: loop.RealClock{}, Runner: runner.ProcessRunner{},
		Observe: func(context.Context) (plan.Observation, error) { return obs, nil },
		Config:  config.Config{}, Workspace: config.Workspace{},
	})
	merges := func(t *testing.T) int {
		t.Helper()
		n := 0
		for _, event := range readEvents(t, st) {
			if event.Kind == storepkg.EventPRMerged && event.TicketURL == ticket.URL {
				n++
			}
		}
		return n
	}

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if got := merges(t); got != 1 {
		t.Fatalf("pr_merged events after the first tick = %d, want 1", got)
	}
	if _, err := openRawDB(t, dsn).ExecContext(t.Context(), "DELETE FROM meta"); err != nil {
		t.Fatalf("DELETE FROM meta: %v", err)
	}
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce after clearing meta: %v", err)
	}
	if got := merges(t); got != 1 {
		t.Errorf("pr_merged events after clearing meta = %d, want the merge fact to survive with 1", got)
	}
}

func TestStore_TicketEventsAfter(t *testing.T) {
	t.Parallel()

	t.Run("returns only this ticket's newer events", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		if err := store.UpsertTickets(ctx, []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
			{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2"},
		}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		at := testNow
		for _, e := range []storepkg.Event{
			{At: at, TicketURL: "sandbox://CC-1", Kind: storepkg.EventPushed},
			{At: at, TicketURL: "sandbox://CC-2", Kind: storepkg.EventPushFailed},
			{At: at.Add(-time.Hour), TicketURL: "sandbox://CC-1", Kind: storepkg.EventRefreshed, Detail: "late write, old time"},
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
		if len(all) != 2 {
			t.Fatalf("TicketEventsAfter(CC-1, 0) = %+v, want two events", all)
		}
		want := []storepkg.Event{
			{ID: all[0].ID, At: at, TicketURL: "sandbox://CC-1", Kind: storepkg.EventPushed},
			{
				ID: all[1].ID, At: at.Add(-time.Hour), TicketURL: "sandbox://CC-1",
				Kind: storepkg.EventRefreshed, Detail: "late write, old time",
			},
		}
		if diff := cmp.Diff(want, all); diff != "" {
			t.Errorf("TicketEventsAfter(CC-1, 0) mismatch (-want +got):\n%s", diff)
		}

		newer, err := store.TicketEventsAfter(ctx, "sandbox://CC-1", all[0].ID, 100)
		if err != nil {
			t.Fatalf("TicketEventsAfter: %v", err)
		}
		if diff := cmp.Diff(want[1:], newer); diff != "" {
			t.Errorf("TicketEventsAfter(CC-1, %d) mismatch (-want +got):\n%s", all[0].ID, diff)
		}
	})

	t.Run("pages without gaps or repeats", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		const ticketURL = "sandbox://CC-1"
		tickets := []storepkg.Ticket{{URL: ticketURL, Repo: "cc-sandbox", Branch: "cc-1"}}
		if err := store.UpsertTickets(ctx, tickets); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		const pageSize = 3
		want := []storepkg.EventKind{
			storepkg.EventPushRefused, storepkg.EventPushFailed, storepkg.EventPushed,
			storepkg.EventRefreshed, storepkg.EventRestacked, storepkg.EventTickError, storepkg.EventPRMerged,
		}
		for _, kind := range want {
			if err := store.AppendEvent(ctx, storepkg.Event{At: testNow, TicketURL: ticketURL, Kind: kind}); err != nil {
				t.Fatalf("AppendEvent(%s): %v", kind, err)
			}
		}

		var got []storepkg.EventKind
		var afterID int64
		for {
			page, err := store.TicketEventsAfter(ctx, ticketURL, afterID, pageSize)
			if err != nil {
				t.Fatalf("TicketEventsAfter(%s, %d, %d): %v", ticketURL, afterID, pageSize, err)
			}
			if len(page) > pageSize {
				t.Fatalf("TicketEventsAfter(%s, %d, %d) = %d events, want at most %d",
					ticketURL, afterID, pageSize, len(page), pageSize)
			}
			for _, e := range page {
				got = append(got, e.Kind)
				afterID = e.ID
			}
			if len(page) < pageSize {
				break
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("paged TicketEventsAfter kinds mismatch (-want +got):\n%s", diff)
		}
	})
}
