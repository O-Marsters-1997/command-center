package loop_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestEditTicket(t *testing.T) {
	t.Parallel()

	t.Run("applies a pending edit", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		store := openStore(t)
		at := testAt

		tickets := []storepkg.Ticket{
			{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
			{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"},
		}
		if err := store.UpsertTickets(ctx, tickets); err != nil {
			t.Fatalf("UpsertTickets(%d tickets): %v", len(tickets), err)
		}
		if err := store.QueueEditTicketIntent(ctx, storepkg.EditRequest{
			Ticket: "sandbox://CC-2", Branch: "cc-2-renamed", BlockedBy: []string{"sandbox://CC-1"}, At: at,
		}); err != nil {
			t.Fatalf("QueueEditTicketIntent(CC-2): %v", err)
		}

		lp := newLoop(store, noOpObserve, fixedClock(at), config.Config{}, config.Workspace{}, runner.ProcessRunner{})
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}

		got, err := store.Tickets(ctx)
		if err != nil {
			t.Fatalf("Tickets(): %v", err)
		}
		edited := ticketsByURLForTest(t, got)["sandbox://CC-2"]
		if edited.Branch != "cc-2-renamed" {
			t.Errorf("branch = %q, want cc-2-renamed", edited.Branch)
		}
		if diff := cmp.Diff([]string{"sandbox://CC-1"}, edited.BlockedBy); diff != "" {
			t.Errorf("edited blocked_by mismatch (-want +got):\n%s", diff)
		}

		pending, err := store.PendingEditTicketIntents(ctx)
		if err != nil {
			t.Fatalf("PendingEditTicketIntents(): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending edit intents = %+v, want the applied one consumed", pending)
		}
	})

	t.Run("a closure refusal is recorded and consumed", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		store := openStore(t)
		at := testAt

		inFeature := "https://github.com/acme/alpha/issues/1"
		outsider := "https://github.com/acme/beta/issues/2"
		seed := []storepkg.ImportedTicket{{Ticket: tracker.Ticket{URL: inFeature, Number: 1, Title: "Add x"}, Repo: "alpha"}}
		if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
			t.Fatalf("seed ImportTickets: %v", err)
		}
		outsiderSeed := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: outsider, Number: 2, Title: "Add y"}, Repo: "beta"},
		}
		if err := store.ImportTickets(ctx, "project:y", outsiderSeed, at); err != nil {
			t.Fatalf("seed outsider ImportTickets: %v", err)
		}

		err := store.QueueEditTicketIntent(ctx, storepkg.EditRequest{
			Ticket: inFeature, Branch: "cc-1-add-x", BlockedBy: []string{outsider}, At: at.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("QueueEditTicketIntent(%q): %v", inFeature, err)
		}

		lp := newLoop(
			store, noOpObserve, fixedClock(at.Add(time.Minute)), config.Config{}, config.Workspace{}, runner.ProcessRunner{},
		)
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce: want the refusal handled in-tick, got %v", err)
		}

		got, err := store.Tickets(ctx)
		if err != nil {
			t.Fatalf("Tickets(): %v", err)
		}
		edited := ticketsByURLForTest(t, got)[inFeature]
		if len(edited.BlockedBy) != 0 {
			t.Errorf("blocked_by = %v, want the refused edit to leave it untouched", edited.BlockedBy)
		}

		pending, err := store.PendingEditTicketIntents(ctx)
		if err != nil {
			t.Fatalf("PendingEditTicketIntents(): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("pending edit intents = %+v, want the refused one consumed rather than retried", pending)
		}

		lastErr, failed, err := store.LastImportError(ctx)
		if err != nil {
			t.Fatalf("LastImportError(): %v", err)
		}
		if !failed || lastErr.Feature != "project:x" || !strings.Contains(lastErr.Message, outsider) {
			t.Errorf("LastImportError = %+v, failed=%v, want project:x naming %s", lastErr, failed, outsider)
		}
	})
}

func ticketsByURLForTest(t *testing.T, tickets []storepkg.Ticket) map[string]storepkg.Ticket {
	t.Helper()
	byURL := make(map[string]storepkg.Ticket, len(tickets))
	for _, ticket := range tickets {
		byURL[ticket.URL] = ticket
	}
	return byURL
}
