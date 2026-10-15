package loop_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
)

func importLoop(store *storepkg.Store, at time.Time, tickets ...tracker.Ticket) *loop.Loop {
	src := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:x": tickets}}
	resolve := trackertest.ByRemote(map[string]tracker.Source{"github.com/acme/alpha": src})
	return newLoop(store, noOpObserve, fixedClock(at), config.Config{}, config.Workspace{}, runner.ProcessRunner{},
		loop.WithTrackerSource(resolve))
}

func TestImport(t *testing.T) {
	t.Parallel()

	t.Run("applies a pending import intent once", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
		if err := store.QueueVerbIntent(ctx, "project:x", "import", testAt); err != nil {
			t.Fatalf("QueueVerbIntent(import): %v", err)
		}
		issue := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x", Status: "ready"}

		if err := importLoop(store, testAt, issue).RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		tickets, err := store.Tickets(ctx)
		if err != nil {
			t.Fatalf("Tickets(): %v", err)
		}
		if len(tickets) != 1 || tickets[0].Branch != "cc-1-add-x" || tickets[0].Repo != "alpha" {
			t.Fatalf("Tickets() = %+v, want one imported row on alpha", tickets)
		}
		pending, err := store.PendingVerbIntents(ctx, "import")
		if err != nil {
			t.Fatalf("PendingVerbIntents(import): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("PendingVerbIntents(import) = %+v, want none", pending)
		}
	})

	t.Run("a refused import is recorded without halting the tick", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
		const outsider = "https://github.com/acme/alpha/issues/1"
		seed := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha"},
		}
		if err := store.ImportTickets(ctx, "project:other", seed, testAt); err != nil {
			t.Fatalf("seed ImportTickets: %v", err)
		}
		if err := store.QueueVerbIntent(ctx, "project:x", "import", testAt); err != nil {
			t.Fatalf("QueueVerbIntent(import): %v", err)
		}

		lp := importLoop(store, testAt.Add(time.Hour), tracker.Ticket{URL: outsider, Number: 1, Title: "Add x"})
		if err := lp.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v, want the refusal handled in the tick", err)
		}

		pending, err := store.PendingVerbIntents(ctx, "import")
		if err != nil {
			t.Fatalf("PendingVerbIntents(import): %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("PendingVerbIntents(import) = %+v, want the refused intent consumed", pending)
		}
		lastErr, failed, err := store.LastImportError(ctx)
		if err != nil {
			t.Fatalf("LastImportError(): %v", err)
		}
		if !failed || lastErr.Feature != "project:x" || !strings.Contains(lastErr.Message, outsider) {
			t.Errorf("LastImportError() = %+v, %v, want project:x naming %s", lastErr, failed, outsider)
		}
	})

	t.Run("sets the ticket source from the repo's configured tracker", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
		if err := store.QueueVerbIntent(ctx, "project:x", "import", testAt); err != nil {
			t.Fatalf("QueueVerbIntent(import): %v", err)
		}
		seed := plan.Observation{Settings: map[string]config.RepoSettings{"alpha": {Tracker: "linear"}}}
		if err := store.SaveObservation(ctx, seed); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		issue := tracker.Ticket{URL: "https://linear.app/acme/issue/eng-1", Number: 1, Title: "Add x", Status: "ready"}

		if err := importLoop(store, testAt, issue).RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}

		tickets, err := store.Tickets(ctx)
		if err != nil {
			t.Fatalf("Tickets(): %v", err)
		}
		if len(tickets) != 1 || tickets[0].Source != "linear" {
			t.Errorf("Tickets() = %+v, want one with source linear", tickets)
		}
	})
}
