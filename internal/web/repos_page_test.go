package web_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

var (
	alphaRepo = storepkg.Repo{Name: "acme/alpha", Remote: "git@github.com:acme/alpha.git"}
	betaRepo  = storepkg.Repo{Name: "acme/beta", Remote: "https://github.com/acme/beta.git"}
)

var pushable = []gh.RepoSummary{
	{FullName: "acme/alpha", SSHURL: "git@github.com:acme/alpha.git", DefaultBranch: "main"},
	{FullName: "acme/api", SSHURL: "git@github.com:acme/api.git", DefaultBranch: "main"},
	{FullName: "acme/payments-api", SSHURL: "git@github.com:acme/payments-api.git", DefaultBranch: "master"},
}

func reposServer(t *testing.T, calls *atomic.Int32) *web.Server {
	t.Helper()
	server := openServer(track(t, openStore(t), alphaRepo, betaRepo), fixedClock(testNow), "")
	server.SetPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
		calls.Add(1)
		return pushable, nil
	})
	return server
}

func TestReposPageListsExactlyTheKnownRepos(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	for _, path := range []string{"/repos", "/repos/search", "/repos/search?q="} {
		body := renderPath(t, server, path)
		for _, want := range []string{"acme/alpha", "acme/beta"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s missing known repo %s:\n%s", path, want, body)
			}
		}
		if strings.Contains(body, "acme/api") || strings.Contains(body, "not tracked") {
			t.Errorf("GET %s listed a repo the app does not know:\n%s", path, body)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("empty queries made %d gh calls, want 0", got)
	}
}

func TestRepoSearchMergesPushableMatchesAndNeverShowsReadOnlyRepos(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/search?q=API")
	for _, want := range []string{"acme/api", "acme/payments-api", "not tracked", `href="/repos/acme/api"`} {
		if !strings.Contains(body, want) {
			t.Errorf("search for API missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "acme/ro") {
		t.Errorf("search listed a repo gh never returned as pushable:\n%s", body)
	}

	tracked := renderPath(t, server, "/repos/search?q=alpha")
	if !strings.Contains(tracked, "acme/alpha") || strings.Contains(tracked, "not tracked") {
		t.Errorf("a pushable repo that is already tracked must show once, tracked:\n%s", tracked)
	}
	if strings.Count(tracked, "acme/alpha") != 2 {
		t.Errorf("acme/alpha rendered a duplicate row:\n%s", tracked)
	}
}

func TestRepoSearchMarksARepoWhoseDefaultBranchIsNotMain(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/search?q=api")
	paymentsRow, apiRow := searchRow(t, body, "acme/payments-api"), searchRow(t, body, "acme/api")
	if !strings.Contains(paymentsRow, "will be refused: default branch is <code>master</code>") {
		t.Errorf("master-default repo is not marked:\n%s", paymentsRow)
	}
	if !strings.Contains(paymentsRow, `<a href="/repos/acme/payments-api">track &rarr;</a>`) {
		t.Errorf("marked row lost its track link:\n%s", paymentsRow)
	}
	if strings.Contains(apiRow, "will be refused") {
		t.Errorf("main-default repo is marked:\n%s", apiRow)
	}
}

func searchRow(t *testing.T, body, fullName string) string {
	t.Helper()
	for row := range strings.SplitSeq(body, "<tr>") {
		if strings.Contains(row, ">"+fullName+"</a>") {
			return row
		}
	}
	t.Fatalf("no search row for %s:\n%s", fullName, body)
	return ""
}

func TestRepoSearchMakesOneGhCallWithinTheCacheWindow(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	for _, q := range []string{"a", "ap", "api"} {
		renderPath(t, server, "/repos/search?q="+q)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("three keystrokes made %d gh calls, want 1", got)
	}
}

func TestRepoSearchRefetchesOnceTheCacheExpires(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	clock := &steppingClock{at: testNow}
	server := openServer(openStore(t), clock, "")
	server.SetPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
		calls.Add(1)
		return pushable, nil
	})

	renderPath(t, server, "/repos/search?q=api")
	clock.at = clock.at.Add(61 * time.Second)
	renderPath(t, server, "/repos/search?q=api")
	if got := calls.Load(); got != 2 {
		t.Errorf("gh calls across the 60s window = %d, want 2", got)
	}
}

type steppingClock struct{ at time.Time }

func (c *steppingClock) Now() time.Time                       { return c.at }
func (*steppingClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func TestRepoSearchFallsBackToKnownReposWhenGhFails(t *testing.T) {
	t.Parallel()

	server := openServer(track(t, openStore(t), alphaRepo), fixedClock(testNow), "")
	server.SetPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
		return nil, errors.New("gh: not logged in")
	})

	body := renderPath(t, server, "/repos/search?q=alpha")
	for _, want := range []string{"acme/alpha", "gh search failed", "not logged in"} {
		if !strings.Contains(body, want) {
			t.Errorf("failed search missing %q:\n%s", want, body)
		}
	}
}

func TestScopedRepoPageListsOnlyThisReposFeaturesAndNamesOtherRepos(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seed := []storepkg.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/alpha/issues/1", Number: 1, Title: "a"}, Repo: "acme/alpha"},
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/beta/issues/2", Number: 2, Title: "b"}, Repo: "acme/beta"},
	}
	if err := store.ImportTickets(ctx, "project:x", seed, testNow); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	server := openServer(track(t, store, alphaRepo, betaRepo), fixedClock(testNow), "")
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{
		"github.com/acme/alpha": fakeTrackerSource{features: []tracker.Feature{"project:x", "project:y"}},
		"github.com/acme/beta":  fakeTrackerSource{features: []tracker.Feature{"project:z"}},
	}))

	body := renderPath(t, server, "/repos/acme/alpha")
	for _, want := range []string{
		"<h1>acme/alpha</h1>", "git@github.com:acme/alpha.git",
		"project:x", "project:y", "also in acme/beta",
		`<a href="/repos/acme/alpha">acme/alpha</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scoped page missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `<table id="board"`) || !strings.Contains(body, `hx-get="/board?repo=acme%2Falpha"`) {
		t.Errorf("scoped page missing its repo's board:\n%s", body)
	}
	if strings.Contains(body, "issues/2") {
		t.Errorf("scoped page's board listed a ticket from beta:\n%s", body)
	}
	if strings.Contains(body, "project:z") {
		t.Errorf("scoped page listed a feature from beta's tracker:\n%s", body)
	}
	if strings.Contains(body, `action="/features/project:y/import"`) {
		t.Errorf("unimported project:y offers reimport:\n%s", body)
	}
	if !strings.Contains(body, `action="/features/project:x/import"`) {
		t.Errorf("imported project:x is missing reimport:\n%s", body)
	}
}

func TestScopedRepoPageForAnUnknownRepoSaysNotTracked(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/acme/stranger")
	for _, want := range []string{"<h1>acme/stranger</h1>", `data-glyph="ready"`, "Not tracked."} {
		if !strings.Contains(body, want) {
			t.Errorf("unknown repo page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "board &rarr;") {
		t.Errorf("unknown repo page offers a board link:\n%s", body)
	}
}

func TestReposPagesMatchGoldens(t *testing.T) {
	var calls atomic.Int32
	server := reposServer(t, &calls)
	server.SetTrackerSource(resolveByRemote(map[string]tracker.Source{
		"github.com/acme/alpha": fakeTrackerSource{features: []tracker.Feature{"project:x"}},
	}))

	for name, path := range map[string]string{
		"repos":              "/repos",
		"repos_search_empty": "/repos/search",
		"repos_search_api":   "/repos/search?q=api",
		"repo_scoped":        "/repos/acme/alpha",
	} {
		rec := get(t, server, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d: %s", path, rec.Code, rec.Body)
		}
		assertGolden(t, "testdata/"+name+".golden.html", rec.Body.Bytes())
	}
}

func TestNoticeRaisesARefusedRepoOnEveryPage(t *testing.T) {
	t.Parallel()

	st := track(t, openStore(t), alphaRepo)
	refused := storepkg.Repo{
		Name: "acme/beta", Remote: "https://github.com/acme/beta.git", State: storepkg.RepoRefused,
		RefusalKind: "clone", Refusal: "default branch is master", TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), refused); err != nil {
		t.Fatal(err)
	}
	server := openServer(st, fixedClock(testNow), "")

	for name, path := range map[string]string{
		"notice_refused_tickets": "/tickets",
		"notice_refused_repo":    "/repos/acme/beta",
		"notice_refused_repos":   "/repos",
	} {
		rec := get(t, server, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<a href="/repos/acme/beta">acme/beta</a> is refused`) {
			t.Errorf("GET %s missing refused-repo link:\n%s", path, rec.Body)
		}
		assertGolden(t, "testdata/"+name+".golden.html", rec.Body.Bytes())
	}
}

func TestFeaturesPathsRedirectToTheReposPages(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	for path, want := range map[string]string{
		"/features":                "/repos",
		"/features?repo=acme/beta": "/repos/acme/beta",
		"/features?repo=../x":      "/repos",
	} {
		rec := get(t, server, path)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != want {
			t.Errorf("GET %s = %d to %q, want 301 to %q", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestRepoPageRejectsANameThatIsNotOwnerSlashName(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	path := "/repos/acme/a%20b"
	if rec := get(t, server, path); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s = %d, want 404", path, rec.Code)
	}
}
