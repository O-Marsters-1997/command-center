package web_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func threeRepoStore(t *testing.T) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Repo: "other", Branch: "lone"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	return store
}

func threeRepoServer(t *testing.T) *web.Server {
	t.Helper()
	at := testNow
	repos := []config.Repo{{Name: "repo"}, {Name: "services"}, {Name: "other"}}
	return web.NewServer(threeRepoStore(t), fixedClock(at), repos, "")
}

func TestRepoScopeAdmitsAGroupWholeAndDropsAnUnrelatedOne(t *testing.T) {
	t.Parallel()

	server := threeRepoServer(t)

	underRepo := renderPath(t, server, "/?repo=repo")
	for _, want := range []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")} {
		if !strings.Contains(underRepo, want) {
			t.Errorf("?repo=repo dropped %s from its own group:\n%s", want, underRepo)
		}
	}
	if strings.Contains(underRepo, ticketRef("sandbox://LONE")) {
		t.Errorf("?repo=repo rendered the unrelated LONE ticket:\n%s", underRepo)
	}

	underOther := renderPath(t, server, "/?repo=other")
	if !strings.Contains(underOther, ticketRef("sandbox://LONE")) {
		t.Errorf("?repo=other dropped its own LONE ticket:\n%s", underOther)
	}
	for _, want := range []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")} {
		if strings.Contains(underOther, want) {
			t.Errorf("?repo=other rendered %s, from a group with no member in other:\n%s", want, underOther)
		}
	}
}

func TestRepoScopeUnknownFallsBackToUnscoped(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/?repo=bogus")
	for _, want := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !strings.Contains(page, ticketRef(want)) {
			t.Errorf("?repo=bogus dropped %s, want the unscoped board:\n%s", want, page)
		}
	}
}

func TestRepoScopeNarrowsTheBandButNotLiveAgents(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/?repo=other")
	if !strings.Contains(page, "1/1 yours") {
		t.Errorf("fleet headline under ?repo=other = want 1/1 yours (LONE only):\n%s", page)
	}
	if !strings.Contains(page, "0 live") {
		t.Errorf("masthead live-agent count changed under scope, want it to stay the process-wide 0 live:\n%s", page)
	}
}

func TestMastheadRepoLinksNameEveryConfiguredRepoAndTheCurrentScope(t *testing.T) {
	t.Parallel()

	server := threeRepoServer(t)

	unscoped := renderPath(t, server, "/")
	for _, want := range []string{`href="/"`, `href="/?repo=repo"`, `href="/?repo=services"`, `href="/?repo=other"`} {
		if !strings.Contains(unscoped, want) {
			t.Errorf("masthead missing repo link %s:\n%s", want, unscoped)
		}
	}
	if !strings.Contains(unscoped, `href="/" aria-current="page"`) {
		t.Errorf("unscoped masthead should mark \"all\" current:\n%s", unscoped)
	}

	scoped := renderPath(t, server, "/?repo=services")
	if !strings.Contains(scoped, `href="/?repo=services" aria-current="page"`) {
		t.Errorf("?repo=services should mark its own pill current:\n%s", scoped)
	}
	if strings.Contains(scoped, `href="/" aria-current="page"`) {
		t.Errorf("?repo=services should not also mark \"all\" current:\n%s", scoped)
	}
}

func TestMastheadOmitsRepoLinksWithNoConfiguredRepos(t *testing.T) {
	t.Parallel()

	page := renderPage(t, seededServer(t))
	if strings.Contains(page, "repo=") {
		t.Errorf("masthead rendered a repo link though no repo is configured:\n%s", page)
	}
}

func threeFeatureStore(t *testing.T) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://ROOT", Feature: "board-scope", Branch: "root"},
		{URL: "sandbox://CHILD", Feature: "board-scope", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Feature: "sqlc-migration", Branch: "lone"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	return store
}

func threeFeatureServer(t *testing.T) *web.Server {
	t.Helper()
	at := testNow
	return newServer(threeFeatureStore(t), at)
}

func TestFeatureScopeAdmitsAGroupWholeAndDropsAnUnrelatedOne(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)

	underFeature := renderPath(t, server, "/?feature=board-scope")
	for _, want := range []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")} {
		if !strings.Contains(underFeature, want) {
			t.Errorf("?feature=board-scope dropped %s from its own group:\n%s", want, underFeature)
		}
	}
	if strings.Contains(underFeature, ticketRef("sandbox://LONE")) {
		t.Errorf("?feature=board-scope rendered the unrelated LONE ticket:\n%s", underFeature)
	}

	underOther := renderPath(t, server, "/?feature=sqlc-migration")
	if !strings.Contains(underOther, ticketRef("sandbox://LONE")) {
		t.Errorf("?feature=sqlc-migration dropped its own LONE ticket:\n%s", underOther)
	}
	for _, want := range []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")} {
		if strings.Contains(underOther, want) {
			t.Errorf("?feature=sqlc-migration rendered %s, from a group with no member in it:\n%s", want, underOther)
		}
	}
}

func TestFeatureScopeUnknownFallsBackToUnscoped(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/?feature=bogus")
	for _, want := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !strings.Contains(page, ticketRef(want)) {
			t.Errorf("?feature=bogus dropped %s, want the unscoped board:\n%s", want, page)
		}
	}
}

func TestFeatureAndRepoScopeComposeNeitherOverridingTheOther(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	ctx := t.Context()
	tickets := []storepkg.Ticket{
		{URL: "sandbox://MATCH", Repo: "repo", Feature: "board-scope", Branch: "match"},
		{URL: "sandbox://WRONG-REPO", Repo: "other", Feature: "board-scope", Branch: "wrong-repo"},
		{URL: "sandbox://WRONG-FEATURE", Repo: "repo", Feature: "sqlc-migration", Branch: "wrong-feature"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	repos := []config.Repo{{Name: "repo"}, {Name: "other"}}
	server := web.NewServer(store, fixedClock(at), repos, "")

	page := renderPath(t, server, "/?feature=board-scope&repo=repo")
	if !strings.Contains(page, ticketRef("sandbox://MATCH")) {
		t.Errorf("?feature=board-scope&repo=repo dropped the ticket matching both:\n%s", page)
	}
	for _, want := range []string{ticketRef("sandbox://WRONG-REPO"), ticketRef("sandbox://WRONG-FEATURE")} {
		if strings.Contains(page, want) {
			t.Errorf("?feature=board-scope&repo=repo rendered %s, matching only one axis:\n%s", want, page)
		}
	}
}

func TestClearingOneScopeAxisLeavesTheOtherApplied(t *testing.T) {
	t.Parallel()

	server := threeRepoServer(t)

	page := renderPath(t, server, "/?repo=repo&feature=")
	for _, want := range []string{ticketRef("sandbox://ROOT"), ticketRef("sandbox://CHILD")} {
		if !strings.Contains(page, want) {
			t.Errorf("?repo=repo&feature= dropped %s though the repo scope should still apply:\n%s", want, page)
		}
	}
	if strings.Contains(page, ticketRef("sandbox://LONE")) {
		t.Errorf("?repo=repo&feature= rendered the out-of-repo LONE ticket:\n%s", page)
	}
}

func TestMastheadOmitsFeatureLinksWithNoTicketCarryingAFeature(t *testing.T) {
	t.Parallel()

	page := renderPage(t, seededServer(t))
	if strings.Contains(page, "feature=") {
		t.Errorf("masthead rendered a feature link though no ticket carries one:\n%s", page)
	}
}

func TestMastheadShowsReimportOnlyWhenFeatureScoped(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)

	unscoped := renderPath(t, server, "/")
	if strings.Contains(unscoped, "/import") {
		t.Errorf("unscoped board offers a reimport action with no feature to name:\n%s", unscoped)
	}

	scoped := renderPath(t, server, "/?feature=board-scope")
	if !strings.Contains(scoped, `hx-post="/features/board-scope/import?feature=board-scope"`) {
		t.Errorf("?feature=board-scope masthead missing the reimport action:\n%s", scoped)
	}
	if !strings.Contains(scoped, `<input type="hidden" name="feature" value="board-scope">`) {
		t.Errorf("?feature=board-scope masthead missing the launch action:\n%s", scoped)
	}
	if !strings.Contains(scoped, `href="/?view=board"`) || !strings.Contains(scoped, ">all features</a>") {
		t.Errorf("?feature=board-scope masthead missing the all-features link:\n%s", scoped)
	}
	if !strings.Contains(scoped, `href="/?view=board&feature=board-scope"`) {
		t.Errorf("?feature=board-scope sidebar board link should carry the feature scope:\n%s", scoped)
	}
	if !strings.Contains(scoped, `href="/?view=graph&feature=board-scope"`) {
		t.Errorf("?feature=board-scope sidebar graph link should carry the feature scope:\n%s", scoped)
	}
}

type scopeJSONGroup struct {
	Root *struct {
		URL string `json:"url"`
	} `json:"root"`
	Children []struct {
		URL string `json:"url"`
	} `json:"children"`
}

func scopeJSONURLs(t *testing.T, groups []scopeJSONGroup) []string {
	t.Helper()
	var urls []string
	for _, g := range groups {
		if g.Root != nil {
			urls = append(urls, g.Root.URL)
		}
		for _, c := range g.Children {
			urls = append(urls, c.URL)
		}
	}
	return urls
}

func TestGraphJSONRespectsTheRepoScope(t *testing.T) {
	t.Parallel()

	server := threeRepoServer(t)
	rec := fetchGraphPath(t, server, "/graph.json?repo=repo")
	var groups []scopeJSONGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode /graph.json?repo=repo: %v\n%s", err, rec.Body)
	}
	urls := scopeJSONURLs(t, groups)
	if len(urls) != 2 {
		t.Fatalf("/graph.json?repo=repo groups = %v, want ROOT and CHILD only", urls)
	}
	for _, want := range []string{"sandbox://ROOT", "sandbox://CHILD"} {
		found := false
		for _, u := range urls {
			found = found || u == want
		}
		if !found {
			t.Errorf("/graph.json?repo=repo = %v, want it to include %s", urls, want)
		}
	}
}

func fetchGraphPath(t *testing.T, server *web.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := get(t, server, path)
	return rec
}

func TestRepoScopeSurvivesTheBoardsOwnPollAndRowPaths(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/?repo=repo")
	if !strings.Contains(page, `hx-get="/board?repo=repo"`) {
		t.Errorf("the board's own poll dropped the repo scope:\n%s", page)
	}
	row := rowHTML(t, page, "sandbox://ROOT")
	if !strings.Contains(row, "repo=repo") {
		t.Errorf("ROOT's row paths dropped the repo scope:\n%s", row)
	}
}
