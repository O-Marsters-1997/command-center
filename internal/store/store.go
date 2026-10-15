package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	maxOpenConns     = 10
	maxIdleConns     = 5
	connMaxLifetime  = 30 * time.Minute
	pingTimeout      = 5 * time.Second
	openTimeout      = 2 * time.Minute
	statementTimeout = 30 * time.Second
)

func isUniqueViolation(err error, constraint string) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

type Store struct {
	db *sql.DB
	q  *ccdb.Queries
}

type Option func(*sql.DB)

// MaxOpenConns sets the store pool's concurrent connection limit.
func MaxOpenConns(n int) Option { return func(db *sql.DB) { db.SetMaxOpenConns(n) } }

// Migrations exposes the embedded migrations used to key test database templates.
func Migrations() fs.FS { return migrations }

// OpenStore connects and migrates the database unless opened from a prepared test template.
func OpenStore(dsn string, opts ...Option) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if !strings.HasPrefix(cfg.Database, "cc_test_") || cfg.RuntimeParams["application_name"] != "cc-cctest-clone" {
		migrateDB := stdlib.OpenDB(*cfg)
		if err := migrate(ctx, migrateDB); err != nil {
			return nil, errors.Join(err, migrateDB.Close())
		}
		if err := migrateDB.Close(); err != nil {
			return nil, fmt.Errorf("close migration connection: %w", err)
		}
	}

	if _, ok := cfg.RuntimeParams["statement_timeout"]; !ok {
		cfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(statementTimeout.Milliseconds(), 10)
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	for _, opt := range opts {
		opt(db)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping pool: %w", err), db.Close())
	}
	return &Store{db: db, q: ccdb.New(db)}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration locker: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// UpsertTickets seeds entire ticket rows directly; production imports through ImportTickets.
func (s *Store) UpsertTickets(ctx context.Context, tickets []Ticket) error {
	rows, err := rowsOf(tickets)
	if err != nil {
		return err
	}
	encoded, err := encodeJSON("tickets", lastPerURL(rows))
	if err != nil {
		return err
	}
	if err := s.q.UpsertTickets(ctx, encoded); err != nil {
		return fmt.Errorf("upsert %d tickets: %w", len(tickets), err)
	}
	return nil
}

func rowsOf(tickets []Ticket) ([]ticketRow, error) {
	rows := make([]ticketRow, 0, len(tickets))
	for _, t := range tickets {
		blockedBy, err := encodeBlockedBy(t.BlockedBy)
		if err != nil {
			return nil, fmt.Errorf("encode blocked_by for %s: %w", t.URL, err)
		}
		row := ticketRow{
			URL: t.URL, Repo: t.Repo, Branch: t.Branch, BlockedBy: blockedBy, Source: t.Source,
			Title: t.Title, Body: t.Body, Status: t.Status, Feature: t.Feature,
		}
		if !t.SyncedAt.IsZero() {
			syncedAt := t.SyncedAt.UTC()
			row.SyncedAt = &syncedAt
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type ticketRow struct {
	URL       string          `json:"url"`
	Repo      string          `json:"repo"`
	Branch    string          `json:"branch"`
	BlockedBy json.RawMessage `json:"blocked_by"`
	Source    string          `json:"source"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Status    string          `json:"status"`
	Feature   string          `json:"feature"`
	SyncedAt  *time.Time      `json:"synced_at"`
}

func lastPerURL(rows []ticketRow) []ticketRow {
	at := make(map[string]int, len(rows))
	unique := make([]ticketRow, 0, len(rows))
	for _, row := range rows {
		if i, seen := at[row.URL]; seen {
			unique[i] = row
			continue
		}
		at[row.URL] = len(unique)
		unique = append(unique, row)
	}
	return unique
}

type blockedByUpdate struct {
	URL       string          `json:"url"`
	BlockedBy json.RawMessage `json:"blocked_by"`
}

func encodeJSON(what string, v any) (json.RawMessage, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", what, err)
	}
	return encoded, nil
}

func (s *Store) inTx(ctx context.Context, fn func(q *ccdb.Queries) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	tx, err := s.db.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(s.q.WithTx(tx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Tickets excludes withdrawn rows and orders by URL.
func (s *Store) Tickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.q.ListTickets(ctx)
	if err != nil {
		return nil, fmt.Errorf("select tickets: %w", err)
	}

	var tickets []Ticket
	for _, row := range rows {
		t := Ticket{
			URL:      row.URL,
			Repo:     row.Repo,
			Branch:   row.Branch,
			Source:   row.Source,
			Title:    row.Title,
			Body:     row.Body,
			Status:   row.Status,
			Feature:  row.Feature,
			SyncedAt: row.SyncedAt.Time,
		}
		blockedBy, err := decodeBlockedBy(row.BlockedBy)
		if err != nil {
			return nil, fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
		}
		t.BlockedBy = blockedBy
		tickets = append(tickets, t)
	}
	return tickets, nil
}

// FeatureConflictError reports a ticket stored under a different feature than its import.
type FeatureConflictError struct {
	URL, Existing, Importing string
}

func (e *FeatureConflictError) Error() string {
	return fmt.Sprintf("ticket %s already belongs to feature %q, refusing to import it into %q",
		e.URL, e.Existing, e.Importing)
}

func (e *FeatureConflictError) refusedTicket() string { return e.URL }

// ImportTickets refreshes tracker rows, preserving seeded branch and blockers while pruning merged blockers.
func (s *Store) ImportTickets(
	ctx context.Context, feature string, tickets []ImportedTicket, now time.Time,
) error {
	return s.inTx(ctx, func(qtx *ccdb.Queries) (err error) {
		syncedAt := now.UTC()
		returned := make(map[string]bool, len(tickets))
		urls := make([]string, 0, len(tickets))
		for _, t := range tickets {
			urls = append(urls, t.URL)
		}
		encodedURLs, err := encodeJSON("ticket urls", urls)
		if err != nil {
			return err
		}
		existingRows, err := qtx.ListActiveTicketFeatures(ctx, encodedURLs)
		if err != nil {
			return fmt.Errorf("read features of imported tickets: %w", err)
		}
		existing := make(map[string]string, len(existingRows))
		for _, row := range existingRows {
			existing[row.URL] = row.Feature
		}

		imported := make([]Ticket, 0, len(tickets))
		for _, t := range tickets {
			if prior := existing[t.URL]; prior != "" && prior != feature {
				return &FeatureConflictError{URL: t.URL, Existing: prior, Importing: feature}
			}

			returned[t.URL] = true
			imported = append(imported, Ticket{
				URL: t.URL, Repo: t.Repo, Source: t.Source, Feature: feature, Title: t.Title, Body: t.Body,
				Status: t.Status, SyncedAt: syncedAt, Branch: tracker.BranchSlug(t.Number, t.Title),
				BlockedBy: t.BlockedBy,
			})
		}
		rows, err := rowsOf(imported)
		if err != nil {
			return err
		}
		encodedRows, err := encodeJSON("imported tickets", lastPerURL(rows))
		if err != nil {
			return err
		}
		if err = qtx.ImportTickets(ctx, encodedRows); err != nil {
			return fmt.Errorf("import %d tickets: %w", len(rows), err)
		}

		previous, err := qtx.ListTicketsInFeature(ctx, feature)
		if err != nil {
			return fmt.Errorf("list existing tickets for %s: %w", feature, err)
		}
		obs, _, err := lastObservation(ctx, qtx)
		if err != nil {
			return fmt.Errorf("read observation for blocker repair: %w", err)
		}

		intended := make([]ticketBlockedBy, 0, len(returned))
		for _, row := range previous {
			if !returned[row.URL] {
				continue
			}
			var blockedBy []string
			blockedBy, err = decodeBlockedBy(row.BlockedBy)
			if err != nil {
				return fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
			}
			intended = append(intended, ticketBlockedBy{URL: row.URL, BlockedBy: blockedBy})
		}
		featureOf, isMerged, err := closureLookups(ctx, qtx, intended, obs)
		if err != nil {
			return err
		}
		violation, err := closeUnderBlockedBy(feature, intended, featureOf, isMerged)
		if err != nil {
			return err
		}
		if violation != nil {
			return violation
		}

		dropped := []string{}
		for _, t := range previous {
			if !returned[t.URL] {
				dropped = append(dropped, t.URL)
			}
		}
		encodedDropped, err := encodeJSON("withdrawn urls", dropped)
		if err != nil {
			return err
		}
		withdrawn, err := qtx.WithdrawTickets(ctx, ccdb.WithdrawTicketsParams{
			WithdrawnAt: notNullTime(now), Urls: encodedDropped,
		})
		if err != nil {
			return fmt.Errorf("withdraw %d tickets: %w", len(dropped), err)
		}
		if len(dropped) > 0 && withdrawn == 0 {
			return fmt.Errorf("withdraw %d tickets: %w", len(dropped), ErrNotFound)
		}

		withdrawnRows, err := qtx.ListWithdrawnTickets(ctx)
		if err != nil {
			return fmt.Errorf("list withdrawn tickets for blocker repair: %w", err)
		}
		mergedWithdrawn := make(map[string]bool)
		for _, row := range withdrawnRows {
			if obs.PRs[plan.BranchRef{Repo: row.Repo, Branch: row.Branch}].State == plan.Merged {
				mergedWithdrawn[row.URL] = true
			}
		}
		return repairBlockedBy(ctx, qtx, mergedWithdrawn)
	})
}

func repairBlockedBy(ctx context.Context, qtx *ccdb.Queries, withdrawn map[string]bool) error {
	if len(withdrawn) == 0 {
		return nil
	}
	rows, err := qtx.ListTicketsWithBlockers(ctx)
	if err != nil {
		return fmt.Errorf("list tickets for blocker repair: %w", err)
	}
	updates := []blockedByUpdate{}
	for _, row := range rows {
		blockedBy, err := decodeBlockedBy(row.BlockedBy)
		if err != nil {
			return fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
		}
		pruned := make([]string, 0, len(blockedBy))
		changed := false
		for _, blocker := range blockedBy {
			if withdrawn[blocker] {
				changed = true
				continue
			}
			pruned = append(pruned, blocker)
		}
		if !changed {
			continue
		}
		blob, err := encodeBlockedBy(pruned)
		if err != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", row.URL, err)
		}
		updates = append(updates, blockedByUpdate{URL: row.URL, BlockedBy: blob})
	}
	if len(updates) == 0 {
		return nil
	}
	encoded, err := encodeJSON("blocked_by repairs", updates)
	if err != nil {
		return err
	}
	n, err := qtx.SetBlockedBys(ctx, encoded)
	return matchedRow(n, err, fmt.Sprintf("repair blocked_by of %d tickets", len(updates)))
}

// WithdrawTicket retains historical references and prunes merged blockers from other tickets.
func (s *Store) WithdrawTicket(ctx context.Context, url string, now time.Time, merged bool) error {
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		n, err := q.WithdrawTicket(ctx, ccdb.WithdrawTicketParams{WithdrawnAt: notNullTime(now), URL: url})
		if err := matchedRow(n, err, "withdraw ticket "+url); err != nil {
			return err
		}
		if merged {
			return repairBlockedBy(ctx, q, map[string]bool{url: true})
		}
		return nil
	})
}

func matchedRow(n int64, err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

func encodeBlockedBy(urls []string) ([]byte, error) {
	blob, err := json.Marshal(nonNil(urls))
	if err != nil {
		return nil, fmt.Errorf("marshal blocked_by: %w", err)
	}
	return blob, nil
}

func decodeBlockedBy(raw []byte) ([]string, error) {
	var urls []string
	if err := json.Unmarshal(raw, &urls); err != nil {
		return nil, fmt.Errorf("unmarshal blocked_by: %w", err)
	}
	return urls, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func notNull(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func notNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func nullOption[T, N any](v option.Option[T], encode func(T) N) N {
	if v.IsSome() {
		return encode(v.Unwrap())
	}
	var zero N
	return zero
}

const (
	metaObservation   = "observation"
	metaLastError     = "last_error"
	metaImportError   = "import_error"
	metaCheckingTicks = "checking_ticks"
	metaLastVerdicts  = "last_verdicts"
)

// CheckingTicks counts successful ticks while each ticket awaits a CI verdict, not wall time.
func (s *Store) CheckingTicks(ctx context.Context) (map[plan.TicketURL]int, error) {
	ticks := map[plan.TicketURL]int{}
	if _, err := s.getMeta(ctx, metaCheckingTicks, &ticks); err != nil {
		return nil, fmt.Errorf("read %s meta: %w", metaCheckingTicks, err)
	}
	return ticks, nil
}

func (s *Store) IncrementCheckingTicks(ctx context.Context, ticketURLs []plan.TicketURL) error {
	ticks, err := s.CheckingTicks(ctx)
	if err != nil {
		return fmt.Errorf("read checking ticks: %w", err)
	}
	for _, url := range ticketURLs {
		ticks[url]++
	}
	if err := s.putMeta(ctx, metaCheckingTicks, ticks); err != nil {
		return fmt.Errorf("write %s meta: %w", metaCheckingTicks, err)
	}
	return nil
}

func (s *Store) ResetCheckingTicks(ctx context.Context, ticketID plan.TicketURL) error {
	ticks, err := s.CheckingTicks(ctx)
	if err != nil {
		return fmt.Errorf("read checking ticks: %w", err)
	}
	if _, ok := ticks[ticketID]; !ok {
		return nil
	}
	delete(ticks, ticketID)
	if err := s.putMeta(ctx, metaCheckingTicks, ticks); err != nil {
		return fmt.Errorf("write %s meta: %w", metaCheckingTicks, err)
	}
	return nil
}

// LastVerdicts returns the latest recorded CI verdict per ticket URL.
func (s *Store) LastVerdicts(ctx context.Context) (map[string]string, error) {
	verdicts := map[string]string{}
	if _, err := s.getMeta(ctx, metaLastVerdicts, &verdicts); err != nil {
		return nil, fmt.Errorf("read %s meta: %w", metaLastVerdicts, err)
	}
	return verdicts, nil
}

func (s *Store) SaveLastVerdicts(ctx context.Context, verdicts map[string]string) error {
	if err := s.putMeta(ctx, metaLastVerdicts, verdicts); err != nil {
		return fmt.Errorf("write %s meta: %w", metaLastVerdicts, err)
	}
	return nil
}

// SaveObservation replaces the persisted observation after a successful tick.
func (s *Store) SaveObservation(ctx context.Context, obs plan.Observation) error {
	if err := s.putMeta(ctx, metaObservation, obs); err != nil {
		return fmt.Errorf("write %s meta: %w", metaObservation, err)
	}
	return nil
}

func (s *Store) LastObservation(ctx context.Context) (plan.Observation, bool, error) {
	return lastObservation(ctx, s.q)
}

func lastObservation(ctx context.Context, q *ccdb.Queries) (plan.Observation, bool, error) {
	var obs plan.Observation
	found, err := getMeta(ctx, q, metaObservation, &obs)
	if err != nil {
		return obs, found, fmt.Errorf("read %s meta: %w", metaObservation, err)
	}
	return obs, found, nil
}

func (s *Store) RecordTickError(ctx context.Context, tickErr TickError) error {
	if err := s.putMeta(ctx, metaLastError, tickErr); err != nil {
		return fmt.Errorf("write %s meta: %w", metaLastError, err)
	}
	if err := s.AppendEvent(ctx, Event{At: tickErr.At, Kind: EventTickError, Detail: tickErr.Message}); err != nil {
		return fmt.Errorf("append tick error event: %w", err)
	}
	return nil
}

// LastError retains the last tick failure even after a successful tick.
func (s *Store) LastError(ctx context.Context) (option.Option[TickError], error) {
	var tickErr TickError
	found, err := s.getMeta(ctx, metaLastError, &tickErr)
	if err != nil {
		return option.None[TickError](), fmt.Errorf("read %s meta: %w", metaLastError, err)
	}
	if !found {
		return option.None[TickError](), nil
	}
	return option.Some(tickErr), nil
}

// ImportError holds the last feature-conflict import refusal.
type ImportError struct {
	At      time.Time `json:"at"`
	Feature string    `json:"feature"`
	Message string    `json:"message"`
}

type importRefusal interface {
	error
	refusedTicket() string
}

// RecordImportRefusal persists the last import refusal and appends an audit event.
func (s *Store) RecordImportRefusal(ctx context.Context, feature string, refusal importRefusal, now time.Time) error {
	importErr := ImportError{At: now, Feature: feature, Message: refusal.Error()}
	if err := s.putMeta(ctx, metaImportError, importErr); err != nil {
		return fmt.Errorf("write %s meta: %w", metaImportError, err)
	}
	err := s.AppendEvent(ctx, Event{
		At: now, TicketURL: refusal.refusedTicket(), Kind: EventImportRefused, Detail: refusal.Error(),
	})
	if err != nil {
		return fmt.Errorf("append import refusal event: %w", err)
	}
	return nil
}

// LastImportError retains the last import refusal even after a successful import.
func (s *Store) LastImportError(ctx context.Context) (ImportError, bool, error) {
	var importErr ImportError
	found, err := s.getMeta(ctx, metaImportError, &importErr)
	if err != nil {
		return importErr, found, fmt.Errorf("read %s meta: %w", metaImportError, err)
	}
	return importErr, found, nil
}

// Event holds an append-only audit row.
type Event struct {
	ID        int64
	At        time.Time
	TicketURL string
	Kind      EventKind
	Detail    string
}

func (s *Store) AppendEvent(ctx context.Context, e Event) error {
	err := s.q.AppendEvent(ctx, ccdb.AppendEventParams{
		At:       e.At.UTC(),
		TicketID: sql.NullString{String: e.TicketURL, Valid: e.TicketURL != ""},
		Kind:     string(e.Kind),
		Detail:   e.Detail,
	})
	if err != nil {
		return fmt.Errorf("append event %s: %w", e.Kind, err)
	}
	return nil
}

func (s *Store) HasEvent(ctx context.Context, ticketURL string, kind EventKind) (bool, error) {
	found, err := s.q.HasEvent(ctx, ccdb.HasEventParams{
		TicketID: sql.NullString{String: ticketURL, Valid: ticketURL != ""},
		Kind:     string(kind),
	})
	if err != nil {
		return false, fmt.Errorf("has event %s for %s: %w", kind, ticketURL, err)
	}
	return found, nil
}

const maxEvents = 5000

func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.q.ListEvents(ctx, maxEvents)
	if err != nil {
		return nil, fmt.Errorf("select events: %w", err)
	}

	var events []Event
	for _, row := range rows {
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind of %s: %w", row.TicketID.String, err)
		}
		e := Event{At: row.At, Kind: kind}
		e.TicketURL, e.Detail = row.TicketID.String, row.Detail
		events = append(events, e)
	}
	return events, nil
}

func (s *Store) TicketEventsAfter(ctx context.Context, ticketURL string, afterID int64, limit int) ([]Event, error) {
	rows, err := s.q.ListTicketEventsAfter(ctx, ccdb.ListTicketEventsAfterParams{
		TicketID: sql.NullString{String: ticketURL, Valid: ticketURL != ""},
		AfterID:  afterID,
		MaxRows:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("select events for %s: %w", ticketURL, err)
	}

	var events []Event
	for _, row := range rows {
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind of event %d: %w", row.ID, err)
		}
		events = append(events, Event{
			ID: row.ID, At: row.At, TicketURL: row.TicketID.String, Kind: kind, Detail: row.Detail,
		})
	}
	return events, nil
}

func (s *Store) putMeta(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	if err := s.q.PutMeta(ctx, ccdb.PutMetaParams{Key: key, Value: encoded}); err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	return nil
}

func (s *Store) getMeta(ctx context.Context, key string, into any) (bool, error) {
	return getMeta(ctx, s.q, key, into)
}

func getMeta(ctx context.Context, q *ccdb.Queries, key string, into any) (bool, error) {
	encoded, err := q.GetMeta(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	if err := json.Unmarshal(encoded, into); err != nil {
		return false, fmt.Errorf("decode %s: %w", key, err)
	}
	return true, nil
}
