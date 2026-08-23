package cc_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func TestApplyLaunchIntentsGroupsIntoOneLaunch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "cc.db")
	store := openStore(t, dbPath)
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatalf("UpsertTasks: %v", err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatalf("QueueLaunchIntent CC-1: %v", err)
	}
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-2", "hash-2", "group-a", at); err != nil {
		t.Fatalf("QueueLaunchIntent CC-2: %v", err)
	}

	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}

	memberships, err := store.ActiveMemberships(ctx)
	if err != nil {
		t.Fatalf("ActiveMemberships: %v", err)
	}
	if !memberships["sandbox://CC-1"] || !memberships["sandbox://CC-2"] {
		t.Errorf("memberships = %+v, want both tasks active", memberships)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want exactly one launch event for the one group", len(events))
	}

	hashes := launchMemberHashes(t, dbPath)
	if len(hashes) != 2 {
		t.Fatalf("launch_members = %d, want 2", len(hashes))
	}
	if hashes["sandbox://CC-1"] == "" || hashes["sandbox://CC-2"] == "" {
		t.Errorf("hashes = %+v, want non-empty prompt_hash per member", hashes)
	}
	if hashes["sandbox://CC-1"] == hashes["sandbox://CC-2"] {
		t.Errorf("hashes = %+v, want distinct prompt_hash per member", hashes)
	}
}

func TestApplyLaunchIntentsSeparatesGroupsIntoDistinctLaunches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{TicketURL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatalf("UpsertTasks: %v", err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-2", "hash-2", "group-b", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events = %d, want two separate launch events for two groups", len(events))
	}
}

func TestApplyLaunchIntentsIsIdempotentOnceConsumed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	task := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTasks(ctx, []cc.Task{task}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatalf("first ApplyLaunchIntents: %v", err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(2*time.Second)); err != nil {
		t.Fatalf("second ApplyLaunchIntents: %v", err)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1: a consumed intent must not be applied twice", len(events))
	}
}

func TestActiveMembershipsExcludesUnauthorisedTasks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	memberships, err := store.ActiveMemberships(ctx)
	if err != nil {
		t.Fatalf("ActiveMemberships: %v", err)
	}
	if len(memberships) != 0 {
		t.Errorf("memberships = %+v, want none with no launches", memberships)
	}
}

// launchMemberHashes reads launch_members directly: the store's own methods expose only
// membership, and asserting distinct, non-empty prompt hashes needs the column itself.
func launchMemberHashes(t *testing.T, dbPath string) map[string]string {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db directly: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT task_id, prompt_hash FROM launch_members`)
	if err != nil {
		t.Fatalf("query launch_members: %v", err)
	}
	defer func() { _ = rows.Close() }()

	hashes := map[string]string{}
	for rows.Next() {
		var taskID, hash string
		if err := rows.Scan(&taskID, &hash); err != nil {
			t.Fatalf("scan launch_members row: %v", err)
		}
		hashes[taskID] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate launch_members: %v", err)
	}
	return hashes
}
