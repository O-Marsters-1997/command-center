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
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
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

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if memberships["sandbox://CC-1"].LaunchID == 0 || memberships["sandbox://CC-2"].LaunchID == 0 {
		t.Errorf("memberships = %+v, want both tickets active", memberships)
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
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
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
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
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

func TestLaunchMembershipsExcludesUnauthorisedTickets(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if len(memberships) != 0 {
		t.Errorf("memberships = %+v, want none with no launches", memberships)
	}
}

func TestCancelLaunchesForCancelsEveryActiveLaunchAndCountsMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, ticketID := range []string{"sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3"} {
		if err := store.QueueLaunchIntent(ctx, ticketID, "hash", "group-a", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	members, err := store.CancelLaunchesFor(ctx, "sandbox://CC-2")
	if err != nil {
		t.Fatalf("CancelLaunchesFor: %v", err)
	}
	if members != 3 {
		t.Errorf("members = %d, want 3: every ticket the launch withdraws", members)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	for _, ticketID := range []string{"sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3"} {
		if !memberships[ticketID].Cancelled {
			t.Errorf("memberships = %+v, want %s cancelled too: every sibling, not just the one named",
				memberships, ticketID)
		}
	}
}

func TestCancelLaunchesForOnAnUnauthorisedTicketCancelsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	members, err := store.CancelLaunchesFor(ctx, "sandbox://GHOST")
	if err != nil {
		t.Fatalf("CancelLaunchesFor: %v", err)
	}
	if members != 0 {
		t.Errorf("members = %d, want 0", members)
	}
}

func TestLaunchMembershipsExcludeARelaunchedTicketFromCancelled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.QueueLaunchIntent(ctx, ticket.URL, "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelLaunchesFor(ctx, ticket.URL); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueLaunchIntent(ctx, ticket.URL, "hash-2", "group-b", at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	if got := memberships[ticket.URL]; got.Cancelled || got.LaunchID == 0 {
		t.Errorf("membership = %+v, want %s active under its new launch, not cancelled", got, ticket.URL)
	}
}

func TestLaunchMembershipsNameTheLaunchAndItsMemberCount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, ticketID := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
		if err := store.QueueLaunchIntent(ctx, ticketID, "hash", "group-a", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApplyLaunchIntents(ctx, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	memberships, err := store.LaunchMemberships(ctx)
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	cc1, ok := memberships["sandbox://CC-1"]
	if !ok {
		t.Fatalf("memberships = %+v, want sandbox://CC-1 present", memberships)
	}
	if cc1.Members != 2 {
		t.Errorf("CC-1 members = %d, want 2", cc1.Members)
	}
	if cc1.LaunchID == 0 {
		t.Error("CC-1 launch id is 0, want the real launch id")
	}
	if got := memberships["sandbox://CC-2"].LaunchID; got != cc1.LaunchID {
		t.Errorf("CC-2 launch id = %d, want CC-1's launch id %d: same group", got, cc1.LaunchID)
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

	rows, err := db.Query(`SELECT ticket_id, prompt_hash FROM launch_members`)
	if err != nil {
		t.Fatalf("query launch_members: %v", err)
	}
	defer func() { _ = rows.Close() }()

	hashes := map[string]string{}
	for rows.Next() {
		var ticketID, hash string
		if err := rows.Scan(&ticketID, &hash); err != nil {
			t.Fatalf("scan launch_members row: %v", err)
		}
		hashes[ticketID] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate launch_members: %v", err)
	}
	return hashes
}
