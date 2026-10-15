package store_test

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func openRawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertingAValueOutsideItsSetFailsOnTheNamedConstraint(t *testing.T) {
	t.Parallel()

	insertWindow := func(table string) func(context.Context, *storepkg.Store, *sql.DB) error {
		return func(ctx context.Context, _ *storepkg.Store, db *sql.DB) error {
			columns, values := windowColumns[table], windowValues[table]
			_, err := db.ExecContext(ctx,
				`INSERT INTO `+table+` ("window", `+columns+`) VALUES ('bogus', `+values+`)`)
			return err
		}
	}
	tests := []struct {
		name       string
		constraint string
		insert     func(context.Context, *storepkg.Store, *sql.DB) error
	}{
		{"run kind", "runs_kind_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
			_, err := s.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
				Ticket: "sandbox://CC-1", Kind: plan.RunKind("bogus"), BaselineSHA: "base", PromptHash: "hash",
			})
			return err
		}},
		{"run outcome", "runs_outcome_check", func(ctx context.Context, s *storepkg.Store, db *sql.DB) error {
			runID, err := s.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
				Ticket: "sandbox://CC-1", Kind: plan.RunKindAgent, BaselineSHA: "base", PromptHash: "hash",
			})
			if err != nil {
				return err
			}
			_, err = db.ExecContext(ctx, `UPDATE runs SET outcome = 'bogus' WHERE id = $1`, runID)
			return err
		}},
		{"intent verb", "intents_verb_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
			return s.QueueVerbIntent(ctx, "sandbox://CC-1", plan.Verb("bogus"), time.Now())
		}},
		{"refusal kind", "repos_refusal_kind_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
			return s.UpsertRepo(ctx, storepkg.Repo{
				Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoRefused,
				RefusalKind: plan.RefusalKind("bogus"), Refusal: "why", TrackedAt: trackedAt,
			})
		}},
		{"event kind", "events_kind_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
			return s.AppendEvent(ctx, storepkg.Event{At: time.Now(), Kind: storepkg.EventKind("bogus")})
		}},
		{"reading window", "utilization_readings_window_check", insertWindow("utilization_readings")},
		{"interval window", "utilization_intervals_window_check", insertWindow("utilization_intervals")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dsn := cctest.DSN(t)
			store := openStoreAt(t, dsn)
			seedOneTicket(t, store)

			err := tt.insert(t.Context(), store, openRawDB(t, dsn))

			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != tt.constraint {
				t.Errorf("insert error = %v, want a violation of %s", err, tt.constraint)
			}
		})
	}
}

var windowColumns = map[string]string{
	"utilization_readings":  "at, utilization, resets_at",
	"utilization_intervals": "start_at, end_at, utilization_start, utilization_end, weight_usd",
}

var windowValues = map[string]string{
	"utilization_readings":  "now(), 0.5, now()",
	"utilization_intervals": "now(), now(), 0, 1, 1",
}

func TestEachCheckListsExactlyItsGoSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		constraint string
		want       []string
	}{
		{"runs_kind_check", toStrings(plan.AllRunKinds())},
		{"runs_outcome_check", outcomeNames()},
		{"intents_verb_check", append(toStrings(plan.AllVerbs()), "edit_ticket")},
		{"repos_refusal_kind_check", toStrings(plan.AllRefusalKinds())},
		{"events_kind_check", toStrings(storepkg.AllEventKinds())},
		{"utilization_readings_window_check", toStrings([]agentlog.Window{agentlog.FiveHour, agentlog.SevenDay})},
		{"utilization_intervals_window_check", toStrings([]agentlog.Window{agentlog.FiveHour, agentlog.SevenDay})},
	}
	dsn := cctest.DSN(t)
	openStoreAt(t, dsn)
	db := openRawDB(t, dsn)
	literal := regexp.MustCompile(`'([^']*)'`)
	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			t.Parallel()

			var definition string
			err := db.QueryRowContext(t.Context(),
				`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, tt.constraint,
			).Scan(&definition)
			if err != nil {
				t.Fatalf("QueryRowContext(%s).Scan: %v", tt.constraint, err)
			}
			var got []string
			for _, m := range literal.FindAllStringSubmatch(definition, -1) {
				got = append(got, m[1])
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tt.want))
			if !slices.Equal(want, got) {
				t.Errorf("%s lists %q, want the Go set %q", tt.constraint, got, want)
			}
		})
	}
}

func toStrings[T ~string](set []T) []string {
	out := make([]string, len(set))
	for i, v := range set {
		out[i] = string(v)
	}
	return out
}

func outcomeNames() []string {
	var out []string
	for _, o := range plan.AllOutcomes() {
		out = append(out, o.String())
	}
	return out
}
