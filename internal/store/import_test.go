package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func TestImportTicketsRefreshesTrackerFieldsButNotBranchOrBlockedBy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

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

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("tickets = %+v, want 2 (the ticket and its blocker)", tickets)
	}
	seeded := ticketsByURLForTest(t, tickets)[url]
	if seeded.Branch != "cc-1-add-x" {
		t.Errorf("branch = %q, want cc-1-add-x", seeded.Branch)
	}
	if !slices.Equal(seeded.BlockedBy, []string{blocker}) {
		t.Errorf("blocked_by = %v", seeded.BlockedBy)
	}
	if seeded.Repo != "alpha" || seeded.Source != "github" || seeded.Feature != "project:x" {
		t.Errorf("repo/source/feature = %q/%q/%q", seeded.Repo, seeded.Source, seeded.Feature)
	}

	// An operator hand-edits the branch and clears the blockers before the next import lands.
	edited := seeded
	edited.Branch = "cc-1-custom-branch"
	edited.BlockedBy = nil
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{edited}); err != nil {
		t.Fatal(err)
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

	tickets, err = store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("tickets = %+v, want the same url upserted, not a second row", tickets)
	}
	refreshed := tickets[0]
	if refreshed.Title != "Add x, renamed" || refreshed.Body != "body two" || refreshed.Status != "in-progress" {
		t.Errorf("title/body/status = %q/%q/%q, want the re-imported values",
			refreshed.Title, refreshed.Body, refreshed.Status)
	}
	if refreshed.Branch != "cc-1-custom-branch" {
		t.Errorf("branch = %q, want the hand-edited one to survive re-import", refreshed.Branch)
	}
	if len(refreshed.BlockedBy) != 0 {
		t.Errorf("blocked_by = %v, want the cleared blockers to survive re-import", refreshed.BlockedBy)
	}
}

func TestImportTicketsWithdrawsAndRestoresOnReimport(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	kept := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}
	withdrawn := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Add y"}

	both := []storepkg.ImportedTicket{{Ticket: kept, Repo: "alpha"}, {Ticket: withdrawn, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", both, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	runID, err := store.InsertRunSkeleton(ctx, withdrawn.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}

	// withdrawn.URL is relabelled to status:backlog, so the next import of project:x omits it.
	onlyKept := []storepkg.ImportedTicket{{Ticket: kept, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", onlyKept, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets after relabelling to backlog: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != kept.URL {
		t.Fatalf("tickets = %+v, want only %s left on the board", tickets, kept.URL)
	}

	runIDs, err := store.RunIDsForTicket(ctx, withdrawn.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 1 || runIDs[0] != runID {
		t.Errorf("run ids for %s = %v, want the run to survive withdrawal", withdrawn.URL, runIDs)
	}

	// withdrawn.URL is relabelled back to status:ready: the next import returns it again.
	if err := store.ImportTickets(ctx, "project:x", both, at.Add(2*time.Hour)); err != nil {
		t.Fatalf("ImportTickets after relabelling back: %v", err)
	}

	tickets, err = store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("tickets = %+v, want %s restored alongside %s", tickets, withdrawn.URL, kept.URL)
	}

	runIDs, err = store.RunIDsForTicket(ctx, withdrawn.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 1 || runIDs[0] != runID {
		t.Errorf("run ids for %s = %v, want the restored ticket's run history intact", withdrawn.URL, runIDs)
	}
}

func TestImportTicketsRepairsBlockedByOnceItsBlockerWithdraws(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	otherBlocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/99", Number: 99, Title: "Other blocker"}
	dependent := tracker.Ticket{
		URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL, otherBlocker.URL},
	}
	seed := []storepkg.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: otherBlocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker's pull request merges.
	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := plan.Observation{PRs: map[string]plan.PR{plan.BranchKey("alpha", blockerBranch): {State: plan.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	// Its issue closes: the next import of its own feature no longer returns it, withdrawing it.
	onlyDependentAndOther := []storepkg.ImportedTicket{
		{Ticket: otherBlocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", onlyDependentAndOther, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing the blocker: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("tickets = %+v, want the dependent and otherBlocker left on the board", tickets)
	}
	dependentRow := ticketsByURLForTest(t, tickets)[dependent.URL]
	want := []string{otherBlocker.URL}
	if !slices.Equal(dependentRow.BlockedBy, want) {
		t.Errorf("blocked_by = %v, want %v (the withdrawn blocker pruned, the other edge kept)",
			dependentRow.BlockedBy, want)
	}
}

func TestImportTicketsRepairsBlockedByOnceTheMergeFactCatchesUpToAnEarlierWithdrawal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	dependent := tracker.Ticket{
		URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL},
	}
	seed := []storepkg.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker's issue closes and withdraws before its merged pull request fact reaches obs.
	onlyDependent := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", onlyDependent, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing the blocker: %v", err)
	}

	// The merge fact lands afterwards.
	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := plan.Observation{PRs: map[string]plan.PR{plan.BranchKey("alpha", blockerBranch): {State: plan.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	// A later reimport of the same feature returns exactly what it already had -- nothing
	// transitions from present to absent this time, only the earlier withdrawal is now merged.
	if err := store.ImportTickets(ctx, "project:x", onlyDependent, at.Add(2*time.Hour)); err != nil {
		t.Fatalf("ImportTickets reimporting after the merge fact lands: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != dependent.URL {
		t.Fatalf("tickets = %+v, want only the dependent left on the board", tickets)
	}
	if len(tickets[0].BlockedBy) != 0 {
		t.Errorf("blocked_by = %v, want the now-merged withdrawal pruned on the later reimport",
			tickets[0].BlockedBy)
	}
}

func TestImportTicketsRepairsBlockedByAcrossFeatures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	dependent := tracker.Ticket{
		URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL},
	}
	blockerSeed := []storepkg.ImportedTicket{{Ticket: blocker, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", blockerSeed, at); err != nil {
		t.Fatalf("ImportTickets blocker: %v", err)
	}

	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := plan.Observation{PRs: map[string]plan.PR{plan.BranchKey("alpha", blockerBranch): {State: plan.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	dependentSeed := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "beta"}}
	if err := store.ImportTickets(ctx, "project:y", dependentSeed, at); err != nil {
		t.Fatalf("ImportTickets dependent: %v", err)
	}

	// project:x's next import withdraws the blocker; project:y is never re-imported.
	if err := store.ImportTickets(ctx, "project:x", nil, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing the blocker: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != dependent.URL {
		t.Fatalf("tickets = %+v, want only the dependent left on the board", tickets)
	}
	if len(tickets[0].BlockedBy) != 0 {
		t.Errorf("blocked_by = %v, want the cross-feature blocker pruned too", tickets[0].BlockedBy)
	}
}

func TestImportTicketsLeavesBlockedByAloneWhenTheBlockerWithdrawsUnmerged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	dependent := tracker.Ticket{
		URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL},
	}
	seed := []storepkg.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker drops off the tracker (its project: label removed, say): it drops out of the
	// next import with no merged (or any) pull request recorded for it.
	onlyDependent := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", onlyDependent, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing the blocker: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != dependent.URL {
		t.Fatalf("tickets = %+v, want only the dependent left on the board", tickets)
	}
	want := []string{blocker.URL}
	if !slices.Equal(tickets[0].BlockedBy, want) {
		t.Errorf("blocked_by = %v, want %v (an unmerged withdrawal must not clobber it)", tickets[0].BlockedBy, want)
	}
}

func TestImportTicketsRefusesAFeatureConflict(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	contested := "https://github.com/acme/alpha/issues/1"
	first := []storepkg.ImportedTicket{{
		Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	fresh := "https://github.com/acme/alpha/issues/2"
	second := []storepkg.ImportedTicket{
		{Ticket: tracker.Ticket{URL: fresh, Number: 2, Title: "Add y"}, Repo: "alpha"},
		{Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha"},
	}
	err := store.ImportTickets(ctx, "project:y", second, at.Add(time.Hour))
	if err == nil {
		t.Fatal("ImportTickets: want an error, got nil")
	}
	var conflict *storepkg.FeatureConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ImportTickets error = %v, want a *FeatureConflictError", err)
	}
	if conflict.URL != contested || conflict.Existing != "project:x" || conflict.Importing != "project:y" {
		t.Errorf("conflict = %+v, want %s naming project:x and project:y", conflict, contested)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Feature != "project:x" {
		t.Fatalf("tickets = %+v, want only the original row, still under project:x, and fresh rolled back", tickets)
	}
}

func TestImportTicketsRefusesAClosureViolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	outsider := "https://github.com/acme/alpha/issues/1"
	outsiderSeed := []storepkg.ImportedTicket{{
		Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", outsiderSeed, at); err != nil {
		t.Fatalf("ImportTickets outsider: %v", err)
	}

	blocked := "https://github.com/acme/beta/issues/2"
	fresh := "https://github.com/acme/beta/issues/3"
	second := []storepkg.ImportedTicket{
		{Ticket: tracker.Ticket{URL: fresh, Number: 3, Title: "Add z"}, Repo: "beta"},
		{
			Ticket: tracker.Ticket{URL: blocked, Number: 2, Title: "Add y", BlockedBy: []string{outsider}},
			Repo:   "beta",
		},
	}
	err := store.ImportTickets(ctx, "project:y", second, at.Add(time.Hour))
	if err == nil {
		t.Fatal("ImportTickets: want an error, got nil")
	}
	var closure *storepkg.FeatureClosureError
	if !errors.As(err, &closure) {
		t.Fatalf("ImportTickets error = %v, want a *FeatureClosureError", err)
	}
	wantsFields := closure.URL == blocked && closure.Blocker == outsider &&
		closure.BlockerFeature == "project:x" && closure.Feature == "project:y"
	if !wantsFields {
		t.Errorf("closure = %+v, want %s blocked by %s (project:x), importing project:y", closure, blocked, outsider)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].URL != outsider {
		t.Fatalf("tickets = %+v, want only the original outsider row, project:y rolled back whole", tickets)
	}
}

func TestImportTicketsAllowsAnOutsideBlockerWhosePullRequestMerged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	blockerSeed := []storepkg.ImportedTicket{{Ticket: blocker, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", blockerSeed, at); err != nil {
		t.Fatalf("ImportTickets blocker: %v", err)
	}

	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := plan.Observation{PRs: map[string]plan.PR{plan.BranchKey("alpha", blockerBranch): {State: plan.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	dependent := tracker.Ticket{
		URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL},
	}
	dependentSeed := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "beta"}}
	if err := store.ImportTickets(ctx, "project:y", dependentSeed, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets dependent: want the merged outside blocker to be exempt, got %v", err)
	}
}

func TestImportTicketsRefusesABlockerNeverImported(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	unseen := "https://github.com/acme/alpha/issues/1"
	dependent := tracker.Ticket{
		URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{unseen},
	}
	seed := []storepkg.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
	err := store.ImportTickets(ctx, "project:x", seed, at)
	var closure *storepkg.FeatureClosureError
	if !errors.As(err, &closure) {
		t.Fatalf("ImportTickets error = %v, want a *FeatureClosureError", err)
	}
	if closure.Blocker != unseen || closure.BlockerFeature != "" {
		t.Errorf("closure = %+v, want %s naming no feature (never imported)", closure, unseen)
	}
}

func TestImportTicketsAllowsAWithdrawnTicketIntoANewFeature(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	relabelled := "https://github.com/acme/alpha/issues/1"
	first := []storepkg.ImportedTicket{{
		Ticket: tracker.Ticket{URL: relabelled, Number: 1, Title: "Add x"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	if err := store.ImportTickets(ctx, "project:x", nil, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing: %v", err)
	}

	second := []storepkg.ImportedTicket{{
		Ticket: tracker.Ticket{URL: relabelled, Number: 1, Title: "Add x"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:y", second, at.Add(2*time.Hour)); err != nil {
		t.Fatalf("ImportTickets into project:y: want a withdrawn ticket to import cleanly, got %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Feature != "project:y" {
		t.Fatalf("tickets = %+v, want the relabelled ticket restored under project:y", tickets)
	}
}

func ticketsByURLForTest(t *testing.T, tickets []storepkg.Ticket) map[string]storepkg.Ticket {
	t.Helper()
	byURL := make(map[string]storepkg.Ticket, len(tickets))
	for _, ticket := range tickets {
		byURL[ticket.URL] = ticket
	}
	return byURL
}
