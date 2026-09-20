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

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", pure Go
	"github.com/pressly/goose/v3"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the Postgres database. Only the loop goroutine writes it (inv. 9).
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

// setUpGoose configures goose once per process. goose keeps its base FS, logger and dialect in
// package-level globals, so setting them per OpenStore races between two concurrent opens.
func setUpGoose() error {
	gooseOnce.Do(func() {
		goose.SetBaseFS(migrations)
		goose.SetLogger(goose.NopLogger())
		gooseSetupErr = goose.SetDialect("postgres")
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

	qtx := s.q.WithTx(tx)
	for _, t := range tickets {
		blockedBy, _ := json.Marshal(nonNil(t.BlockedBy)) // json.Marshal of a []string cannot error
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
	return tx.Commit()
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
		_ = json.Unmarshal(row.BlockedBy, &t.BlockedBy) // jsonb rejects malformed JSON at write, so this can't fail
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

// ImportTickets upserts one feature's tracker tickets, keyed on url, withdrawing (and later
// restoring) any row the tracker stops (or resumes) returning for that feature. Every
// tracker-owned column refreshes each call; branch and blocked_by are seeded once and left to
// POST /ticket after that, except that a merged-and-withdrawn blocker is pruned out of every
// other ticket's blocked_by (see repairBlockedBy) rather than left stale forever.
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
		blockedBy, _ := json.Marshal(nonNil(t.BlockedBy)) // json.Marshal of a []string cannot error
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
		if err = json.Unmarshal(row.BlockedBy, &blockedBy); err != nil {
			return fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
		}
		intended = append(intended, ticketBlockedBy{URL: row.URL, BlockedBy: blockedBy})
	}
	if violation := closeUnderBlockedBy(
		feature, intended, ticketFeatureLookup(ctx, qtx), blockerMergedLookup(ctx, qtx, obs),
	); violation != nil {
		return violation
	}

	mergedWithdrawn := make(map[string]bool)
	for _, t := range previous {
		if returned[t.URL] {
			continue
		}
		if err = qtx.WithdrawTicket(ctx, ccdb.WithdrawTicketParams{WithdrawnAt: notNullTime(now), URL: t.URL}); err != nil {
			return fmt.Errorf("withdraw ticket %s: %w", t.URL, err)
		}
		// A withdrawal merely relabelled out of in-flight status (e.g. to status:backlog) is
		// reversible and must not touch anyone's blocked_by; only a withdrawal behind a merged
		// pull request is the terminal "this blocker is gone for good" the issue describes.
		if obs.PRs[branchKey(t.Repo, t.Branch)].State == gh.Merged {
			mergedWithdrawn[t.URL] = true
		}
	}
	if err = repairBlockedBy(ctx, qtx, mergedWithdrawn); err != nil {
		return err
	}
	return tx.Commit()
}

// repairBlockedBy prunes every merged-and-withdrawn url out of every other (live) ticket's
// stored blocked_by, across every feature. blocked_by is otherwise write-once after a ticket's
// first import (#215's app-owned split), so once its blocker merges and withdraws, nothing else
// ever revisits the stale edge and the dependent is stuck at blocked forever (issue #235).
func repairBlockedBy(ctx context.Context, qtx *ccdb.Queries, withdrawn map[string]bool) error {
	if len(withdrawn) == 0 {
		return nil
	}
	rows, err := qtx.TicketsWithBlockers(ctx)
	if err != nil {
		return fmt.Errorf("list tickets for blocker repair: %w", err)
	}
	for _, row := range rows {
		var blockedBy []string
		if err := json.Unmarshal(row.BlockedBy, &blockedBy); err != nil {
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
		blob, _ := json.Marshal(nonNil(pruned)) // json.Marshal of a []string cannot error
		if err := qtx.SetBlockedBy(ctx, ccdb.SetBlockedByParams{BlockedBy: blob, URL: row.URL}); err != nil {
			return fmt.Errorf("repair blocked_by for %s: %w", row.URL, err)
		}
	}
	return nil
}

// WithdrawTicket retracts a ticket without deleting its row, so runs, pushes and events keep
// their foreign key to it.
func (s *Store) WithdrawTicket(ctx context.Context, url string, now time.Time) error {
	if err := s.q.WithdrawTicket(ctx, ccdb.WithdrawTicketParams{WithdrawnAt: notNullTime(now), URL: url}); err != nil {
		return fmt.Errorf("withdraw ticket %s: %w", url, err)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// notNull wraps s as an always-valid sql.NullString, for a nullable column this package always
// writes a real value into, never an explicit NULL.
func notNull(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// notNullTime is notNull's sql.NullTime counterpart.
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

// ImportError is the last import refusal, rendered on GET /import.
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
		return err
	}
	return s.AppendEvent(ctx, Event{
		At: now, TicketURL: refusal.refusedTicket(), Kind: eventImportRefused, Detail: refusal.Error(),
	})
}

// LastImportError returns the last import refusal, if any; it is not cleared by a later success.
func (s *Store) LastImportError(ctx context.Context) (ImportError, bool, error) {
	var importErr ImportError
	found, err := s.getMeta(ctx, metaImportError, &importErr)
	return importErr, found, err
}

// Event is one append-only audit row.
type Event struct {
	At        time.Time
	TicketURL string
	Kind      string
	Detail    string
}

func (s *Store) AppendEvent(ctx context.Context, e Event) error {
	err := s.q.AppendEvent(ctx, ccdb.AppendEventParams{
		At:       e.At.UTC(),
		TicketID: sql.NullString{String: e.TicketURL, Valid: e.TicketURL != ""},
		Kind:     e.Kind,
		Detail:   notNull(e.Detail),
	})
	if err != nil {
		return fmt.Errorf("append event %s: %w", e.Kind, err)
	}
	return nil
}

// Events returns every audit row, oldest first.
func (s *Store) Events(ctx context.Context) ([]Event, error) {
	rows, err := s.q.Events(ctx)
	if err != nil {
		return nil, fmt.Errorf("select events: %w", err)
	}

	var events []Event
	for _, row := range rows {
		e := Event{At: row.At, Kind: row.Kind}
		e.TicketURL, e.Detail = row.TicketID.String, row.Detail.String
		events = append(events, e)
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
