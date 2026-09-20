package cc_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
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
// trackerSourceFor passes it (host/owner/repo, e.g. github.com/acme/alpha), so a multi-repo test
// can give each repo its own fixed answers.
func resolveByRemote(byRemote map[string]tracker.Source) cc.TrackerSource {
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := byRemote[remote]
		if !ok {
			return nil, fmt.Errorf("resolveByRemote: no source for %q", remote)
		}
		return src, nil
	}
}

func TestImportFeaturesListsLabelsAcrossRepos(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{
		{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
		{Name: "beta", Remote: "git@github.com:acme/beta.git"},
		{Name: "local", Path: "."}, // no remote: no tracker to dispatch to, silently skipped
	}

	var ticketCalls int
	alpha := fakeTrackerSource{features: []tracker.Feature{"project:x"}, ticketCalls: &ticketCalls}
	beta := fakeTrackerSource{features: []tracker.Feature{"project:x", "project:y"}, ticketCalls: &ticketCalls}
	resolve := resolveByRemote(map[string]tracker.Source{
		"github.com/acme/alpha": alpha,
		"github.com/acme/beta":  beta,
	})

	got, err := cc.ImportFeatures(t.Context(), repos, resolve)
	if err != nil {
		t.Fatalf("ImportFeatures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("features = %+v, want 2", got)
	}
	if got[0].Feature != "project:x" || got[1].Feature != "project:y" {
		t.Errorf("feature order = %q, %q, want project:x then project:y", got[0].Feature, got[1].Feature)
	}
	if ticketCalls != 0 {
		t.Errorf("Tickets calls = %d, want 0: listing features costs one gh label list per repo, nothing more",
			ticketCalls)
	}
}

// TestImportFeaturesDispatchesOnEachReposConfiguredTrackerKind pins the leak issue #223 closes:
// alpha and beta share a github.com remote, so a resolve keyed on the host alone would send them
// to the same tracker. Only their own Tracker config may decide that.
func TestImportFeaturesDispatchesOnEachReposConfiguredTrackerKind(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{
		{Name: "alpha", Remote: "git@github.com:acme/alpha.git", Tracker: "github"},
		{Name: "beta", Remote: "git@github.com:acme/beta.git", Tracker: "linear"},
	}

	var gotKinds []tracker.Kind
	resolve := func(kind tracker.Kind, _ string) (tracker.Source, error) {
		gotKinds = append(gotKinds, kind)
		return fakeTrackerSource{}, nil
	}

	if _, err := cc.ImportFeatures(t.Context(), repos, resolve); err != nil {
		t.Fatalf("ImportFeatures: %v", err)
	}
	want := []tracker.Kind{"github", "linear"}
	if !slices.Equal(gotKinds, want) {
		t.Errorf("kinds passed to resolve = %v, want %v", gotKinds, want)
	}
}

func TestImportTicketsRefreshesTrackerFieldsButNotBranchOrBlockedBy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	url := "https://github.com/acme/alpha/issues/1"
	blocker := "https://github.com/acme/alpha/issues/2"
	first := []cc.ImportedTicket{
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
	if err := store.UpsertTickets(ctx, []cc.Ticket{edited}); err != nil {
		t.Fatal(err)
	}

	second := []cc.ImportedTicket{{
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

// TestImportTicketsWithdrawsAndRestoresOnReimport simulates an issue dropping out of the
// tracker's results (withdrawn on reimport, run history intact) and coming back (restored on
// reimport, same history).
func TestImportTicketsWithdrawsAndRestoresOnReimport(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	kept := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}
	withdrawn := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/2", Number: 2, Title: "Add y"}

	both := []cc.ImportedTicket{{Ticket: kept, Repo: "alpha"}, {Ticket: withdrawn, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", both, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	runID, err := store.InsertRunSkeleton(ctx, withdrawn.URL, "agent", "", "hash-1")
	if err != nil {
		t.Fatal(err)
	}

	// withdrawn.URL is relabelled to status:backlog, so the next import of project:x omits it.
	onlyKept := []cc.ImportedTicket{{Ticket: kept, Repo: "alpha"}}
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

// TestImportTicketsRepairsBlockedByOnceItsBlockerWithdraws pins issue #235: blocked_by is
// otherwise write-once after a ticket's first import, so once its blocker merges and withdraws,
// nothing else ever revisits the stale edge and the dependent is stuck at blocked forever.
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
	seed := []cc.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: otherBlocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker's pull request merges.
	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := cc.Observation{PRs: map[string]gh.PR{cc.BranchKey("alpha", blockerBranch): {State: gh.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	// Its issue closes: the next import of its own feature no longer returns it, withdrawing it.
	onlyDependentAndOther := []cc.ImportedTicket{
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

// TestImportTicketsRepairsBlockedByOnceTheMergeFactCatchesUpToAnEarlierWithdrawal pins issue
// #266: a blocker can withdraw before its merged pull request fact reaches obs, and nothing
// else ever revisits that ticket once it drops out of previous \ returned on a later call.
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
	seed := []cc.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker's issue closes and withdraws before its merged pull request fact reaches obs.
	onlyDependent := []cc.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", onlyDependent, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing the blocker: %v", err)
	}

	// The merge fact lands afterwards.
	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := cc.Observation{PRs: map[string]gh.PR{cc.BranchKey("alpha", blockerBranch): {State: gh.Merged}}}
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

// TestImportTicketsRepairsBlockedByAcrossFeatures makes the same repair reach a dependent
// imported under a different feature than its blocker -- the withdrawal and the stale edge it
// leaves behind are never scoped to one feature.
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
	blockerSeed := []cc.ImportedTicket{{Ticket: blocker, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", blockerSeed, at); err != nil {
		t.Fatalf("ImportTickets blocker: %v", err)
	}

	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := cc.Observation{PRs: map[string]gh.PR{cc.BranchKey("alpha", blockerBranch): {State: gh.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	dependentSeed := []cc.ImportedTicket{{Ticket: dependent, Repo: "beta"}}
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

// TestImportTicketsLeavesBlockedByAloneWhenTheBlockerWithdrawsUnmerged pins the other half of
// issue #235: a withdrawal is not always "the blocker is gone for good" -- an issue dropped from
// the tracker's results (its project: label removed, say) withdraws the very same way a merged
// one does, but its issue stays open and the ticket can come back. Pruning blocked_by for it
// would silently satisfy a blocker that never resolved, the same failure AC3 warns against for a
// pull request closed without merging.
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
	seed := []cc.ImportedTicket{
		{Ticket: blocker, Repo: "alpha"},
		{Ticket: dependent, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	// The blocker drops off the tracker (its project: label removed, say): it drops out of the
	// next import with no merged (or any) pull request recorded for it.
	onlyDependent := []cc.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
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
	first := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	fresh := "https://github.com/acme/alpha/issues/2"
	second := []cc.ImportedTicket{
		{Ticket: tracker.Ticket{URL: fresh, Number: 2, Title: "Add y"}, Repo: "alpha"},
		{Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha"},
	}
	err := store.ImportTickets(ctx, "project:y", second, at.Add(time.Hour))
	if err == nil {
		t.Fatal("ImportTickets: want an error, got nil")
	}
	var conflict *cc.FeatureConflictError
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

// TestImportTicketsRefusesAClosureViolation pins issue #255: a feature is closed under
// blocked_by, so a ticket blocked by another feature's ticket is legal data ImportTickets must
// refuse whole, rather than import and leave the board stuck.
func TestImportTicketsRefusesAClosureViolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	outsider := "https://github.com/acme/alpha/issues/1"
	outsiderSeed := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{URL: outsider, Number: 1, Title: "Outsider"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", outsiderSeed, at); err != nil {
		t.Fatalf("ImportTickets outsider: %v", err)
	}

	blocked := "https://github.com/acme/beta/issues/2"
	fresh := "https://github.com/acme/beta/issues/3"
	second := []cc.ImportedTicket{
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
	var closure *cc.FeatureClosureError
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

// TestImportTicketsAllowsAnOutsideBlockerWhosePullRequestMerged is the closure check's own
// exemption: a blocker outside the feature does not refuse once its pull request has merged.
func TestImportTicketsAllowsAnOutsideBlockerWhosePullRequestMerged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	blocker := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Blocker"}
	blockerSeed := []cc.ImportedTicket{{Ticket: blocker, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", blockerSeed, at); err != nil {
		t.Fatalf("ImportTickets blocker: %v", err)
	}

	blockerBranch := tracker.BranchSlug(blocker.Number, blocker.Title)
	obs := cc.Observation{PRs: map[string]gh.PR{cc.BranchKey("alpha", blockerBranch): {State: gh.Merged}}}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	dependent := tracker.Ticket{
		URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Dependent",
		BlockedBy: []string{blocker.URL},
	}
	dependentSeed := []cc.ImportedTicket{{Ticket: dependent, Repo: "beta"}}
	if err := store.ImportTickets(ctx, "project:y", dependentSeed, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets dependent: want the merged outside blocker to be exempt, got %v", err)
	}
}

// TestImportTicketsRefusesABlockerNeverImported pins the other half of issue #255's closure
// check: a blocker the app has never seen at all is outside the feature and unmerged, so it
// refuses exactly like a blocker imported under another feature would.
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
	seed := []cc.ImportedTicket{{Ticket: dependent, Repo: "alpha"}}
	err := store.ImportTickets(ctx, "project:x", seed, at)
	var closure *cc.FeatureClosureError
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
	first := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{URL: relabelled, Number: 1, Title: "Add x"}, Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	if err := store.ImportTickets(ctx, "project:x", nil, at.Add(time.Hour)); err != nil {
		t.Fatalf("ImportTickets withdrawing: %v", err)
	}

	second := []cc.ImportedTicket{{
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
	cfg := cc.Config{Repos: []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}}

	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, cc.Workspace{}, cc.ProcessRunner{})
	loop.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := loop.RunOnce(ctx); err != nil {
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
	seed := []cc.ImportedTicket{{Ticket: tracker.Ticket{URL: contested, Number: 1, Title: "Add x"}, Repo: "alpha"}}
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
	cfg := cc.Config{Repos: []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}}

	loop := cc.NewLoop(store, noOpObserve, fixedClock(at.Add(time.Hour)), cfg, cc.Workspace{}, cc.ProcessRunner{})
	loop.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := loop.RunOnce(ctx); err != nil {
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

// TestLoopRecordsAClosureRefusalWithoutHaltingTheTick mirrors the feature-conflict case above for
// issue #255's closure check: applyImportIntents must catch a *FeatureClosureError the same way.
func TestLoopRecordsAClosureRefusalWithoutHaltingTheTick(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	outsider := "https://github.com/acme/alpha/issues/1"
	outsiderSeed := []cc.ImportedTicket{{
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
	cfg := cc.Config{Repos: []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}}

	loop := cc.NewLoop(store, noOpObserve, fixedClock(at.Add(time.Hour)), cfg, cc.Workspace{}, cc.ProcessRunner{})
	loop.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := loop.RunOnce(ctx); err != nil {
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

// TestLoopSetsTicketSourceFromTheReposConfiguredTracker pins issue #223's third leak: the loop
// must carry the repo's own configured tracker into the stored ticket, not a literal "github".
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
	cfg := cc.Config{
		Repos: []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git", Tracker: "linear"}},
	}

	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, cc.Workspace{}, cc.ProcessRunner{})
	loop.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	if err := loop.RunOnce(ctx); err != nil {
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

func TestHandleFeaturesListsEveryFeatureImportedOrNot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seed := []cc.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}, Repo: "alpha"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, time.Now()); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	src := fakeTrackerSource{features: []tracker.Feature{"project:x", "project:y"}}

	server := cc.NewServer(store, time.Now, repos, "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"project:x", "project:y"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "yes") > strings.Index(body, "project:y") {
		t.Errorf("project:y, which has no imported tickets, reads as imported:\n%s", body)
	}
}

func TestHandleFeaturesShowsTheLastRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	url := "https://github.com/acme/alpha/issues/1"
	seed := []cc.ImportedTicket{{Ticket: tracker.Ticket{URL: url, Number: 1, Title: "Add x"}, Repo: "alpha"}}
	if err := store.ImportTickets(ctx, "project:x", seed, time.Now()); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	conflict := &cc.FeatureConflictError{URL: url, Existing: "project:x", Importing: "project:y"}
	if err := store.RecordImportRefusal(ctx, "project:y", conflict, time.Now()); err != nil {
		t.Fatal(err)
	}

	server := cc.NewServer(store, time.Now, nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"project:y", url, "already belongs to feature"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleFeaturesFiltersByQuery(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	src := fakeTrackerSource{features: []tracker.Feature{"project:x", "project:y"}}

	server := cc.NewServer(openStore(t), time.Now, repos, "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/features?q=X", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "project:x") {
		t.Errorf("?q=X (case-insensitive) should still match project:x:\n%s", body)
	}
	if strings.Contains(body, "project:y") {
		t.Errorf("?q=X should not match project:y:\n%s", body)
	}
}

func TestHandleFeatureRedirectScopesTheBoard(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(openStore(t), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp, err := noRedirect(srv).Get(srv.URL + "/features/" + url.PathEscape("project:x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/?feature=project%3Ax" {
		t.Fatalf("Location = %q, want /?feature=project%%3Ax", got)
	}
}
