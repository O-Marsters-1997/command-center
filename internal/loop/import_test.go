package loop_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type fakeTrackerSource struct {
	features    []tracker.Feature
	tickets     map[string][]tracker.Ticket
	ticketCalls *int
}

func (f fakeTrackerSource) Features(context.Context) ([]tracker.Feature, error) {
	return f.features, nil
}

func (f fakeTrackerSource) Tickets(_ context.Context, feature string) ([]tracker.Ticket, error) {
	if f.ticketCalls != nil {
		*f.ticketCalls++
	}
	return f.tickets[feature], nil
}

func resolveByRemote(byRemote map[string]tracker.Source) tracker.Resolver {
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := byRemote[remote]
		if !ok {
			return nil, fmt.Errorf("resolveByRemote: no source for %q", remote)
		}
		return src, nil
	}
}

func TestLoopAppliesAPendingImportIntent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := testAt
	if err := store.QueueVerbIntent(ctx, "project:x", "import", at); err != nil {
		t.Fatalf("QueueVerbIntent(project:x, import): %v", err)
	}

	src := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {{
				URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x", Body: "b", Status: "ready",
			}},
		},
	}
	trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
	cfg := config.Config{}

	lp := newLoopWithDeps(loop.Deps{
		Store: store, Observe: noOpObserve, Clock: fixedClock(at), Config: cfg,
		Runner:  runner.ProcessRunner{},
		Tracker: resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}),
	})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatalf("Tickets(): %v", err)
	}
	if len(tickets) != 1 || tickets[0].Branch != "cc-1-add-x" || tickets[0].Repo != "alpha" {
		t.Fatalf("tickets = %+v, want one imported row for alpha", tickets)
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatalf("PendingVerbIntents(import): %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending import intents = %+v, want the applied one consumed", pending)
	}
}

func TestLoopRecordsImportRefusalsWithoutHaltingTheTick(t *testing.T) {
	t.Parallel()

	const outsider = "https://github.com/acme/alpha/issues/1"
	tests := []struct {
		name       string
		imported   tracker.Ticket
		refusedURL string
	}{
		{
			name:       "ticket already belongs to another feature",
			imported:   tracker.Ticket{URL: outsider, Number: 1, Title: "Add x"},
			refusedURL: outsider,
		},
		{
			name: "blocker belongs to another feature", imported: tracker.Ticket{
				URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Add y", BlockedBy: []string{outsider},
			},
			refusedURL: "https://github.com/acme/alpha/issues/2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store := openStore(t)
			at := testAt
			seed := []storepkg.ImportedTicket{{
				Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha",
			}}
			if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
				t.Fatalf("seed ImportTickets: %v", err)
			}
			if err := store.QueueVerbIntent(ctx, "project:y", "import", at); err != nil {
				t.Fatalf("QueueVerbIntent(project:y, import): %v", err)
			}
			src := fakeTrackerSource{
				features: []tracker.Feature{"project:y"},
				tickets:  map[string][]tracker.Ticket{"project:y": {tt.imported}},
			}
			trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
			lp := newLoopWithDeps(loop.Deps{
				Store: store, Observe: noOpObserve, Clock: fixedClock(at.Add(time.Hour)),
				Runner:  runner.ProcessRunner{},
				Tracker: resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}),
			})
			if err := lp.RunOnce(ctx); err != nil {
				t.Fatalf("RunOnce: want the refusal handled in-tick, got %v", err)
			}
			tickets, err := store.Tickets(ctx)
			if err != nil {
				t.Fatalf("Tickets(): %v", err)
			}
			if len(tickets) != 1 || tickets[0].URL != outsider || tickets[0].Feature != "project:x" {
				t.Errorf("tickets = %+v, want only the outsider under project:x", tickets)
			}
			pending, err := store.PendingVerbIntents(ctx, "import")
			if err != nil {
				t.Fatalf("PendingVerbIntents(import): %v", err)
			}
			if len(pending) != 0 {
				t.Errorf("pending import intents = %+v, want the refused one consumed", pending)
			}
			lastErr, failed, err := store.LastImportError(ctx)
			if err != nil {
				t.Fatalf("LastImportError(): %v", err)
			}
			if !failed || lastErr.Feature != "project:y" || !strings.Contains(lastErr.Message, tt.refusedURL) {
				t.Errorf("LastImportError = %+v, failed=%v, want project:y naming %s", lastErr, failed, tt.refusedURL)
			}
			events, err := store.Events(ctx)
			if err != nil {
				t.Fatalf("Events(): %v", err)
			}
			found := false
			for _, e := range events {
				if e.Kind == "import_refused" && (e.TicketURL == tt.refusedURL || strings.Contains(e.Detail, tt.refusedURL)) {
					found = true
				}
			}
			if !found {
				t.Errorf("events = %+v, want import_refused naming %s", events, tt.refusedURL)
			}
		})
	}
}

func TestLoopSetsTicketSourceFromTheReposConfiguredTracker(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := testAt
	if err := store.QueueVerbIntent(ctx, "project:x", "import", at); err != nil {
		t.Fatalf("QueueVerbIntent(project:x, import): %v", err)
	}

	src := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {{
				URL: "https://linear.app/acme/issue/eng-1", Number: 1, Title: "Add x", Status: "ready",
			}},
		},
	}
	trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
	cfg := config.Config{}
	seed := plan.Observation{Settings: map[string]config.RepoSettings{"alpha": {Tracker: "linear"}}}
	if err := store.SaveObservation(ctx, seed); err != nil {
		t.Fatalf("SaveObservation(seed): %v", err)
	}

	lp := newLoopWithDeps(loop.Deps{
		Store: store, Observe: noOpObserve, Clock: fixedClock(at), Config: cfg,
		Runner:  runner.ProcessRunner{},
		Tracker: resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}),
	})
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatalf("Tickets(): %v", err)
	}
	if len(tickets) != 1 || tickets[0].Source != "linear" {
		t.Fatalf("tickets = %+v, want source linear from the repo's own configured tracker", tickets)
	}
}
