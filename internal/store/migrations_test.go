package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

const lastIndexMigration = 32

func migrateTo(t *testing.T, dsn string, version int64) *sql.DB {
	t.Helper()
	db := openRawDB(t, dsn)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("migrations"))
	if err != nil {
		t.Fatalf("goose.NewProvider: %v", err)
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

var windowColumns = map[string]string{
	"utilization_readings":  "at, utilization, resets_at",
	"utilization_intervals": "start_at, end_at, utilization_start, utilization_end, weight_usd",
}

var windowValues = map[string]string{
	"utilization_readings":  "now(), 0.5, now()",
	"utilization_intervals": "now(), now(), 0, 1, 1",
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

func TestMigrations(t *testing.T) {
	t.Parallel()

	t.Run("convert legacy rows to typed columns", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		dsn := cctest.UnmigratedDSN(t)
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

		byURL := ticketsByURLForTest(t, readTickets(t, store))
		if got := byURL["https://x/1"].SyncedAt; !got.IsZero() {
			t.Errorf("Tickets()[x/1].SyncedAt = %v, want the zero time for a legacy empty value", got)
		}
		if got := byURL["https://x/2"].SyncedAt; !got.Equal(at) {
			t.Errorf("Tickets()[x/2].SyncedAt = %v, want %v", got, at)
		}
		followUps, err := store.PendingVerbIntents(ctx, plan.VerbFollowUp)
		if err != nil {
			t.Fatalf("PendingVerbIntents(follow_up): %v", err)
		}
		if len(followUps) != 1 || followUps[0].Payload != `fix "the" flaky test` {
			t.Errorf("PendingVerbIntents(follow_up) = %+v, want the legacy prompt decoded", followUps)
		}
		edits, err := store.PendingEditTicketIntents(ctx)
		if err != nil {
			t.Fatalf("PendingEditTicketIntents: %v", err)
		}
		if len(edits) != 1 || edits[0].Branch != "nb" || !slices.Equal(edits[0].BlockedBy, []string{"https://x/2"}) {
			t.Errorf("PendingEditTicketIntents() = %+v, want branch nb blocked by x/2", edits)
		}
		events, err := store.Events(ctx)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		if len(events) != 1 || events[0].Detail != "" {
			t.Errorf("Events() = %+v, want the NULL detail backfilled to empty", events)
		}
		ticks, err := store.CheckingTicks(ctx)
		if err != nil {
			t.Fatalf("CheckingTicks: %v", err)
		}
		if got := ticks["https://x/1"]; got != 2 {
			t.Errorf("CheckingTicks()[x/1] = %d, want 2", got)
		}
	})

	t.Run("leave typed columns and named constraints", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		openStoreAt(t, dsn)
		db := openRawDB(t, dsn)

		columns := []struct{ table, column, dataType, nullable string }{
			{"meta", "value", "jsonb", "NO"},
			{"tickets", "synced_at", "timestamp with time zone", "YES"},
			{"intents", "payload", "jsonb", "YES"},
			{"events", "detail", "text", "NO"},
		}
		for _, c := range columns {
			t.Run(c.table+"."+c.column, func(t *testing.T) {
				t.Parallel()
				var dataType, nullable string
				err := db.QueryRowContext(t.Context(), `
					SELECT data_type, is_nullable FROM information_schema.columns
					WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
					c.table, c.column).Scan(&dataType, &nullable)
				if err != nil {
					t.Fatalf("read %s.%s: %v", c.table, c.column, err)
				}
				if dataType != c.dataType || nullable != c.nullable {
					t.Errorf("%s.%s = %s nullable=%s, want %s nullable=%s",
						c.table, c.column, dataType, nullable, c.dataType, c.nullable)
				}
			})
		}

		constraints := []struct{ table, name string }{
			{"repos", "repos_state_check"},
			{"repos", "repos_refusal_check"},
			{"projects", "projects_hue_check"},
			{"projects", "projects_name_check"},
			{"meta", "meta_pkey"},
		}
		for _, c := range constraints {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				var found bool
				err := db.QueryRowContext(t.Context(), `
					SELECT EXISTS (SELECT 1 FROM pg_constraint
					WHERE conrelid = to_regclass($1) AND conname = $2)`, c.table, c.name).Scan(&found)
				if err != nil || !found {
					t.Errorf("constraint %s on %s = found %v, %v, want found", c.name, c.table, found, err)
				}
			})
		}
	})

	t.Run("leave every hot index valid", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		openStoreAt(t, dsn)
		db := openRawDB(t, dsn)
		for _, name := range []string{
			"runs_ticket_id_id_idx",
			"runs_launch_id_idx",
			"runs_pending_idx",
			"pushes_ticket_id_id_idx",
			"events_ticket_id_id_idx",
			"events_kind_ticket_id_id_idx",
			"events_tick_error_at_idx",
			"launch_members_ticket_id_idx",
			"repos_project_id_idx",
			"intents_pending_idx",
			"intents_consumed_at_idx",
			"tickets_feature_idx",
			"utilization_intervals_end_at_idx",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var valid bool
				err := db.QueryRowContext(t.Context(), `
					SELECT i.indisvalid
					FROM pg_indexes x
					JOIN pg_class c ON c.relname = x.indexname
					JOIN pg_index i ON i.indexrelid = c.oid
					WHERE x.schemaname = current_schema() AND x.indexname = $1`, name).Scan(&valid)
				if err != nil {
					t.Fatalf("read index %s: %v", name, err)
				}
				if !valid {
					t.Errorf("index %s indisvalid = false, want true", name)
				}
			})
		}
	})

	t.Run("each check lists exactly its Go set", func(t *testing.T) {
		t.Parallel()
		windows := toStrings([]agentlog.Window{agentlog.FiveHour, agentlog.SevenDay})
		tests := []struct {
			constraint string
			want       []string
		}{
			{"runs_kind_check", toStrings(plan.AllRunKinds())},
			{"runs_outcome_check", outcomeNames()},
			{"intents_verb_check", append(toStrings(plan.AllVerbs()), "edit_ticket")},
			{"repos_refusal_kind_check", toStrings(plan.AllRefusalKinds())},
			{"events_kind_check", toStrings(storepkg.AllEventKinds())},
			{"launches_effort_check", toStrings(plan.AllEfforts())},
			{"runs_effort_check", toStrings(plan.AllEfforts())},
			{"utilization_readings_window_check", windows},
			{"utilization_intervals_window_check", windows},
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
					t.Fatalf("read %s: %v", tt.constraint, err)
				}
				var got []string
				for _, m := range literal.FindAllStringSubmatch(definition, -1) {
					got = append(got, m[1])
				}
				slices.Sort(got)
				if diff := cmp.Diff(slices.Sorted(slices.Values(tt.want)), got); diff != "" {
					t.Errorf("%s values mismatch with the Go set (-want +got):\n%s", tt.constraint, diff)
				}
			})
		}
	})

	t.Run("a value outside its set fails on the named constraint", func(t *testing.T) {
		t.Parallel()
		insertWindow := func(table string) func(context.Context, *storepkg.Store, *sql.DB) error {
			return func(ctx context.Context, _ *storepkg.Store, db *sql.DB) error {
				_, err := db.ExecContext(ctx,
					`INSERT INTO `+table+` ("window", `+windowColumns[table]+`) VALUES ('bogus', `+windowValues[table]+`)`)
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
				return s.QueueVerbIntent(ctx, storepkg.VerbRequest{Ticket: "sandbox://CC-1", Verb: plan.Verb("bogus"), At: testNow})
			}},
			{"refusal kind", "repos_refusal_kind_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
				return s.UpsertRepo(ctx, storepkg.Repo{
					Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoRefused,
					RefusalKind: plan.RefusalKind("bogus"), Refusal: "why", TrackedAt: trackedAt,
				})
			}},
			{"event kind", "events_kind_check", func(ctx context.Context, s *storepkg.Store, _ *sql.DB) error {
				return s.AppendEvent(ctx, storepkg.Event{At: testNow, Kind: storepkg.EventKind("bogus")})
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
				if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.ConstraintName != tt.constraint {
					t.Errorf("insert %s error = %v, want a violation of %s", tt.name, err, tt.constraint)
				}
			})
		}
	})

	t.Run("tickets.blocked_by rejects invalid JSON", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		openStoreAt(t, dsn)
		_, err := openRawDB(t, dsn).Exec(
			`INSERT INTO tickets (url, repo, branch, blocked_by) VALUES ($1, $2, $3, $4)`,
			"sandbox://CC-1", "cc-sandbox", "cc-1", "not json",
		)
		if err == nil {
			t.Error("INSERT tickets with blocked_by 'not json' error = nil, want a database error")
		}
	})
}
