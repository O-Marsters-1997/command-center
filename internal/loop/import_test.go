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

// fakeTrackerSource answers Features and Tickets from fixed data, so a test drives import.go's
// consumers without shelling out to gh. ticketCalls, when set, counts Tickets calls so a test can
// pin that a caller never makes one.
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

// resolveByRemote builds a TrackerSource that dispatches on the normalised remote
// tracker.ForRemote passes it (host/owner/repo, e.g. github.com/acme/alpha), so a multi-repo test
// can give each repo its own fixed answers.
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
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(ctx, "project:x", "import", at); err != nil {
		t.Fatal(err)
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

	lp := loop.NewLoop(store, noOpObserve, fixedClock(at), cfg, config.Workspace{}, runner.ProcessRunner{})
	lp.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Branch != "cc-1-add-x" || tickets[0].Repo != "alpha" {
		t.Fatalf("tickets = %+v, want one imported row for alpha", tickets)
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending import intents = %+v, want the applied one consumed", pending)
	}
}

func TestLoopRecordsAnImportRefusalWithoutHaltingTheTick(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	contested := "https://github.com/acme/alpha/issues/1"
	seed := []storepkg.ImportedTicket{{Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	if err := store.QueueVerbIntent(ctx, "project:y", "import", at); err != nil {
		t.Fatal(err)
	}
	src := fakeTrackerSource{
		features: []tracker.Feature{"project:y"},
		tickets: map[string][]tracker.Ticket{
			"project:y": {{URL: contested, Number: 1, Title: "Add x"}},
		},
	}
	trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
	cfg := config.Config{}

	lp := loop.NewLoop(store, noOpObserve, fixedClock(at.Add(time.Hour)), cfg, config.Workspace{}, runner.ProcessRunner{})
	lp.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: want the refusal handled in-tick, got %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Feature != "project:x" {
		t.Fatalf("tickets = %+v, want the contested ticket to stay under project:x", tickets)
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending import intents = %+v, want the refused one consumed rather than retried", pending)
	}

	lastErr, failed, err := store.LastImportError(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !failed || lastErr.Feature != "project:y" || !strings.Contains(lastErr.Message, contested) {
		t.Errorf("LastImportError = %+v, failed=%v, want project:y naming %s", lastErr, failed, contested)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "import_refused" && e.TicketURL == contested {
			found = true
		}
	}
	if !found {
		t.Errorf("events = %+v, want an import_refused event against %s", events, contested)
	}
}

func TestLoopRecordsAClosureRefusalWithoutHaltingTheTick(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	outsider := "https://github.com/acme/alpha/issues/1"
	outsiderSeed := []storepkg.ImportedTicket{{
		Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", outsiderSeed, at); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	blocked := "https://github.com/acme/alpha/issues/2"
	if err := store.QueueVerbIntent(ctx, "project:y", "import", at); err != nil {
		t.Fatal(err)
	}
	src := fakeTrackerSource{
		features: []tracker.Feature{"project:y"},
		tickets: map[string][]tracker.Ticket{
			"project:y": {{URL: blocked, Number: 2, Title: "Add y", BlockedBy: []string{outsider}}},
		},
	}
	trackRepo(t, store, "alpha", "git@github.com:acme/alpha.git")
	cfg := config.Config{}

	lp := loop.NewLoop(store, noOpObserve, fixedClock(at.Add(time.Hour)), cfg, config.Workspace{}, runner.ProcessRunner{})
	lp.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: want the refusal handled in-tick, got %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != outsider {
		t.Fatalf("tickets = %+v, want only the outsider, project:y rolled back whole", tickets)
	}

	pending, err := store.PendingVerbIntents(ctx, "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending import intents = %+v, want the refused one consumed rather than retried", pending)
	}

	lastErr, failed, err := store.LastImportError(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !failed || lastErr.Feature != "project:y" || !strings.Contains(lastErr.Message, blocked) {
		t.Errorf("LastImportError = %+v, failed=%v, want project:y naming %s", lastErr, failed, blocked)
	}

	events, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "import_refused" && strings.Contains(e.Detail, blocked) {
			found = true
		}
	}
	if !found {
		t.Errorf("events = %+v, want an import_refused event naming %s", events, blocked)
	}
}

func TestLoopSetsTicketSourceFromTheReposConfiguredTracker(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(ctx, "project:x", "import", at); err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}

	lp := loop.NewLoop(store, noOpObserve, fixedClock(at), cfg, config.Workspace{}, runner.ProcessRunner{})
	lp.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := lp.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Source != "linear" {
		t.Fatalf("tickets = %+v, want source linear from the repo's own configured tracker", tickets)
	}
}
