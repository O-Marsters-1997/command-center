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
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// fakeTrackerSource answers Features and Tickets from fixed data, so a test drives import.go's
// consumers without shelling out to gh.
type fakeTrackerSource struct {
	features []tracker.Feature
	tickets  map[string][]tracker.Ticket
}

func (f fakeTrackerSource) Features(context.Context) ([]tracker.Feature, error) {
	return f.features, nil
}

func (f fakeTrackerSource) Tickets(_ context.Context, feature string) ([]tracker.Ticket, error) {
	return f.tickets[feature], nil
}

// resolveByURL builds a TrackerSource that dispatches on the pseudo-url trackerSourceFor
// constructs from a repo's own remote (https://<host>/<owner>/<repo>), so a multi-repo test can
// give each repo its own fixed answers.
func resolveByURL(byURL map[string]tracker.Source) cc.TrackerSource {
	return func(ticketURL string) (tracker.Source, error) {
		src, ok := byURL[ticketURL]
		if !ok {
			return nil, fmt.Errorf("resolveByURL: no source for %q", ticketURL)
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

	alphaTicket := tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}
	betaXTicket := tracker.Ticket{URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "Add y"}
	betaYTicket := tracker.Ticket{URL: "https://github.com/acme/beta/issues/3", Number: 3, Title: "Add z"}

	alpha := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets:  map[string][]tracker.Ticket{"project:x": {alphaTicket}},
	}
	beta := fakeTrackerSource{
		features: []tracker.Feature{"project:x", "project:y"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {betaXTicket},
			"project:y": {betaYTicket},
		},
	}
	resolve := resolveByURL(map[string]tracker.Source{
		"https://github.com/acme/alpha": alpha,
		"https://github.com/acme/beta":  beta,
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
	if len(got[0].Tickets) != 2 {
		t.Fatalf("project:x tickets = %+v, want one from each of alpha and beta", got[0].Tickets)
	}
	if len(got[1].Tickets) != 1 || got[1].Tickets[0].URL != betaYTicket.URL {
		t.Errorf("project:y tickets = %+v, want just beta's own", got[1].Tickets)
	}
}

func TestRepoForTicketURL(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{
		{Name: "command-center", Remote: "git@github.com:O-Marsters-1997/command-center.git"},
	}

	tests := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"https form", "https://github.com/O-Marsters-1997/command-center/issues/105", "command-center", true},
		{"different case", "https://GitHub.com/O-Marsters-1997/Command-Center/issues/1", "command-center", true},
		{"unrelated repo", "https://github.com/other/repo/issues/1", "", false},
		{"unparseable", "not a url \x7f", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := cc.RepoForTicketURL(tt.url, repos)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("repoForTicketURL(%q) = %q, %v; want %q, %v", tt.url, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestImportTicketsRefreshesTrackerFieldsButNotBranchOrBlockedBy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	url := "https://github.com/acme/alpha/issues/1"
	first := []cc.ImportedTicket{{
		Ticket: tracker.Ticket{
			URL: url, Number: 1, Title: "Add x", Body: "body one", Status: "ready",
			BlockedBy: []string{"https://github.com/acme/alpha/issues/2"},
		},
		Repo: "alpha",
	}}
	if err := store.ImportTickets(ctx, "project:x", first, at); err != nil {
		t.Fatalf("ImportTickets: %v", err)
	}

	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("tickets = %+v, want 1", tickets)
	}
	seeded := tickets[0]
	if seeded.Branch != "cc-1-add-x" {
		t.Errorf("branch = %q, want cc-1-add-x", seeded.Branch)
	}
	if !slices.Equal(seeded.BlockedBy, []string{"https://github.com/acme/alpha/issues/2"}) {
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
		Repo: "alpha",
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

// TestImportTicketsWithdrawsAndRestoresOnReimport simulates relabelling an issue to
// status:backlog (withdrawn on reimport, run history intact) and back to status:ready
// (restored on reimport, same history).
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
	loop.SetTrackerSource(resolveByURL(map[string]tracker.Source{"https://github.com/acme/alpha": src}))
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
	loop.SetTrackerSource(resolveByURL(map[string]tracker.Source{"https://github.com/acme/alpha": src}))
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

func TestHandleImportRendersEveryFeatureAndItsTickets(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	src := fakeTrackerSource{
		features: []tracker.Feature{"project:x"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}},
		},
	}

	server := cc.NewServer(openStore(t), time.Now, repos, "")
	server.SetTrackerSource(resolveByURL(map[string]tracker.Source{"https://github.com/acme/alpha": src}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/import", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"project:x", "https://github.com/acme/alpha/issues/1", "Add x"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleImportShowsTheLastRefusalWithoutEvents(t *testing.T) {
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
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/import", nil))
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

func TestPostImportQueuesAnIntentAndRedirects(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"feature": {"project:x"}}.Encode()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/import", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/import" {
		t.Fatalf("Location = %q, want /import", got)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "import")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TicketID != "project:x" {
		t.Fatalf("pending import intents = %+v, want one naming project:x", pending)
	}
}

func TestPostImportRequiresBrowserOrigin(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"feature": {"project:x"}}.Encode()
	resp, err := http.Post(srv.URL+"/import", "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 with no Origin header", resp.StatusCode)
	}
}
