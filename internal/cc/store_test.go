package cc_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func openStore(t *testing.T, path string) *cc.Store {
	t.Helper()
	store, err := cc.OpenStore(path)
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

func TestOpenStoreAppliesSchema(t *testing.T) {
	t.Parallel()

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	got, err := store.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if got != cc.SchemaVersion {
		t.Errorf("schema version = %d, want %d", got, cc.SchemaVersion)
	}
}

func TestOpenStoreRefusesVersionMismatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cc.db")
	store, err := cc.OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value = '99' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = cc.OpenStore(path)
	if err == nil {
		t.Fatal("want an error opening a DB at the wrong schema version")
	}
	for _, want := range []string{"99", "1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name version %s", err, want)
		}
	}
}

func TestUpsertTasksIsIdempotentOnTicketURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	first := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTasks(ctx, first); err != nil {
		t.Fatalf("UpsertTasks: %v", err)
	}

	edited := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-renamed"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTasks(ctx, edited); err != nil {
		t.Fatalf("UpsertTasks again: %v", err)
	}

	got, err := store.Tasks(ctx)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tasks = %d, want 2 (upsert on ticket_url, not insert)", len(got))
	}
	if got[0].Branch != "cc-1-renamed" {
		t.Errorf("branch = %q, want the edited %q", got[0].Branch, "cc-1-renamed")
	}
	if len(got[1].BlockedBy) != 1 || got[1].BlockedBy[0] != "sandbox://CC-1" {
		t.Errorf("blocked_by = %v, want [sandbox://CC-1]", got[1].BlockedBy)
	}
}

// TestUpsertTasksRoundTripsSeamsInConfigOrder covers issue #52: a task's seams must survive the
// store round-trip the loop's spawn path reads from, not just the config the app starts with.
func TestUpsertTasksRoundTripsSeamsInConfigOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	task := cc.Task{
		TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Seams: []string{"one", "two"},
	}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatalf("UpsertTasks: %v", err)
	}

	got, err := store.Tasks(ctx)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("tasks = %d, want 1", len(got))
	}
	if want := []string{"one", "two"}; !slices.Equal(got[0].Seams, want) {
		t.Errorf("seams = %v, want %v", got[0].Seams, want)
	}
}
