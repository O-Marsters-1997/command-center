package web_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker/trackertest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
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
	return openServer(track(t, openStore(t), alphaRepo, betaRepo), fixedClock(testNow), "",
		web.WithPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
			calls.Add(1)
			return pushable, nil
		}))
}

func TestReposPage(t *testing.T) {
	t.Parallel()

	for name, run := range map[string]func(*testing.T){
		"lists exactly the known repos":                 testReposPageListsKnownRepos,
		"search merges pushable matches":                testRepoSearchMergesPushable,
		"search marks a non-main default branch":        testRepoSearchMarksDefaultBranch,
		"search makes one gh call within the window":    testRepoSearchOneGhCall,
		"board renders while the pushable fetch blocks": testBoardRendersWhilePushableBlocked,
		"search refetches once the cache expires":       testRepoSearchRefetches,
		"search falls back to known repos":              testRepoSearchFallsBack,
		"scoped page lists only this repo's features":   testScopedRepoPage,
		"scoped page for an unknown repo":               testScopedRepoPageUnknown,
		"notice raises a refused repo on every page":    testNoticeRaisesRefusedRepo,
		"rows show check glyph, status and open count":  testReposPageRowStates,
		"rejects a name that is not owner/name":         testRepoPageRejectsBadName,
	} {
		t.Run(name, run)
	}
}

func testReposPageListsKnownRepos(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)
	t.Cleanup(func() {
		if got := calls.Load(); got != 0 {
			t.Errorf("empty queries made %d gh calls, want 0", got)
		}
	})

	for _, tt := range []struct{ name, path string }{
		{name: "repos", path: "/repos"},
		{name: "empty search", path: "/repos/search"},
		{name: "empty query", path: "/repos/search?q="},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := renderPath(t, server, tt.path)
			root := htmltest.Parse(t, body)
			for _, repo := range []string{"acme/alpha", "acme/beta"} {
				root.One(t, `a[href="/repos/`+repo+`"]`)
			}
			if api := root.Find(`a[href="/repos/acme/api"]`); len(api) != 0 {
				t.Errorf("GET %s listed unknown repo: %s", tt.path, api[0].HTML())
			}
		})
	}
}

func testRepoSearchMergesPushable(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/search?q=API")
	root := htmltest.Parse(t, body)
	for _, repo := range []string{"acme/api", "acme/payments-api"} {
		row := searchRow(t, body, repo)
		if links := row.Find(`a[href="/repos/` + repo + `"]`); len(links) != 2 {
			t.Errorf("search result %s links = %d, want name and track: %s", repo, len(links), row.HTML())
		}
		if !strings.Contains(row.Text(), "not tracked") {
			t.Errorf("search result %s missing not-tracked label: %s", repo, row.HTML())
		}
	}
	if other := root.Find(`a[href="/repos/acme/ro"]`); len(other) != 0 {
		t.Errorf("search listed a non-pushable repo: %s", other[0].HTML())
	}

	tracked := renderPath(t, server, "/repos/search?q=alpha")
	trackedRoot := htmltest.Parse(t, tracked)
	if rows := trackedRoot.Find(`.track-hit`); len(rows) != 1 {
		t.Errorf("tracked search rows = %d, want one", len(rows))
	} else {
		link := rows[0].One(t, `a[href="/repos/acme/alpha"]`)
		if got := link.Text(); got != "acme/alpha" {
			t.Errorf("tracked repo link text = %q, want acme/alpha", got)
		}
		if got := rows[0].Text(); strings.Contains(got, "not tracked") {
			t.Errorf("tracked repo labelled untracked: %s", rows[0].HTML())
		}
	}
}

func testRepoSearchMarksDefaultBranch(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/search?q=api")
	paymentsRow, apiRow := searchRow(t, body, "acme/payments-api"), searchRow(t, body, "acme/api")
	if got := paymentsRow.Text(); !strings.Contains(got, "will be refused: default branch is master") {
		t.Errorf("master-default repo is not marked: %s", paymentsRow.HTML())
	}
	if links := paymentsRow.Find(`a[href="/repos/acme/payments-api"]`); len(links) != 2 || links[1].Text() != "track →" {
		t.Errorf("marked row lacks track link: %s", paymentsRow.HTML())
	}
	if strings.Contains(apiRow.Text(), "will be refused") {
		t.Errorf("main-default repo is marked: %s", apiRow.HTML())
	}
}

func searchRow(t *testing.T, body, fullName string) htmltest.Node {
	t.Helper()
	for _, row := range htmltest.Parse(t, body).Find(".track-hit") {
		if len(row.Find(`a[href="/repos/`+fullName+`"]`)) != 0 {
			return row
		}
	}
	t.Fatalf("no search row for %s", fullName)
	return htmltest.Node{}
}

func testRepoSearchOneGhCall(t *testing.T) {
	t.Helper()
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

func testBoardRendersWhilePushableBlocked(t *testing.T) {
	t.Helper()
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := openServer(openStore(t), fixedClock(testNow), "", web.WithPushableSource(
		func(context.Context) ([]gh.RepoSummary, error) {
			close(started)
			<-release
			return pushable, nil
		},
	))
	searchDone := make(chan int, 1)
	go func() {
		response := do(t, server, http.MethodGet, "/repos/search?q=api", req{Header: http.Header{"Hx-Request": {"true"}}})
		searchDone <- response.Code
	}()
	<-started
	boardDone := make(chan int, 1)
	go func() {
		response := do(t, server, http.MethodGet, "/board", req{Header: http.Header{"Hx-Request": {"true"}}})
		boardDone <- response.Code
	}()
	if status := <-boardDone; status != http.StatusOK {
		t.Errorf("GET /board during pushable fetch = %d, want 200", status)
	}
	close(release)
	if status := <-searchDone; status != http.StatusOK {
		t.Errorf("GET /repos/search = %d, want 200", status)
	}
}

func testRepoSearchRefetches(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	clock := &steppingClock{at: testNow}
	server := openServer(openStore(t), clock, "", web.WithPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
		calls.Add(1)
		return pushable, nil
	}))

	renderPath(t, server, "/repos/search?q=api")
	clock.at = clock.at.Add(61 * time.Second)
	renderPath(t, server, "/repos/search?q=api")
	if got := calls.Load(); got != 2 {
		t.Errorf("gh calls across the 60s window = %d, want 2", got)
	}
}

type steppingClock struct{ at time.Time }

func (c *steppingClock) Now() time.Time { return c.at }

func testRepoSearchFallsBack(t *testing.T) {
	t.Helper()
	t.Parallel()

	server := openServer(track(t, openStore(t), alphaRepo), fixedClock(testNow), "",
		web.WithPushableSource(func(context.Context) ([]gh.RepoSummary, error) {
			return nil, errors.New("gh: not logged in")
		}))

	body := renderPath(t, server, "/repos/search?q=alpha")
	root := htmltest.Parse(t, body)
	root.One(t, `a[href="/repos/acme/alpha"]`)
	if got := root.Text(); !strings.Contains(got, "gh search failed") || !strings.Contains(got, "not logged in") {
		t.Errorf("failed search message = %q", got)
	}
}

func testScopedRepoPage(t *testing.T) {
	t.Helper()
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
	betaY := []storepkg.ImportedTicket{
		{Ticket: tracker.Ticket{URL: "https://github.com/acme/beta/issues/3", Number: 3, Title: "c"}, Repo: "acme/beta"},
	}
	if err := store.ImportTickets(ctx, "project:y", betaY, testNow); err != nil {
		t.Fatalf("seed ImportTickets: %v", err)
	}

	server := openServer(track(t, store, alphaRepo, betaRepo), fixedClock(testNow), "",
		web.WithTrackerSource(trackertest.ByRemote(map[string]tracker.Source{
			"github.com/acme/alpha": &trackertest.Source{
				ByFeature: map[string][]tracker.Ticket{"project:x": nil, "project:y": nil},
			},
			"github.com/acme/beta": &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:z": nil}},
		})))

	body := renderPath(t, server, "/repos/acme/alpha")
	main := htmltest.Parse(t, body).One(t, "main#main")
	if got := main.One(t, "h1").Text(); got != "acme/alpha" {
		t.Errorf("repo heading = %q", got)
	}
	crumbs := main.One(t, "ol.crumbs")
	crumbs.One(t, `a[href="/repos"][hx-select="#main"]`)
	if got := crumbs.Text(); !strings.Contains(got, "acme/alpha") {
		t.Errorf("repo breadcrumb = %q, want it to name acme/alpha", got)
	}
	wants := []string{
		"git@github.com:acme/alpha.git", "1 open ticket", "base origin/main", "project:y", "also in acme/beta",
	}
	for _, want := range wants {
		if got := main.Text(); !strings.Contains(got, want) {
			t.Errorf("repo content lacks %q: %q", want, got)
		}
	}
	main.One(t, `#checks-h`)
	board := main.One(t, "div#board")
	if got, _ := board.Attr("hx-get"); got != "/board?repo=acme%2Falpha" {
		t.Errorf("repo board hx-get = %q, want /board?repo=acme%%2Falpha", got)
	}
	if beta := board.Find(`a[href="/s/acme/beta/2"]`); len(beta) != 0 {
		t.Errorf("repo board includes beta ticket: %s", beta[0].HTML())
	}
	if strings.Contains(main.Text(), "project:z") {
		t.Error("repo page contains beta-only feature")
	}
	if imported := main.Find(`form[action="/features/project:x/import"]`); len(imported) != 0 {
		t.Errorf("imported feature offers import: %s", imported[0].HTML())
	}
	main.One(t, `form[action="/features/project:y/import"]`)
}

func testScopedRepoPageUnknown(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	body := renderPath(t, server, "/repos/acme/stranger")
	main := htmltest.Parse(t, body).One(t, "main#main")
	if got := main.One(t, "h1").Text(); got != "acme/stranger" {
		t.Errorf("unknown repo heading = %q", got)
	}
	main.One(t, `#repo-banner[data-glyph="ready"]`)
	if got := main.Text(); !strings.Contains(got, "Not tracked.") {
		t.Errorf("unknown repo status = %q", got)
	}
	if board := main.Find("div#board"); len(board) != 0 {
		t.Errorf("unknown repo offers board: %s", board[0].HTML())
	}
}

func testNoticeRaisesRefusedRepo(t *testing.T) {
	t.Helper()
	t.Parallel()

	st := track(t, openStore(t), alphaRepo)
	refused := storepkg.Repo{
		Name: "acme/beta", Remote: "https://github.com/acme/beta.git", Phase: plan.RepoRefused,
		RefusalKind: "clone", Refusal: "default branch is master", TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), refused); err != nil {
		t.Fatalf("UpsertRepo(%q): %v", refused.Name, err)
	}
	server := openServer(st, fixedClock(testNow), "")

	for _, tt := range []struct{ name, path string }{
		{name: "tickets", path: "/tickets"},
		{name: "refused repo", path: "/repos/acme/beta"},
		{name: "repos", path: "/repos"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, server, http.MethodGet, tt.path, req{})
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d", tt.path, rec.Code)
			}
			notices := htmltest.Parse(t, rec.Body).One(t, "#notices")
			link := notices.One(t, `a[href="/repos/acme/beta"]`)
			if got := link.Text(); got != "acme/beta" {
				t.Errorf("GET %s refusal link = %q: %s", tt.path, got, link.HTML())
			}
		})
	}
}

func testReposPageRowStates(t *testing.T) {
	t.Helper()
	t.Parallel()

	st := track(t, openStore(t), alphaRepo)
	refused := storepkg.Repo{
		Name: "acme/beta", Remote: "https://github.com/acme/beta.git", Phase: plan.RepoRefused,
		RefusalKind: plan.RefusalMergeSettings, Refusal: "merge commits allowed", TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), refused); err != nil {
		t.Fatalf("UpsertRepo(%q): %v", refused.Name, err)
	}
	server := openServer(st, fixedClock(testNow), "")

	root := htmltest.Parse(t, renderPath(t, server, "/repos")).One(t, `[data-scope="repo"]`)
	alpha := root.One(t, `a[href="/repos/acme/alpha"]`)
	alpha.One(t, ".glyph-done")
	for _, want := range []string{"Checks passed", "No open tickets"} {
		if got := alpha.Text(); !strings.Contains(got, want) {
			t.Errorf("ready row = %q, want %q", got, want)
		}
	}
	beta := root.One(t, `a[href="/repos/acme/beta"]`)
	beta.One(t, ".glyph-failed")
	if got := beta.Text(); !strings.Contains(got, "Failed: squash-only merges") {
		t.Errorf("refused row = %q, want Failed: squash-only merges", got)
	}
}

func testRepoPageRejectsBadName(t *testing.T) {
	t.Helper()
	t.Parallel()

	var calls atomic.Int32
	server := reposServer(t, &calls)

	path := "/repos/acme/a%20b"
	if rec := do(t, server, http.MethodGet, path, req{}); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s = %d, want 404", path, rec.Code)
	}
}
