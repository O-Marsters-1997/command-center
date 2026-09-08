package cc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// fakeTrackerSource answers Groups and Tickets from fixed data, so a test drives import.go's
// consumers without shelling out to gh.
type fakeTrackerSource struct {
	groups  []tracker.Group
	tickets map[string][]tracker.Ticket
}

func (f fakeTrackerSource) Groups(context.Context) ([]tracker.Group, error) { return f.groups, nil }

func (f fakeTrackerSource) Tickets(_ context.Context, group string) ([]tracker.Ticket, error) {
	return f.tickets[group], nil
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

func TestImportGroupsListsLabelsAcrossRepos(t *testing.T) {
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
		groups:  []tracker.Group{"project:x"},
		tickets: map[string][]tracker.Ticket{"project:x": {alphaTicket}},
	}
	beta := fakeTrackerSource{
		groups: []tracker.Group{"project:x", "project:y"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {betaXTicket},
			"project:y": {betaYTicket},
		},
	}
	resolve := resolveByURL(map[string]tracker.Source{
		"https://github.com/acme/alpha": alpha,
		"https://github.com/acme/beta":  beta,
	})

	got, err := cc.ImportGroups(t.Context(), repos, resolve)
	if err != nil {
		t.Fatalf("ImportGroups: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("groups = %+v, want 2", got)
	}
	if got[0].Group != "project:x" || got[1].Group != "project:y" {
		t.Errorf("group order = %q, %q, want project:x then project:y", got[0].Group, got[1].Group)
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
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
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
	if seeded.Repo != "alpha" || seeded.Source != "github" || seeded.GroupKey != "project:x" {
		t.Errorf("repo/source/group_key = %q/%q/%q", seeded.Repo, seeded.Source, seeded.GroupKey)
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

func TestLoopAppliesAPendingImportIntent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.QueueVerbIntent(ctx, "project:x", "import", at); err != nil {
		t.Fatal(err)
	}

	src := fakeTrackerSource{
		groups: []tracker.Group{"project:x"},
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

func TestHandleImportRendersEveryGroupAndItsTickets(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	src := fakeTrackerSource{
		groups: []tracker.Group{"project:x"},
		tickets: map[string][]tracker.Ticket{
			"project:x": {{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}},
		},
	}

	server := cc.NewServer(openStore(t, filepath.Join(t.TempDir(), "cc.db")), time.Now, repos, "")
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

func TestPostImportQueuesAnIntentAndRedirects(t *testing.T) {
	t.Parallel()

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"group": {"project:x"}}.Encode()
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

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	body := url.Values{"group": {"project:x"}}.Encode()
	resp, err := http.Post(srv.URL+"/import", "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 with no Origin header", resp.StatusCode)
	}
}
