package cc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// threeRepoStore seeds a same-repo fan-out -- ROOT and CHILD both in "repo", CHILD blocked by
// ROOT -- plus LONE, an unrelated ticket in "other" that no scope below shares a group with
// (issue #219). plan.Unlocked only ever counts a same-repo blocker (plan.go:60), so a group as
// groupRows renders it can never itself straddle two repos; filterGroupsByRepo's own
// admit-whole rule for a group that did is proven directly against []group in scope_test.go.
func threeRepoStore(t *testing.T) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Repo: "other", Branch: "lone"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	return store
}

func threeRepoServer(t *testing.T) *cc.Server {
	t.Helper()
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	repos := []cc.Repo{{Name: "repo"}, {Name: "services"}, {Name: "other"}}
	return cc.NewServer(threeRepoStore(t), fixedClock(at), repos, "")
}

// TestRepoScopeAdmitsAGroupWholeAndDropsAnUnrelatedOne covers issue #219 AC1: a scope keeps a
// group with a member in scope and drops a group with none.
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

// TestRepoScopeUnknownFallsBackToUnscoped covers issue #219 AC1's other half.
func TestRepoScopeUnknownFallsBackToUnscoped(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/?repo=bogus")
	for _, want := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !strings.Contains(page, ticketRef(want)) {
			t.Errorf("?repo=bogus dropped %s, want the unscoped board:\n%s", want, page)
		}
	}
}

// TestRepoScopeNarrowsTheBandButNotLiveAgents covers issue #219 AC3.
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

// TestMastheadRepoLinksNameEveryConfiguredRepoAndTheCurrentScope covers issue #219's masthead
// nav row: "all" plus one pill per configured repo, with aria-current tracking the query.
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

// TestMastheadOmitsRepoLinksWithNoConfiguredRepos protects the many single-repo fixtures across
// this package's other tests: a server built with no [[repo]] renders no repo nav row at all.
func TestMastheadOmitsRepoLinksWithNoConfiguredRepos(t *testing.T) {
	t.Parallel()

	page := renderPage(t, seededServer(t))
	if strings.Contains(page, "repo=") {
		t.Errorf("masthead rendered a repo link though no repo is configured:\n%s", page)
	}
}

// threeFeatureStore seeds a same-feature fan-out -- ROOT and CHILD both in "board-scope", CHILD
// blocked by ROOT -- plus LONE, an unrelated ticket in "sqlc-migration", following
// threeRepoStore (issue #220).
func threeFeatureStore(t *testing.T) *cc.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://ROOT", Feature: "board-scope", Branch: "root"},
		{URL: "sandbox://CHILD", Feature: "board-scope", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Feature: "sqlc-migration", Branch: "lone"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	return store
}

func threeFeatureServer(t *testing.T) *cc.Server {
	t.Helper()
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return cc.NewServer(threeFeatureStore(t), fixedClock(at), nil, "")
}

// TestFeatureScopeAdmitsAGroupWholeAndDropsAnUnrelatedOne covers issue #220 AC1: a scope keeps a
// group with a member in scope and drops a group with none.
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

// TestFeatureScopeUnknownFallsBackToUnscoped covers issue #220 AC1's other half.
func TestFeatureScopeUnknownFallsBackToUnscoped(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/?feature=bogus")
	for _, want := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !strings.Contains(page, ticketRef(want)) {
			t.Errorf("?feature=bogus dropped %s, want the unscoped board:\n%s", want, page)
		}
	}
}

// TestFeatureAndRepoScopeComposeNeitherOverridingTheOther covers issue #220 AC3.
func TestFeatureAndRepoScopeComposeNeitherOverridingTheOther(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	ctx := t.Context()
	tickets := []cc.Ticket{
		{URL: "sandbox://MATCH", Repo: "repo", Feature: "board-scope", Branch: "match"},
		{URL: "sandbox://WRONG-REPO", Repo: "other", Feature: "board-scope", Branch: "wrong-repo"},
		{URL: "sandbox://WRONG-FEATURE", Repo: "repo", Feature: "sqlc-migration", Branch: "wrong-feature"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SaveObservation(ctx, cc.Observation{ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	repos := []cc.Repo{{Name: "repo"}, {Name: "other"}}
	server := cc.NewServer(store, fixedClock(at), repos, "")

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

// TestClearingOneScopeAxisLeavesTheOtherApplied covers issue #220 AC4.
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

// TestMastheadFeatureLinksNameTheFleetsOwnFeaturesAndTheCurrentScope covers issue #220's masthead
// nav row: "all" plus one pill per feature in the fleet, not every feature the tracker knows.
func TestMastheadFeatureLinksNameTheFleetsOwnFeaturesAndTheCurrentScope(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)

	unscoped := renderPath(t, server, "/")
	for _, want := range []string{`href="/"`, `href="/?feature=board-scope"`, `href="/?feature=sqlc-migration"`} {
		if !strings.Contains(unscoped, want) {
			t.Errorf("masthead missing feature link %s:\n%s", want, unscoped)
		}
	}

	scoped := renderPath(t, server, "/?feature=sqlc-migration")
	if !strings.Contains(scoped, `href="/?feature=sqlc-migration" aria-current="page"`) {
		t.Errorf("?feature=sqlc-migration should mark its own pill current:\n%s", scoped)
	}
	if strings.Contains(scoped, `href="/" aria-current="page"`) {
		t.Errorf("?feature=sqlc-migration should not also mark \"all\" current:\n%s", scoped)
	}
}

// TestMastheadOmitsFeatureLinksWithNoTicketCarryingAFeature protects the many fixtures across this
// package's other tests that never set Feature on a ticket.
func TestMastheadOmitsFeatureLinksWithNoTicketCarryingAFeature(t *testing.T) {
	t.Parallel()

	page := renderPage(t, seededServer(t))
	if strings.Contains(page, "feature=") {
		t.Errorf("masthead rendered a feature link though no ticket carries one:\n%s", page)
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

// TestGraphJSONRespectsTheRepoScope covers issue #219 AC5: /graph.json carries the same scoped
// groups the board renders, through the same parseViewParams.
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

func fetchGraphPath(t *testing.T, server *cc.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestRepoScopeSurvivesTheBoardsOwnPollAndRowPaths covers issue #219 AC4: the poll's own hx-get,
// a row's select path and a row's toggle path all carry the scope forward, since applyViewState
// copies params the same way it already does for Log, Sel and Tickets.
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

// TestLoopReconcilesATicketTheRepoScopeHides covers issue #219 AC6: the loop never reads a view
// parameter, so a tick still authorises and spawns a ticket that a repo scope's own board would
// never show.
func TestLoopReconcilesATicketTheRepoScopeHides(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 2, []string{"true"})
	cfg.Repos = append(cfg.Repos, cc.Repo{Name: "other", Checkout: filepath.Join(root, "repo")})

	store := openStore(t)
	shown := cc.Ticket{URL: "sandbox://SHOWN", Repo: "repo", Branch: "shown"}
	hidden := cc.Ticket{URL: "sandbox://HIDDEN", Repo: "other", Branch: "hidden"}
	if err := store.UpsertTickets(t.Context(), []cc.Ticket{shown, hidden}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Ticket{URL: hidden.URL}))
	authoriseTicket(t, store, hidden.URL, hash, at)

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the loop must act on HIDDEN whether or not any view ever scopes it out",
			len(fake.spawns))
	}
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := latest[hidden.URL]; !ok {
		t.Fatal("no run recorded for HIDDEN: the loop should have acted on it regardless of scope")
	}

	server := cc.NewServer(store, fixedClock(at), cfg.Repos, "")
	scoped := renderPath(t, server, "/?repo=repo")
	if strings.Contains(scoped, ticketRef(hidden.URL)) {
		t.Errorf("?repo=repo still rendered the hidden ticket, so it proves nothing about the loop's own scope:\n%s",
			scoped)
	}
	if !strings.Contains(scoped, ticketRef(shown.URL)) {
		t.Errorf("?repo=repo dropped its own ticket:\n%s", scoped)
	}
}
