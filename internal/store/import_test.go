package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

func TestStore_ImportTickets(t *testing.T) {
	t.Parallel()

	t.Run("refreshes tracker fields but not branch or blocked_by", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		at := testNow

		url := "https://github.com/acme/alpha/issues/1"
		blocker := "https://github.com/acme/alpha/issues/2"
		first := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: blocker, Number: 2, Title: "Blocker"}, Repo: "alpha", Source: "github"},
			{
				Ticket: tracker.Ticket{
					URL: url, Number: 1, Title: "Add x", Body: "body one", Status: "ready",
					BlockedBy: []string{blocker},
				},
				Repo:   "alpha",
				Source: "github",
			},
		}
		if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}

		tickets := readTickets(t, store)
		if len(tickets) != 2 {
			t.Fatalf("Tickets() = %+v, want 2 (the ticket and its blocker)", tickets)
		}
		seeded := ticketsByURLForTest(t, tickets)[url]
		want := storepkg.Ticket{
			URL: url, Repo: "alpha", Branch: "cc-1-add-x", BlockedBy: []string{blocker}, Source: "github",
			Title: "Add x", Body: "body one", Status: "ready", Feature: "project:x", SyncedAt: at,
		}
		if diff := cmp.Diff(want, seeded); diff != "" {
			t.Errorf("Tickets()[%s] mismatch (-want +got):\n%s", url, diff)
		}

		if err := store.EditTicket(ctx, url, "cc-1-custom-branch", nil); err != nil {
			t.Fatalf("EditTicket: %v", err)
		}

		second := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{
				URL: url, Number: 1, Title: "Add x, renamed", Body: "body two", Status: "in-progress",
				BlockedBy: []string{"https://github.com/acme/alpha/issues/3"},
			},
			Repo:   "alpha",
			Source: "github",
		}}
		if err := store.ImportTickets(ctx, "project:x", second, at.Add(time.Hour)); err != nil {
			t.Fatalf("ImportTickets again: %v", err)
		}

		refreshed := readTickets(t, store)
		wantRefreshed := []storepkg.Ticket{{
			URL: url, Repo: "alpha", Branch: "cc-1-custom-branch", BlockedBy: []string{}, Source: "github",
			Title: "Add x, renamed", Body: "body two", Status: "in-progress", Feature: "project:x",
			SyncedAt: at.Add(time.Hour),
		}}
		if diff := cmp.Diff(wantRefreshed, refreshed, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Tickets() after reimport mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("keeps a queued edit ticket intent", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		st := openStore(t)
		at := testNow
		url := "https://github.com/acme/alpha/issues/1"
		seed := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{URL: url, Number: 1, Title: "Add x", Status: "ready"}, Repo: "alpha",
		}}
		if err := st.ImportTickets(ctx, "project:x", seed, at); err != nil {
			t.Fatalf("ImportTickets (seed): %v", err)
		}
		if err := st.QueueEditTicketIntent(ctx, storepkg.EditRequest{
			Ticket: url, Branch: "cc-1-custom-branch", At: at.Add(time.Minute),
		}); err != nil {
			t.Fatalf("QueueEditTicketIntent: %v", err)
		}
		again := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{URL: url, Number: 1, Title: "Add x, renamed", Status: "in-progress"}, Repo: "alpha",
		}}
		if err := st.ImportTickets(ctx, "project:x", again, at.Add(time.Hour)); err != nil {
			t.Fatalf("ImportTickets (again): %v", err)
		}
		intents, err := st.PendingEditTicketIntents(ctx)
		if err != nil {
			t.Fatalf("PendingEditTicketIntents: %v", err)
		}
		if len(intents) != 1 || intents[0].TicketID != url || intents[0].Branch != "cc-1-custom-branch" {
			t.Errorf("PendingEditTicketIntents() = %+v, want the queued branch edit for %s", intents, url)
		}
		tickets := readTickets(t, st)
		if len(tickets) != 1 || tickets[0].Title != "Add x, renamed" {
			t.Errorf("Tickets() = %+v, want the refreshed tracker-owned title", tickets)
		}
	})

	t.Run("reads synced_at back as the import clock", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
		tickets := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: "https://x/1", Number: 1, Title: "t"}, Repo: "r", Source: "github"},
		}
		if err := store.ImportTickets(t.Context(), "project:x", tickets, now); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}
		got := readTickets(t, store)
		if len(got) != 1 || !got[0].SyncedAt.Equal(now) {
			t.Errorf("Tickets() = %+v, want synced_at %v", got, now)
		}
	})

	t.Run("reads that decide a write run on the transaction's connection", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		s := openStoreAt(t, cctest.DSN(t), storepkg.MaxOpenConns(1))
		url := "https://github.com/acme/alpha/issues/1"
		tickets := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: url, Number: 1, Title: "One"}, Repo: "alpha", Source: "github"},
		}
		if err := s.ImportTickets(ctx, "project:x", tickets, testNow); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}
		if err := s.SaveObservation(ctx, plan.Observation{}); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		if err := s.ImportTickets(ctx, "project:x", tickets, testNow); err != nil {
			t.Fatalf("ImportTickets with an observation: %v", err)
		}
		if err := s.EditTicket(ctx, url, "issue-1/one", nil); err != nil {
			t.Fatalf("EditTicket: %v", err)
		}
	})

	t.Run("withdraws and restores on reimport", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		at := testNow

		kept := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}
		withdrawn := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Add y"}

		both := []storepkg.ImportedTicket{{Ticket: kept, Repo: "alpha"}, {Ticket: withdrawn, Repo: "alpha"}}
		if err := store.ImportTickets(ctx, "project:x", both, at); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}
		runID, err := store.InsertRunSkeleton(
			ctx, storepkg.RunSkeleton{Ticket: withdrawn.URL, Kind: "agent", PromptHash: "hash-1"})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}

		onlyKept := []storepkg.ImportedTicket{{Ticket: kept, Repo: "alpha"}}
		if err := store.ImportTickets(ctx, "project:x", onlyKept, at.Add(time.Hour)); err != nil {
			t.Fatalf("ImportTickets after relabelling to backlog: %v", err)
		}
		if got := ticketURLs(readTickets(t, store)); !cmp.Equal(got, []string{kept.URL}) {
			t.Fatalf("Tickets() after withdrawal = %v, want [%s]", got, kept.URL)
		}
		if diff := cmp.Diff([]int64{runID}, runIDsFor(t, store, withdrawn.URL)); diff != "" {
			t.Errorf("RunIDsForTicket(%s) after withdrawal mismatch (-want +got):\n%s", withdrawn.URL, diff)
		}

		if err := store.ImportTickets(ctx, "project:x", both, at.Add(2*time.Hour)); err != nil {
			t.Fatalf("ImportTickets after relabelling back: %v", err)
		}
		if got := readTickets(t, store); len(got) != 2 {
			t.Fatalf("Tickets() after restoration = %+v, want %s restored alongside %s", got, withdrawn.URL, kept.URL)
		}
		if diff := cmp.Diff([]int64{runID}, runIDsFor(t, store, withdrawn.URL)); diff != "" {
			t.Errorf("RunIDsForTicket(%s) after restoration mismatch (-want +got):\n%s", withdrawn.URL, diff)
		}
	})

	t.Run("blocked_by repair", func(t *testing.T) {
		t.Parallel()
		blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
		blockerMerged := plan.Observation{PRs: map[plan.BranchRef]plan.PR{
			{Repo: "alpha", Branch: tracker.BranchSlug(blocker.Number, blocker.Title)}: {State: plan.Merged},
		}}
		saveObservation := func(t *testing.T, store *storepkg.Store) {
			t.Helper()
			if err := store.SaveObservation(t.Context(), blockerMerged); err != nil {
				t.Fatalf("SaveObservation: %v", err)
			}
		}
		importOrFail := func(
			t *testing.T, store *storepkg.Store, feature string, at time.Time, ts ...storepkg.ImportedTicket,
		) {
			t.Helper()
			if err := store.ImportTickets(t.Context(), feature, ts, at); err != nil {
				t.Fatalf("ImportTickets(%s): %v", feature, err)
			}
		}

		t.Run("prunes a merged blocker once it withdraws, keeping other edges", func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			other := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/99", Number: 99, Title: "Other blocker"}
			dependent := tracker.Ticket{
				URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent",
				BlockedBy: []string{blocker.URL, other.URL},
			}
			importOrFail(t, store, "project:x", testNow,
				storepkg.ImportedTicket{Ticket: blocker, Repo: "alpha"},
				storepkg.ImportedTicket{Ticket: other, Repo: "alpha"},
				storepkg.ImportedTicket{Ticket: dependent, Repo: "alpha"})
			saveObservation(t, store)
			importOrFail(t, store, "project:x", testNow.Add(time.Hour),
				storepkg.ImportedTicket{Ticket: other, Repo: "alpha"},
				storepkg.ImportedTicket{Ticket: dependent, Repo: "alpha"})

			got := ticketsByURLForTest(t, readTickets(t, store))[dependent.URL].BlockedBy
			if diff := cmp.Diff([]string{other.URL}, got); diff != "" {
				t.Errorf("Tickets()[dependent].BlockedBy mismatch (-want +got):\n%s", diff)
			}
		})

		t.Run("prunes an earlier withdrawal once its merge fact catches up", func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			dependent := tracker.Ticket{
				URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent", BlockedBy: []string{blocker.URL},
			}
			onlyDependent := storepkg.ImportedTicket{Ticket: dependent, Repo: "alpha"}
			importOrFail(t, store, "project:x", testNow, storepkg.ImportedTicket{Ticket: blocker, Repo: "alpha"}, onlyDependent)
			importOrFail(t, store, "project:x", testNow.Add(time.Hour), onlyDependent)
			saveObservation(t, store)
			importOrFail(t, store, "project:x", testNow.Add(2*time.Hour), onlyDependent)

			tickets := readTickets(t, store)
			if len(tickets) != 1 || len(tickets[0].BlockedBy) != 0 {
				t.Errorf("Tickets() = %+v, want only the dependent with its blocker pruned", tickets)
			}
		})

		t.Run("prunes across features", func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			dependent := tracker.Ticket{
				URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Dependent", BlockedBy: []string{blocker.URL},
			}
			importOrFail(t, store, "project:x", testNow, storepkg.ImportedTicket{Ticket: blocker, Repo: "alpha"})
			saveObservation(t, store)
			importOrFail(t, store, "project:y", testNow, storepkg.ImportedTicket{Ticket: dependent, Repo: "beta"})
			importOrFail(t, store, "project:x", testNow.Add(time.Hour))

			tickets := readTickets(t, store)
			if len(tickets) != 1 || tickets[0].URL != dependent.URL || len(tickets[0].BlockedBy) != 0 {
				t.Errorf("Tickets() = %+v, want only the dependent with the cross-feature blocker pruned", tickets)
			}
		})

		t.Run("leaves an unmerged withdrawn blocker alone", func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			dependent := tracker.Ticket{
				URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent", BlockedBy: []string{blocker.URL},
			}
			onlyDependent := storepkg.ImportedTicket{Ticket: dependent, Repo: "alpha"}
			importOrFail(t, store, "project:x", testNow, storepkg.ImportedTicket{Ticket: blocker, Repo: "alpha"}, onlyDependent)
			importOrFail(t, store, "project:x", testNow.Add(time.Hour), onlyDependent)

			tickets := readTickets(t, store)
			if len(tickets) != 1 {
				t.Fatalf("Tickets() = %+v, want only the dependent", tickets)
			}
			if diff := cmp.Diff([]string{blocker.URL}, tickets[0].BlockedBy); diff != "" {
				t.Errorf("Tickets()[dependent].BlockedBy mismatch (-want +got):\n%s", diff)
			}
		})

		t.Run("allows an outside blocker whose pull request merged", func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			importOrFail(t, store, "project:x", testNow, storepkg.ImportedTicket{Ticket: blocker, Repo: "alpha"})
			saveObservation(t, store)
			dependent := tracker.Ticket{
				URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Dependent", BlockedBy: []string{blocker.URL},
			}
			importOrFail(t, store, "project:y", testNow.Add(time.Hour), storepkg.ImportedTicket{Ticket: dependent, Repo: "beta"})
		})
	})

	t.Run("allows a withdrawn ticket into a new feature", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		relabelled := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}, Repo: "alpha",
		}}
		if err := store.ImportTickets(ctx, "project:x", relabelled, testNow); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}
		if err := store.ImportTickets(ctx, "project:x", nil, testNow.Add(time.Hour)); err != nil {
			t.Fatalf("ImportTickets withdrawing: %v", err)
		}
		if err := store.ImportTickets(ctx, "project:y", relabelled, testNow.Add(2*time.Hour)); err != nil {
			t.Fatalf("ImportTickets(project:y) error = %v, want a withdrawn ticket to import cleanly", err)
		}
		tickets := readTickets(t, store)
		if len(tickets) != 1 || tickets[0].Feature != "project:y" {
			t.Errorf("Tickets() = %+v, want the relabelled ticket restored under project:y", tickets)
		}
	})

	t.Run("refuses a feature conflict and rolls back", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)

		contested := "https://github.com/acme/alpha/issues/1"
		first := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha",
		}}
		if err := store.ImportTickets(ctx, "project:x", first, testNow); err != nil {
			t.Fatalf("ImportTickets: %v", err)
		}
		second := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Add y"}, Repo: "alpha"},
			{Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha"},
		}
		err := store.ImportTickets(ctx, "project:y", second, testNow.Add(time.Hour))
		conflict, ok := errors.AsType[*storepkg.FeatureConflictError](err)
		if !ok {
			t.Fatalf("ImportTickets(project:y) error = %v, want a *FeatureConflictError", err)
		}
		want := storepkg.FeatureConflictError{URL: contested, Existing: "project:x", Importing: "project:y"}
		if diff := cmp.Diff(want, *conflict); diff != "" {
			t.Errorf("ImportTickets(project:y) conflict mismatch (-want +got):\n%s", diff)
		}
		tickets := readTickets(t, store)
		if len(tickets) != 1 || tickets[0].Feature != "project:x" {
			t.Errorf("Tickets() = %+v, want only the original row under project:x", tickets)
		}
	})

	t.Run("refuses a closure violation and rolls back", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)

		outsider := "https://github.com/acme/alpha/issues/1"
		outsiderSeed := []storepkg.ImportedTicket{{
			Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha",
		}}
		if err := store.ImportTickets(ctx, "project:x", outsiderSeed, testNow); err != nil {
			t.Fatalf("ImportTickets outsider: %v", err)
		}
		blocked := "https://github.com/acme/beta/issues/2"
		second := []storepkg.ImportedTicket{
			{Ticket: tracker.Ticket{URL: "https://github.com/acme/beta/issues/3", Number: 3, Title: "Add z"}, Repo: "beta"},
			{Ticket: tracker.Ticket{URL: blocked, Number: 2, Title: "Add y", BlockedBy: []string{outsider}}, Repo: "beta"},
		}
		err := store.ImportTickets(ctx, "project:y", second, testNow.Add(time.Hour))
		closure, ok := errors.AsType[*storepkg.FeatureClosureError](err)
		if !ok {
			t.Fatalf("ImportTickets(project:y) error = %v, want a *FeatureClosureError", err)
		}
		want := storepkg.FeatureClosureError{
			Feature: "project:y", URL: blocked, Blocker: outsider, BlockerFeature: "project:x",
		}
		if diff := cmp.Diff(want, *closure); diff != "" {
			t.Errorf("ImportTickets(project:y) closure mismatch (-want +got):\n%s", diff)
		}
		if got := ticketURLs(readTickets(t, store)); !cmp.Equal(got, []string{outsider}) {
			t.Errorf("Tickets() = %v, want only [%s]", got, outsider)
		}
	})

	t.Run("refuses a blocker never imported", func(t *testing.T) {
		t.Parallel()
		unseen := "https://github.com/acme/alpha/issues/1"
		dependent := tracker.Ticket{
			URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent", BlockedBy: []string{unseen},
		}
		imported := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
		err := openStore(t).ImportTickets(t.Context(), "project:x", imported, testNow)
		closure, ok := errors.AsType[*storepkg.FeatureClosureError](err)
		if !ok {
			t.Fatalf("ImportTickets() error = %v, want a *FeatureClosureError", err)
		}
		want := storepkg.FeatureClosureError{Feature: "project:x", URL: dependent.URL, Blocker: unseen}
		if diff := cmp.Diff(want, *closure); diff != "" {
			t.Errorf("ImportTickets() closure mismatch (-want +got):\n%s", diff)
		}
	})
}

func readTickets(t *testing.T, store *storepkg.Store) []storepkg.Ticket {
	t.Helper()
	tickets, err := store.Tickets(t.Context())
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	return tickets
}

func ticketURLs(tickets []storepkg.Ticket) []string {
	urls := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		urls = append(urls, ticket.URL)
	}
	return urls
}

func runIDsFor(t *testing.T, store *storepkg.Store, ticketURL string) []int64 {
	t.Helper()
	ids, err := store.RunIDsForTicket(t.Context(), ticketURL)
	if err != nil {
		t.Fatalf("RunIDsForTicket(%s): %v", ticketURL, err)
	}
	return ids
}

func ticketsByURLForTest(t *testing.T, tickets []storepkg.Ticket) map[string]storepkg.Ticket {
	t.Helper()
	byURL := make(map[string]storepkg.Ticket, len(tickets))
	for _, ticket := range tickets {
		byURL[ticket.URL] = ticket
	}
	return byURL
}
