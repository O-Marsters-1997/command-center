package cc_test

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func ticketByURL(t *testing.T, store *cc.Store, url string) cc.Ticket {
	t.Helper()
	tickets, err := store.Tickets(t.Context())
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	for _, ticket := range tickets {
		if ticket.URL == url {
			return ticket
		}
	}
	t.Fatalf("no ticket %s", url)
	return cc.Ticket{}
}

func appendVerdictEvent(t *testing.T, store *cc.Store, ticketURL string, at time.Time, detail string) {
	t.Helper()
	event := cc.Event{At: at, TicketURL: ticketURL, Kind: "verdict_transition", Detail: detail}
	if err := store.AppendEvent(t.Context(), event); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
}

func noopLoop(store *cc.Store, at time.Time) *cc.Loop {
	return cc.NewLoop(store,
		func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil },
		fixedClock(at), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
}

func TestRecordFirstPushCIRecordsFalseAndIgnoresALaterPass(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	pushedAt := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	if err := store.RecordPush(ctx, ticket.URL, "tip1", "main", "base1", pushedAt); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}
	appendVerdictEvent(t, store, ticket.URL, pushedAt.Add(time.Minute), "ci_failed: a check went red")

	if err := noopLoop(store, pushedAt.Add(time.Hour)).RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := ticketByURL(t, store, ticket.URL)
	if got.FirstPushCI == nil || *got.FirstPushCI {
		t.Fatalf("first_push_ci = %v, want false", got.FirstPushCI)
	}

	appendVerdictEvent(t, store, ticket.URL, pushedAt.Add(2*time.Hour), "review_me: every check is green")
	if err := noopLoop(store, pushedAt.Add(3*time.Hour)).RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got = ticketByURL(t, store, ticket.URL)
	if got.FirstPushCI == nil || *got.FirstPushCI {
		t.Fatalf("first_push_ci after a later pass = %v, want still false", got.FirstPushCI)
	}
}

func TestRecordFirstPushCIRecordsTrueForAPassingFirstVerdict(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	ticket := cc.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []cc.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}

	pushedAt := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	if err := store.RecordPush(ctx, ticket.URL, "tip1", "main", "base1", pushedAt); err != nil {
		t.Fatalf("RecordPush: %v", err)
	}
	appendVerdictEvent(t, store, ticket.URL, pushedAt.Add(-time.Minute), "ci_failed: stale, before the push")
	appendVerdictEvent(t, store, ticket.URL, pushedAt.Add(time.Minute), "checking: waiting on checks")
	appendVerdictEvent(t, store, ticket.URL, pushedAt.Add(2*time.Minute), "review_me: every check is green")

	if err := noopLoop(store, pushedAt.Add(time.Hour)).RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := ticketByURL(t, store, ticket.URL)
	if got.FirstPushCI == nil || !*got.FirstPushCI {
		t.Fatalf("first_push_ci = %v, want true", got.FirstPushCI)
	}
}
