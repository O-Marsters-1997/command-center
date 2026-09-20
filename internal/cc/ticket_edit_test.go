package cc_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestLoopAppliesAPendingEditTicketIntent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueEditTicketIntent(ctx, "sandbox://CC-2", "cc-2-renamed",
		[]string{"sandbox://CC-1"}, at); err != nil {
		t.Fatal(err)
	}

	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{})
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edited := ticketsByURLForTest(t, got)["sandbox://CC-2"]
	if edited.Branch != "cc-2-renamed" {
		t.Errorf("branch = %q, want cc-2-renamed", edited.Branch)
	}
	if !slices.Equal(edited.BlockedBy, []string{"sandbox://CC-1"}) {
		t.Errorf("blocked_by = %v, want [sandbox://CC-1]", edited.BlockedBy)
	}

	pending, err := store.PendingEditTicketIntents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending edit intents = %+v, want the applied one consumed", pending)
	}
}

func TestReimportDoesNotOverwriteAnAppliedEditTicketIntent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	url := "https://github.com/acme/alpha/issues/1"
	seed := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{URL: url, Number: 1, Title: "Add x", Status: "ready"},
		Repo:   "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	if err := store.QueueEditTicketIntent(ctx, url, "cc-1-custom-branch", nil, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	loop := cc.NewLoop(
		store, noOpObserve, fixedClock(at.Add(time.Minute)), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{},
	)
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	again := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{
			URL: url, Number: 1, Title: "Add x, renamed", Status: "in-progress",
			BlockedBy: []string{"https://github.com/acme/alpha/issues/2"},
		},
		Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", again, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets again: %v", err)
	}

	got, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("tickets = %+v, want 1", got)
	}
	if got[0].Branch != "cc-1-custom-branch" {
		t.Errorf("branch = %q, want the edited branch to survive re-import", got[0].Branch)
	}
	if len(got[0].BlockedBy) != 0 {
		t.Errorf("blocked_by = %v, want the edited (cleared) blockers to survive re-import", got[0].BlockedBy)
	}
	if got[0].Title != "Add x, renamed" || got[0].Status != "in-progress" {
		t.Errorf("title/status = %q/%q, want the re-imported tracker-owned values", got[0].Title, got[0].Status)
	}
}

// TestLoopRecordsAClosureRefusalOnAnEditTicketIntent pins issue #255's other route in: POST
// /ticket must not be able to edit blocked_by into a ticket outside its own feature either.
func TestLoopRecordsAClosureRefusalOnAnEditTicketIntent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	inFeature := "https://github.com/acme/alpha/issues/1"
	outsider := "https://github.com/acme/beta/issues/2"
	seed := []cc.ImportedTicket{{Ticket: tracker.Ticket{URL: inFeature, Number: 1, Title: "Add x"}, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}
	outsiderSeed := []cc.ImportedTicket{{Ticket: tracker.Ticket{URL: outsider, Number: 2, Title: "Add y"}, Repo: "beta"}}
	if err := store.ImportTickets(ctx, "project:y", outsiderSeed, at); err != nil {
		t.Fatalf("seed outsider ImportTickets: %v", err)
	}

	err := store.QueueEditTicketIntent(ctx, inFeature, "cc-1-add-x", []string{outsider}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	loop := cc.NewLoop(
		store, noOpObserve, fixedClock(at.Add(time.Minute)), cc.Config{}, cc.Workspace{}, cc.ProcessRunner{},
	)
	if err := loop.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: want the refusal handled in-tick, got %v", err)
	}

	got, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edited := ticketsByURLForTest(t, got)[inFeature]
	if len(edited.BlockedBy) != 0 {
		t.Errorf("blocked_by = %v, want the refused edit to leave it untouched", edited.BlockedBy)
	}

	pending, err := store.PendingEditTicketIntents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending edit intents = %+v, want the refused one consumed rather than retried", pending)
	}

	lastErr, failed, err := store.LastImportError(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !failed || lastErr.Feature != "project:x" || !strings.Contains(lastErr.Message, outsider) {
		t.Errorf("LastImportError = %+v, failed=%v, want project:x naming %s", lastErr, failed, outsider)
	}
}

func ticketsByURLForTest(t *testing.T, tickets []cc.Ticket) map[string]cc.Ticket {
	t.Helper()
	byURL := make(map[string]cc.Ticket, len(tickets))
	for _, ticket := range tickets {
		byURL[ticket.URL] = ticket
	}
	return byURL
}
