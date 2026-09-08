package cc

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // database/sql driver "sqlite", pure Go

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the SQLite database. Only the loop goroutine writes it (inv. 9).
type Store struct {
	db *sql.DB
}

// OpenStore opens (creating if needed) the database at path and migrates it up to the latest
// embedded migration.
func OpenStore(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return store, nil
}

var (
	gooseOnce     sync.Once
	gooseSetupErr error
)

// setUpGoose configures goose once per process. goose keeps its base FS, logger and dialect in
// package-level globals, so setting them per OpenStore races between two concurrent opens.
func setUpGoose() error {
	gooseOnce.Do(func() {
		goose.SetBaseFS(migrations)
		goose.SetLogger(goose.NopLogger())
		gooseSetupErr = goose.SetDialect("sqlite3")
	})
	return gooseSetupErr
}

func (s *Store) init(ctx context.Context) error {
	if err := setUpGoose(); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, s.db, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// UpsertTickets writes a ticket row directly, every column at once, keyed on url. Production no
// longer calls this itself -- ImportTickets is the tracker's own write path -- but it stays as
// the direct-write counterpart tests use to seed a fixture with an exact row.
func (s *Store) UpsertTickets(ctx context.Context, tickets []Ticket) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	for _, t := range tickets {
		blockedBy, marshalErr := json.Marshal(nonNil(t.BlockedBy))
		if marshalErr != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", t.URL, marshalErr)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tickets (url, repo, branch, blocked_by, source, title, body, status, group_key, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (url) DO UPDATE SET
				repo = excluded.repo, branch = excluded.branch, blocked_by = excluded.blocked_by,
				source = excluded.source, title = excluded.title, body = excluded.body,
				status = excluded.status, group_key = excluded.group_key, synced_at = excluded.synced_at`,
			t.URL, t.Repo, t.Branch, string(blockedBy), t.Source, t.Title, t.Body, t.Status, t.GroupKey, t.SyncedAt)
		if err != nil {
			return fmt.Errorf("upsert ticket %s: %w", t.URL, err)
		}
	}
	return tx.Commit()
}

// Tickets returns every ticket row, ordered by url. The tracker-owned columns read as empty
// strings rather than NULL for a row from before 0003_ticket_fields.sql added them.
func (s *Store) Tickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT url, repo, branch, blocked_by,
		       COALESCE(source, ''), COALESCE(title, ''), COALESCE(body, ''),
		       COALESCE(status, ''), COALESCE(group_key, ''), COALESCE(synced_at, '')
		FROM tickets ORDER BY url`)
	if err != nil {
		return nil, fmt.Errorf("select tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tickets []Ticket
	for rows.Next() {
		var t Ticket
		var blockedBy string
		if err := rows.Scan(&t.URL, &t.Repo, &t.Branch, &blockedBy,
			&t.Source, &t.Title, &t.Body, &t.Status, &t.GroupKey, &t.SyncedAt); err != nil {
			return nil, fmt.Errorf("scan ticket: %w", err)
		}
		if err := json.Unmarshal([]byte(blockedBy), &t.BlockedBy); err != nil {
			return nil, fmt.Errorf("decode blocked_by for %s: %w", t.URL, err)
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tickets: %w", err)
	}
	return tickets, nil
}

// ImportTickets upserts one group's tracker tickets, keyed on url. Every tracker-owned column
// refreshes on each call; branch and blocked_by are seeded only the first time a url is imported
// and left alone after that -- they're the app's own, not the tracker's.
func (s *Store) ImportTickets(ctx context.Context, group string, tickets []ImportedTicket, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	syncedAt := now.UTC().Format(time.RFC3339Nano)
	for _, t := range tickets {
		blockedBy, marshalErr := json.Marshal(nonNil(t.BlockedBy))
		if marshalErr != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", t.URL, marshalErr)
		}
		// ponytail: source is hardcoded to "github" because tracker.Source names no other
		// tracker today; derive it from the resolved Source once a second one exists.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tickets (url, repo, source, group_key, title, body, status, synced_at, branch, blocked_by)
			VALUES (?, ?, 'github', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (url) DO UPDATE SET
				repo = excluded.repo, source = excluded.source, group_key = excluded.group_key,
				title = excluded.title, body = excluded.body, status = excluded.status,
				synced_at = excluded.synced_at`,
			t.URL, t.Repo, group, t.Title, t.Body, t.Status, syncedAt,
			tracker.BranchSlug(t.Number, t.Title), string(blockedBy))
		if err != nil {
			return fmt.Errorf("import ticket %s: %w", t.URL, err)
		}
	}
	return tx.Commit()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

const (
	metaObservation   = "observation"
	metaLastError     = "last_error"
	metaCheckingTicks = "checking_ticks"
	metaLastVerdicts  = "last_verdicts"
)

// CheckingTicks returns each ticket's count of successful ticks since it last had anything to
// resolve into a CI verdict. verdict.Input.Now is derived from this, never wall clock, so a
// GitHub outage cannot walk every in-flight row to needs_you the moment it ends
// (docs/designs/command-centre-design.md § 11 inv. 11).
func (s *Store) CheckingTicks(ctx context.Context) (map[string]int, error) {
	ticks := map[string]int{}
	if _, err := s.getMeta(ctx, metaCheckingTicks, &ticks); err != nil {
		return nil, err
	}
	return ticks, nil
}

// IncrementCheckingTicks bumps every named ticket's counter by one -- called once per successful
// tick, never on a failed observe, which is what makes the counter track successful ticks,
// not wall time.
func (s *Store) IncrementCheckingTicks(ctx context.Context, ticketURLs []string) error {
	ticks, err := s.CheckingTicks(ctx)
	if err != nil {
		return err
	}
	for _, url := range ticketURLs {
		ticks[url]++
	}
	return s.putMeta(ctx, metaCheckingTicks, ticks)
}

// resetCheckingTicks zeroes one ticket's counter -- called by RecordPush (pushes.go) on every
// fresh push, so a re-run's second push starts its own bounded wait rather than inheriting the
// first push's.
func (s *Store) resetCheckingTicks(ctx context.Context, ticketID string) error {
	ticks, err := s.CheckingTicks(ctx)
	if err != nil {
		return err
	}
	if _, ok := ticks[ticketID]; !ok {
		return nil
	}
	delete(ticks, ticketID)
	return s.putMeta(ctx, metaCheckingTicks, ticks)
}

// LastVerdicts returns each ticket's most recently recorded CI verdict label ("review_me",
// "needs_you" or "checking"), keyed by ticket URL -- what recordVerdictTransitions (loop.go)
// compares this tick's freshly computed verdict against before logging a transition event.
func (s *Store) LastVerdicts(ctx context.Context) (map[string]string, error) {
	verdicts := map[string]string{}
	if _, err := s.getMeta(ctx, metaLastVerdicts, &verdicts); err != nil {
		return nil, err
	}
	return verdicts, nil
}

func (s *Store) SaveLastVerdicts(ctx context.Context, verdicts map[string]string) error {
	return s.putMeta(ctx, metaLastVerdicts, verdicts)
}

// SaveObservation replaces the persisted observation. Only a successful tick calls it, which
// is what makes the page's observe age an honest inv. 10 signal.
func (s *Store) SaveObservation(ctx context.Context, obs Observation) error {
	return s.putMeta(ctx, metaObservation, obs)
}

func (s *Store) LastObservation(ctx context.Context) (Observation, bool, error) {
	var obs Observation
	found, err := s.getMeta(ctx, metaObservation, &obs)
	return obs, found, err
}

// RecordTickError stores the last tick failure and appends its audit row.
func (s *Store) RecordTickError(ctx context.Context, tickErr TickError) error {
	if err := s.putMeta(ctx, metaLastError, tickErr); err != nil {
		return err
	}
	return s.AppendEvent(ctx, Event{At: tickErr.At, Kind: "tick_error", Detail: tickErr.Message})
}

// LastError returns the last tick failure, if there has been one. It is not cleared by a
// later success: the page shows both ages.
func (s *Store) LastError(ctx context.Context) (TickError, bool, error) {
	var tickErr TickError
	found, err := s.getMeta(ctx, metaLastError, &tickErr)
	return tickErr, found, err
}

// Event is one append-only audit row.
type Event struct {
	At        time.Time
	TicketURL string
	Kind      string
	Detail    string
}

func (s *Store) AppendEvent(ctx context.Context, e Event) error {
	var ticketID any
	if e.TicketURL != "" {
		ticketID = e.TicketURL
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (at, ticket_id, kind, detail) VALUES (?, ?, ?, ?)`,
		e.At.UTC().Format(time.RFC3339Nano), ticketID, e.Kind, e.Detail)
	if err != nil {
		return fmt.Errorf("append event %s: %w", e.Kind, err)
	}
	return nil
}

// Events returns every audit row, oldest first.
func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, ticket_id, kind, detail FROM events ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		var e Event
		var at string
		var ticketID, detail sql.NullString
		if err := rows.Scan(&at, &ticketID, &e.Kind, &detail); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, fmt.Errorf("decode event time %q: %w", at, err)
		}
		e.TicketURL, e.Detail = ticketID.String, detail.String
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

func (s *Store) putMeta(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, string(encoded))
	if err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	return nil
}

func (s *Store) getMeta(ctx context.Context, key string, into any) (bool, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	if err := json.Unmarshal([]byte(encoded), into); err != nil {
		return false, fmt.Errorf("decode %s: %w", key, err)
	}
	return true, nil
}
