package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

func launchTickets(t *testing.T, store *storepkg.Store, urls ...string) {
	t.Helper()
	tickets := make([]storepkg.Ticket, 0, len(urls))
	for _, url := range urls {
		tickets = append(tickets, storepkg.Ticket{URL: url, Repo: "cc-sandbox", Branch: "branch-" + url})
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
}

func queueLaunch(t *testing.T, store *storepkg.Store, req storepkg.LaunchRequest) {
	t.Helper()
	if err := store.QueueLaunchIntent(t.Context(), req); err != nil {
		t.Fatalf("QueueLaunchIntent(%s): %v", req.Ticket, err)
	}
}

func applyLaunches(t *testing.T, store *storepkg.Store, at time.Time) {
	t.Helper()
	if err := store.ApplyLaunchIntents(t.Context(), at); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
}

func readEvents(t *testing.T, store *storepkg.Store) []storepkg.Event {
	t.Helper()
	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	return events
}

func readMemberships(t *testing.T, store *storepkg.Store) map[plan.TicketURL]plan.LaunchMembership {
	t.Helper()
	memberships, err := store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatalf("LaunchMemberships: %v", err)
	}
	return memberships
}

func launchMemberHashes(t *testing.T, dsn string) map[string]string {
	t.Helper()
	rows, err := openRawDB(t, dsn).Query(`SELECT ticket_id, prompt_hash FROM launch_members`)
	if err != nil {
		t.Fatalf("query launch_members: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close launch_members rows: %v", err)
		}
	}()
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

func TestStore_ApplyLaunchIntents(t *testing.T) {
	t.Parallel()

	t.Run("groups one group into one launch with each member's hash", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		store := openStoreAt(t, dsn)
		launchTickets(t, store, "sandbox://CC-1", "sandbox://CC-2")
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
		})
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-2", PromptHash: "hash-2", Group: "group-a", At: testNow,
		})
		applyLaunches(t, store, testNow.Add(time.Second))

		memberships := readMemberships(t, store)
		if memberships["sandbox://CC-1"].LaunchID == 0 || memberships["sandbox://CC-2"].LaunchID == 0 {
			t.Errorf("LaunchMemberships() = %+v, want both tickets active", memberships)
		}
		if got := readEvents(t, store); len(got) != 1 {
			t.Errorf("Events() = %+v, want one launch event for the one group", got)
		}
		want := map[string]string{"sandbox://CC-1": "hash-1", "sandbox://CC-2": "hash-2"}
		if diff := cmp.Diff(want, launchMemberHashes(t, dsn)); diff != "" {
			t.Errorf("launch_members prompt hashes mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("separates groups into distinct launches", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		launchTickets(t, store, "sandbox://CC-1", "sandbox://CC-2")
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
		})
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-2", PromptHash: "hash-2", Group: "group-b", At: testNow,
		})
		applyLaunches(t, store, testNow.Add(time.Second))

		if got := readEvents(t, store); len(got) != 2 {
			t.Errorf("Events() = %+v, want two launch events for two groups", got)
		}
	})

	t.Run("is idempotent once consumed", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		launchTickets(t, store, "sandbox://CC-1")
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
		})
		applyLaunches(t, store, testNow.Add(time.Second))
		applyLaunches(t, store, testNow.Add(2*time.Second))

		if got := readEvents(t, store); len(got) != 1 {
			t.Errorf("Events() after two applies = %+v, want one launch event", got)
		}
	})
}

func TestStore_LaunchMemberships(t *testing.T) {
	t.Parallel()

	t.Run("none without launches", func(t *testing.T) {
		t.Parallel()
		if got := readMemberships(t, openStore(t)); len(got) != 0 {
			t.Errorf("LaunchMemberships() = %+v, want none", got)
		}
	})

	t.Run("names the launch and its member count", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		launchTickets(t, store, "sandbox://CC-1", "sandbox://CC-2")
		for _, ticket := range []string{"sandbox://CC-1", "sandbox://CC-2"} {
			queueLaunch(t, store, storepkg.LaunchRequest{Ticket: ticket, PromptHash: "hash", Group: "group-a", At: testNow})
		}
		applyLaunches(t, store, testNow.Add(time.Second))

		memberships := readMemberships(t, store)
		cc1 := memberships["sandbox://CC-1"]
		if cc1.LaunchID == 0 || cc1.Members != 2 {
			t.Errorf("LaunchMemberships()[CC-1] = %+v, want a launch id with 2 members", cc1)
		}
		if got := memberships["sandbox://CC-2"].LaunchID; got != cc1.LaunchID {
			t.Errorf("LaunchMemberships()[CC-2].LaunchID = %d, want CC-1's %d", got, cc1.LaunchID)
		}
	})

	t.Run("carries the launch's context and agent choice", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		launchTickets(t, store, "sandbox://CC-1", "sandbox://CC-2")
		choice := plan.AgentChoice{Model: "opus", Effort: option.Some(plan.EffortHigh)}
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash", Group: "group-a",
			Context: "prefer table tests", Choice: choice, At: testNow,
		})
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-2", PromptHash: "hash", Group: "group-b", At: testNow,
		})
		applyLaunches(t, store, testNow.Add(time.Second))

		memberships := readMemberships(t, store)
		if got := memberships["sandbox://CC-1"]; got.Context != "prefer table tests" || got.Choice != choice {
			t.Errorf("LaunchMemberships()[CC-1] = %+v, want context %q and choice %+v",
				got, "prefer table tests", choice)
		}
		if got := memberships["sandbox://CC-2"]; got.Context != "" || got.Choice != (plan.AgentChoice{}) {
			t.Errorf("LaunchMemberships()[CC-2] = %+v, want the default choice and no context", got)
		}
	})

	t.Run("a relaunched ticket is not cancelled", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		const ticket = "sandbox://CC-1"
		launchTickets(t, store, ticket)
		queueLaunch(t, store, storepkg.LaunchRequest{Ticket: ticket, PromptHash: "hash-1", Group: "group-a", At: testNow})
		applyLaunches(t, store, testNow.Add(time.Second))
		if _, err := store.CancelLaunchesFor(t.Context(), ticket); err != nil {
			t.Fatalf("CancelLaunchesFor(%s): %v", ticket, err)
		}
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: ticket, PromptHash: "hash-2", Group: "group-b", At: testNow.Add(3 * time.Second),
		})
		applyLaunches(t, store, testNow.Add(4*time.Second))

		if got := readMemberships(t, store)[ticket]; got.Cancelled || got.LaunchID == 0 {
			t.Errorf("LaunchMemberships()[CC-1] = %+v, want active under its new launch", got)
		}
	})
}

func TestStore_CancelLaunchesFor(t *testing.T) {
	t.Parallel()

	t.Run("cancels every sibling in the launch and counts them", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		urls := []string{"sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3"}
		launchTickets(t, store, urls...)
		for _, ticket := range urls {
			queueLaunch(t, store, storepkg.LaunchRequest{Ticket: ticket, PromptHash: "hash", Group: "group-a", At: testNow})
		}
		applyLaunches(t, store, testNow.Add(time.Second))

		members, err := store.CancelLaunchesFor(t.Context(), "sandbox://CC-2")
		if err != nil || members != 3 {
			t.Errorf("CancelLaunchesFor(CC-2) = %d, %v, want 3, nil", members, err)
		}
		memberships := readMemberships(t, store)
		for _, ticket := range urls {
			if !memberships[plan.TicketURL(ticket)].Cancelled {
				t.Errorf("LaunchMemberships()[%s].Cancelled = false, want true", ticket)
			}
		}
	})

	t.Run("an unauthorised ticket cancels nothing", func(t *testing.T) {
		t.Parallel()
		members, err := openStore(t).CancelLaunchesFor(t.Context(), "sandbox://GHOST")
		if err != nil || members != 0 {
			t.Errorf("CancelLaunchesFor(GHOST) = %d, %v, want 0, nil", members, err)
		}
	})
}

func TestStore_FeatureFirstLaunch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Feature: "f"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", Feature: "f"},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", Feature: "g"},
	}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	first, later := testNow, testNow.Add(time.Hour)
	for _, launch := range []struct {
		ticket string
		at     time.Time
	}{{"sandbox://CC-2", later}, {"sandbox://CC-1", first}} {
		queueLaunch(t, store, storepkg.LaunchRequest{
			Ticket: launch.ticket, PromptHash: "hash", Group: launch.ticket, At: launch.at,
		})
		applyLaunches(t, store, launch.at)
	}

	t.Run("the earliest launch of the feature", func(t *testing.T) {
		t.Parallel()
		got, err := store.FeatureFirstLaunch(ctx, "f")
		if err != nil || !got.Equal(first) {
			t.Errorf("FeatureFirstLaunch(f) = %v, %v, want %v, nil", got, err, first)
		}
	})

	t.Run("a feature never launched", func(t *testing.T) {
		t.Parallel()
		if _, err := store.FeatureFirstLaunch(ctx, "g"); !errors.Is(err, storepkg.ErrNotFound) {
			t.Errorf("FeatureFirstLaunch(g) error = %v, want ErrNotFound", err)
		}
	})
}
