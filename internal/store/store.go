package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", pure Go
	"github.com/pressly/goose/v3"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
	q  *ccdb.Queries
}

// OpenStore connects to the database dsn names and migrates it up to the latest embedded
// migration.
func OpenStore(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	store := &Store{db: db, q: ccdb.New(db)}
	if err := store.init(context.Background()); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return store, nil
}

var (
	gooseOnce     sync.Once
	gooseSetupErr error
)

// goose keeps its base FS, logger and dialect in package-level globals, so setting them
// per OpenStore races between concurrent opens.
func setUpGoose() error {
	gooseOnce.Do(func() {
		goose.SetBaseFS(migrations)
		goose.SetLogger(goose.NopLogger())
		if err := goose.SetDialect("postgres"); err != nil {
			gooseSetupErr = fmt.Errorf("set goose dialect: %w", err)
		}
	})
	return gooseSetupErr
}

func (s *Store) init(ctx context.Context) error {
	if err := setUpGoose(); err != nil {
		return fmt.Errorf("set up goose: %w", err)
	}
	if err := goose.UpContext(ctx, s.db, "migrations"); err != nil {
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

// UpsertTickets writes a ticket row directly, every column at once, keyed on url.
// Tests use it to seed an exact row; production imports through ImportTickets.
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

	qtx := s.q.WithTx(tx)
	for _, t := range tickets {
		var blockedBy []byte
		blockedBy, err = encodeBlockedBy(t.BlockedBy)
		if err != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", t.URL, err)
		}
		err = qtx.UpsertTicket(ctx, ccdb.UpsertTicketParams{
			URL:       t.URL,
			Repo:      t.Repo,
			Branch:    t.Branch,
			BlockedBy: blockedBy,
			Source:    t.Source,
			Title:     t.Title,
			Body:      t.Body,
			Status:    t.Status,
			Feature:   t.Feature,
			SyncedAt:  t.SyncedAt,
		})
		if err != nil {
			return fmt.Errorf("upsert ticket %s: %w", t.URL, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit ticket upserts: %w", err)
	}
	return nil
}

// Tickets returns every ticket row not withdrawn, ordered by url.
func (s *Store) Tickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.q.Tickets(ctx)
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
			SyncedAt: row.SyncedAt,
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

// FeatureConflictError reports a ticket already stored under a different feature than the one
// being imported.
type FeatureConflictError struct {
	URL, Existing, Importing string
}

func (e *FeatureConflictError) Error() string {
	return fmt.Sprintf("ticket %s already belongs to feature %q, refusing to import it into %q",
		e.URL, e.Existing, e.Importing)
}

func (e *FeatureConflictError) refusedTicket() string { return e.URL }

// ImportTickets upserts one feature's tracker tickets, keyed on url, withdrawing and later
// restoring rows the tracker stops and resumes returning. branch and blocked_by are seeded
// once; a merged-and-withdrawn blocker is pruned from every other ticket's blocked_by.
func (s *Store) ImportTickets(
	ctx context.Context, feature string, tickets []ImportedTicket, now time.Time,
) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	qtx := s.q.WithTx(tx)
	syncedAt := now.UTC().Format(time.RFC3339)
	returned := make(map[string]bool, len(tickets))
	for _, t := range tickets {
		var existing string
		existing, err = qtx.TicketFeature(ctx, t.URL)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read feature for %s: %w", t.URL, err)
		}
		if existing != "" && existing != feature {
			return &FeatureConflictError{URL: t.URL, Existing: existing, Importing: feature}
		}

		returned[t.URL] = true
		var blockedBy []byte
		blockedBy, err = encodeBlockedBy(t.BlockedBy)
		if err != nil {
			return fmt.Errorf("encode blocked_by for %s: %w", t.URL, err)
		}
		err = qtx.ImportTicket(ctx, ccdb.ImportTicketParams{
			URL:       t.URL,
			Repo:      t.Repo,
			Source:    t.Source,
			Feature:   feature,
			Title:     t.Title,
			Body:      t.Body,
			Status:    t.Status,
			SyncedAt:  syncedAt,
			Branch:    tracker.BranchSlug(t.Number, t.Title),
			BlockedBy: blockedBy,
		})
		if err != nil {
			return fmt.Errorf("import ticket %s: %w", t.URL, err)
		}
	}

	previous, err := qtx.TicketsInFeature(ctx, feature)
	if err != nil {
		return fmt.Errorf("list existing tickets for %s: %w", feature, err)
	}
	obs, _, err := s.LastObservation(ctx)
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
	violation, err := closeUnderBlockedBy(
		feature, intended, ticketFeatureLookup(ctx, qtx), blockerMergedLookup(ctx, qtx, obs),
	)
	if err != nil {
		return fmt.Errorf("check feature closure for %s: %w", feature, err)
	}
	if violation != nil {
		return violation
	}

	for _, t := range previous {
		if returned[t.URL] {
			continue
		}
		if err = qtx.WithdrawTicket(ctx, ccdb.WithdrawTicketParams{WithdrawnAt: notNullTime(now), URL: t.URL}); err != nil {
			return fmt.Errorf("withdraw ticket %s: %w", t.URL, err)
		}
	}

	withdrawnRows, err := qtx.WithdrawnTickets(ctx)
	if err != nil {
		return fmt.Errorf("list withdrawn tickets for blocker repair: %w", err)
	}
	mergedWithdrawn := make(map[string]bool)
	for _, row := range withdrawnRows {
		if obs.PRs[plan.BranchRef{Repo: row.Repo, Branch: row.Branch}].State == plan.Merged {
			mergedWithdrawn[row.URL] = true
		}
	}
	if err = repairBlockedBy(ctx, qtx, mergedWithdrawn); err != nil {
		return fmt.Errorf("repair blocked_by after import of %s: %w", feature, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit import of %s: %w", feature, err)
	}
	return nil
}

func repairBlockedBy(ctx context.Context, qtx *ccdb.Queries, withdrawn map[string]bool) error {
	if len(withdrawn) == 0 {
		return nil
	}
	rows, err := qtx.TicketsWithBlockers(ctx)
	if err != nil {
		return fmt.Errorf("list tickets for blocker repair: %w", err)
	}
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
		if err := qtx.SetBlockedBy(ctx, ccdb.SetBlockedByParams{BlockedBy: blob, URL: row.URL}); err != nil {
			return fmt.Errorf("repair blocked_by for %s: %w", row.URL, err)
		}
	}
	return nil
}

// WithdrawTicket retracts a ticket without deleting its row, so runs, pushes and events keep
// their foreign key. When merged, it also prunes the URL from every other ticket's blocked_by.
func (s *Store) WithdrawTicket(ctx context.Context, url string, now time.Time, merged bool) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	qtx := s.q.WithTx(tx)
	if err = qtx.WithdrawTicket(ctx, ccdb.WithdrawTicketParams{WithdrawnAt: notNullTime(now), URL: url}); err != nil {
		return fmt.Errorf("withdraw ticket %s: %w", url, err)
	}
	if merged {
		if err = repairBlockedBy(ctx, qtx, map[string]bool{url: true}); err != nil {
			return fmt.Errorf("repair blocked_by after withdrawing %s: %w", url, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit withdrawal of %s: %w", url, err)
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

const (
	metaObservation   = "observation"
	metaLastError     = "last_error"
	metaImportError   = "import_error"
	metaCheckingTicks = "checking_ticks"
	metaLastVerdicts  = "last_verdicts"
)

// CheckingTicks returns each ticket's count of successful ticks since it last had anything to
// resolve into a CI verdict. verdict.Input.Now derives from this, never wall clock.
func (s *Store) CheckingTicks(ctx context.Context) (map[plan.TicketURL]int, error) {
	ticks := map[plan.TicketURL]int{}
	if _, err := s.getMeta(ctx, metaCheckingTicks, &ticks); err != nil {
		return nil, fmt.Errorf("read %s meta: %w", metaCheckingTicks, err)
	}
	return ticks, nil
}

// IncrementCheckingTicks bumps every named ticket's counter by one per successful tick.
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

// ResetCheckingTicks zeroes one ticket's counter on every fresh push.
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

// LastVerdicts returns each ticket's most recently recorded CI verdict label, keyed by ticket URL.
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

// SaveObservation replaces the persisted observation. Only a successful tick calls it.
func (s *Store) SaveObservation(ctx context.Context, obs plan.Observation) error {
	if err := s.putMeta(ctx, metaObservation, obs); err != nil {
		return fmt.Errorf("write %s meta: %w", metaObservation, err)
	}
	return nil
}

func (s *Store) LastObservation(ctx context.Context) (plan.Observation, bool, error) {
	var obs plan.Observation
	found, err := s.getMeta(ctx, metaObservation, &obs)
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

// LastError returns the last tick failure. A later success does not clear it.
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

// ImportError is the last feature-conflict import refusal.
type ImportError struct {
	At      time.Time `json:"at"`
	Feature string    `json:"feature"`
	Message string    `json:"message"`
}

type importRefusal interface {
	error
	refusedTicket() string
}

// RecordImportRefusal stores refusal as the last import failure and appends its audit event.
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

// LastImportError returns the last import refusal; a later success does not clear it.
func (s *Store) LastImportError(ctx context.Context) (ImportError, bool, error) {
	var importErr ImportError
	found, err := s.getMeta(ctx, metaImportError, &importErr)
	if err != nil {
		return importErr, found, fmt.Errorf("read %s meta: %w", metaImportError, err)
	}
	return importErr, found, nil
}

// Event is one append-only audit row.
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
		Detail:   notNull(e.Detail),
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

func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.q.Events(ctx)
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
		e.TicketURL, e.Detail = row.TicketID.String, row.Detail.String
		events = append(events, e)
	}
	return events, nil
}

func (s *Store) TicketEventsAfter(ctx context.Context, ticketURL string, afterID int64) ([]Event, error) {
	rows, err := s.q.TicketEventsAfter(ctx, ccdb.TicketEventsAfterParams{
		TicketID: sql.NullString{String: ticketURL, Valid: ticketURL != ""},
		ID:       afterID,
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
			ID: row.ID, At: row.At, TicketURL: row.TicketID.String, Kind: kind, Detail: row.Detail.String,
		})
	}
	return events, nil
}

func (s *Store) putMeta(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	if err := s.q.PutMeta(ctx, ccdb.PutMetaParams{Key: key, Value: string(encoded)}); err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	return nil
}

func (s *Store) getMeta(ctx context.Context, key string, into any) (bool, error) {
	encoded, err := s.q.GetMeta(ctx, key)
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
