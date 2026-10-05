package cc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// TestFeatureSearchEmptyQueryListsExactlyTheKnownRepos covers the repos page's own default load
// and the search fragment's own empty-query behaviour, which must agree.
func TestFeatureSearchEmptyQueryListsExactlyTheKnownRepos(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	server := cc.NewServer(openStore(t), time.Now, repos, "")
	calls := 0
	server.SetPushableReposSource(func(context.Context) ([]gh.RepoSummary, error) {
		calls++
		return nil, nil
	})

	page := renderPath(t, server, "/features")
	if !strings.Contains(page, ">alpha<") {
		t.Errorf("repos page missing the known repo:\n%s", page)
	}

	fragment := renderPath(t, server, "/features/search?q=")
	if !strings.Contains(fragment, ">alpha<") {
		t.Errorf("empty-query search fragment missing the known repo:\n%s", fragment)
	}
	if calls != 0 {
		t.Errorf("an empty query made %d gh calls, want 0: the known list needs no GitHub read", calls)
	}
}

// TestFeatureSearchListsPushableMatchesAndDropsReadOnly covers the acceptance criterion that a
// repo the box can only read never appears.
func TestFeatureSearchListsPushableMatchesAndDropsReadOnly(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(openStore(t), time.Now, nil, "")
	server.SetPushableReposSource(func(context.Context) ([]gh.RepoSummary, error) {
		return []gh.RepoSummary{{FullName: "acme/api-gateway", SSHURL: "git@github.com:acme/api-gateway.git"}}, nil
	})

	fragment := renderPath(t, server, "/features/search?q=api")
	if !strings.Contains(fragment, "acme/api-gateway") {
		t.Errorf("search fragment missing the pushable match:\n%s", fragment)
	}
	if !strings.Contains(fragment, "not tracked") {
		t.Errorf("an untracked pushable match should read not tracked:\n%s", fragment)
	}
	if !strings.Contains(fragment, `href="/features?repo=acme/api-gateway">track`) {
		t.Errorf("search fragment missing the track link:\n%s", fragment)
	}
}

// TestFeatureSearchCachesPushableReposWithinTheTTL covers "repeated keystrokes within 60s make one
// gh call."
func TestFeatureSearchCachesPushableReposWithinTheTTL(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(openStore(t), fixedClock(at), nil, "")
	calls := 0
	server.SetPushableReposSource(func(context.Context) ([]gh.RepoSummary, error) {
		calls++
		return []gh.RepoSummary{{FullName: "acme/api-gateway", SSHURL: "git@github.com:acme/api-gateway.git"}}, nil
	})

	renderPath(t, server, "/features/search?q=a")
	renderPath(t, server, "/features/search?q=ap")
	renderPath(t, server, "/features/search?q=api")
	if calls != 1 {
		t.Errorf("gh calls = %d, want 1 across three keystrokes inside the 60s cache window", calls)
	}
}

// TestScopedPageListsOnlyThisRepoFeaturesAndNamesOtherRepos covers the scoped page's own feature
// list and its cross-repo note.
func TestScopedPageListsOnlyThisRepoFeaturesAndNamesOtherRepos(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seed := []cc.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}, Repo: "alpha"},
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/beta/issues/9", Number: 9, Title: "Add x too"}, Repo: "beta"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, time.Now()); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	repos := []cc.Repo{
		{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
		{Name: "beta", Remote: "git@github.com:acme/beta.git"},
	}
	src := fakeTrackerSource{features: []tracker.Feature{"project:x"}}
	server := cc.NewServer(store, time.Now, repos, "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{
		"github.com/acme/alpha": src, "github.com/acme/beta": src,
	}))

	page := renderPath(t, server, "/features?repo=alpha")
	if !strings.Contains(page, "project:x") {
		t.Errorf("scoped page missing its own tracker's feature:\n%s", page)
	}
	if !strings.Contains(page, "also in") || !strings.Contains(page, "beta") {
		t.Errorf("scoped page should name beta as the feature's other repo:\n%s", page)
	}
}

// TestScopedPageUnknownRepoShowsNotTrackedBanner covers an unrecognised ?repo= on the scoped page.
func TestScopedPageUnknownRepoShowsNotTrackedBanner(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(openStore(t), time.Now, nil, "")
	page := renderPath(t, server, "/features?repo=acme/unknown")
	if !strings.Contains(page, "acme/unknown") {
		t.Errorf("scoped page missing the repo name in its title:\n%s", page)
	}
	if !strings.Contains(page, "Not tracked.") {
		t.Errorf("an unknown repo should show the not-tracked banner:\n%s", page)
	}
}

// TestScopedPageKnownRepoNamesItsRemoteAndBoardLink covers the title/remote/board-link parts of
// the scoped page.
func TestScopedPageKnownRepoNamesItsRemoteAndBoardLink(t *testing.T) {
	t.Parallel()

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	server := cc.NewServer(openStore(t), time.Now, repos, "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": fakeTrackerSource{}}))

	page := renderPath(t, server, "/features?repo=alpha")
	if !strings.Contains(page, "git@github.com:acme/alpha.git") {
		t.Errorf("scoped page missing the repo's remote:\n%s", page)
	}
	if !strings.Contains(page, `href="/?repo=alpha">board`) {
		t.Errorf("scoped page missing the board link:\n%s", page)
	}
}

const (
	goldenRepos            = "testdata/repos.golden.html"
	goldenReposSearchKnown = "testdata/repos_search_known.golden.html"
	goldenReposSearchMatch = "testdata/repos_search_match.golden.html"
	goldenRepoScoped       = "testdata/repo_scoped.golden.html"
)

// reposGoldenServer seeds one known repo (alpha, ready) and one feature with a cross-repo note,
// the fixture every repos/search/scoped golden below renders from.
func reposGoldenServer(t *testing.T) *cc.Server {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	seed := []cc.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "Add x"}, Repo: "alpha"},
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/beta/issues/9", Number: 9, Title: "Add x too"}, Repo: "beta"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, time.Now()); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	repos := []cc.Repo{{Name: "alpha", Remote: "git@github.com:acme/alpha.git"}}
	src := fakeTrackerSource{features: []tracker.Feature{"project:x"}}
	server := cc.NewServer(store, time.Now, repos, "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{"github.com/acme/alpha": src}))
	server.SetPushableReposSource(func(context.Context) ([]gh.RepoSummary, error) {
		return []gh.RepoSummary{
			{FullName: "acme/alpha", SSHURL: "git@github.com:acme/alpha.git"},
			{FullName: "acme/widgets", SSHURL: "git@github.com:acme/widgets.git"},
		}, nil
	})
	return server
}

// TestReposPageGolden pins the unscoped repos page's own markup.
func TestReposPageGolden(t *testing.T) {
	t.Parallel()
	assertGolden(t, goldenRepos, []byte(renderPath(t, reposGoldenServer(t), "/features")))
}

// TestReposSearchGoldens pins both search states: an empty query (the known repos, unchanged) and
// a query matching both a known and an untracked pushable repo.
func TestReposSearchGoldens(t *testing.T) {
	t.Parallel()

	server := reposGoldenServer(t)
	assertGolden(t, goldenReposSearchKnown, []byte(renderPath(t, server, "/features/search?q=")))
	assertGolden(t, goldenReposSearchMatch, []byte(renderPath(t, server, "/features/search?q=a")))
}

// TestRepoScopedPageGolden pins the scoped repo page's own markup, including the cross-repo note.
func TestRepoScopedPageGolden(t *testing.T) {
	t.Parallel()
	assertGolden(t, goldenRepoScoped, []byte(renderPath(t, reposGoldenServer(t), "/features?repo=alpha")))
}
