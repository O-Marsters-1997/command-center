package store_test

import (
	"slices"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestSessionRunsForTicketReturnsTheLatestSessionInOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	const ticket = "sandbox://CC-1"

	got, err := store.SessionRunsForTicket(ctx, ticket)
	if err != nil || len(got) != 0 {
		t.Fatalf("SessionRunsForTicket with no runs = %v, %v, want none", got, err)
	}

	_, oldSession, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	launchID, session, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if session == oldSession {
		t.Fatal("a second launch reused the first session")
	}
	firstID, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "follow_up", Session: storepkg.RunSession{JoinID: session, Message: "go on"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err = store.SessionRunsForTicket(ctx, ticket)
	if err != nil {
		t.Fatal(err)
	}
	want := []storepkg.SessionRun{{ID: launchID}, {ID: firstID, Message: "go on"}}
	if !slices.Equal(got, want) {
		t.Errorf("SessionRunsForTicket() = %+v, want %+v", got, want)
	}
}

func TestSessionRunsForTicketIsEmptyWhenTheLatestRunHasNoSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	const ticket = "sandbox://CC-1"

	skeleton := storepkg.RunSkeleton{Ticket: ticket, Kind: "agent", PromptHash: "hash"}
	if _, err := store.InsertRunSkeleton(ctx, skeleton); err != nil {
		t.Fatal(err)
	}
	got, err := store.SessionRunsForTicket(ctx, ticket)
	if err != nil || len(got) != 0 {
		t.Errorf("SessionRunsForTicket(no session) = %v, %v, want none", got, err)
	}
}
