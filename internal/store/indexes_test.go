package store_test

import (
	"database/sql"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
)

func TestMigrationsLeaveEveryHotIndexValid(t *testing.T) {
	t.Parallel()

	dsn := cctest.DSN(t)
	openStoreAt(t, dsn)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	want := []string{
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
	}
	for _, name := range want {
		t.Run(name, func(t *testing.T) {
			var valid bool
			err := db.QueryRowContext(t.Context(), `
				SELECT i.indisvalid
				FROM pg_indexes x
				JOIN pg_class c ON c.relname = x.indexname
				JOIN pg_index i ON i.indexrelid = c.oid
				WHERE x.schemaname = current_schema() AND x.indexname = $1`, name).Scan(&valid)
			if err != nil {
				t.Fatalf("index %s: %v", name, err)
			}
			if !valid {
				t.Errorf("index %s is invalid", name)
			}
		})
	}
}
