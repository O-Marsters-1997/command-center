package store_test

import (
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestLatestSessionIDIsTheLatestAgentOrFollowUpRunsSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	const ticket = "sandbox://CC-1"

	got, err := store.LatestSessionID(ctx, ticket)
	if err != nil || got != "" {
		t.Fatalf("LatestSessionID with no runs = %q, %v, want empty", got, err)
	}

	_, agentSession, err := store.InsertSessionRunSkeleton(ctx, ticket, "agent", "", "hash", storepkg.RunSession{StartNew: true})
	if err != nil {
		t.Fatal(err)
	}
	if agentSession == "" {
		t.Fatal("a new session run has no session id")
	}

	_, reviewSession, err := store.InsertSessionRunSkeleton(ctx, ticket, "review", "", "", storepkg.RunSession{})
	if err != nil {
		t.Fatal(err)
	}
	if reviewSession != "" {
		t.Errorf("review run session = %q, want none", reviewSession)
	}

	got, err = store.LatestSessionID(ctx, ticket)
	if err != nil || got != agentSession {
		t.Errorf("LatestSessionID after a review run = %q, %v, want the agent's %q", got, err, agentSession)
	}

	_, followSession, err := store.InsertSessionRunSkeleton(
		ctx, ticket, "follow_up", "", "", storepkg.RunSession{JoinID: agentSession, Message: "go on"})
	if err != nil {
		t.Fatal(err)
	}
	if followSession != agentSession {
		t.Errorf("resumed run session = %q, want %q", followSession, agentSession)
	}
}
