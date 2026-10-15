package web_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func threeRepoStore(t *testing.T) *storepkg.Store {
	t.Helper()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://ROOT", Repo: "repo", Branch: "root"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Repo: "other", Branch: "lone"},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(t.Context(), plan.Observation{ObservedAt: testNow}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func threeRepoServer(t *testing.T) *web.Server {
	t.Helper()
	at := testNow
	repos := named("repo", "services", "other")
	return openServer(track(t, threeRepoStore(t), repos...), fixedClock(at), "")
}

func TestBoardScopeAdmitsAGroupWholeAndDropsAnUnrelatedOne(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, groupPath, otherPath string
		server                     func(*testing.T) *web.Server
	}{
		{"repo", "/board?repo=repo", "/board?repo=other", threeRepoServer},
		{"feature", "/f/board-scope", "/f/sqlc-migration", threeFeatureServer},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := tt.server(t)
			refs := ticketRefs(webtest.BoardRows(t, renderPath(t, server, tt.groupPath)))
			for _, ticket := range []string{"sandbox://ROOT", "sandbox://CHILD"} {
				if !slices.Contains(refs, ticketRef(ticket)) {
					t.Errorf("%s lacks %s; refs = %q", tt.groupPath, ticket, refs)
				}
			}
			if slices.Contains(refs, ticketRef("sandbox://LONE")) {
				t.Errorf("%s includes unrelated LONE; refs = %q", tt.groupPath, refs)
			}

			refs = ticketRefs(webtest.BoardRows(t, renderPath(t, server, tt.otherPath)))
			if !slices.Equal(refs, []string{ticketRef("sandbox://LONE")}) {
				t.Errorf("%s refs = %q, want [#LONE]", tt.otherPath, refs)
			}
		})
	}
}

func TestRepoScopeAcceptsAnOwnerNameWithItsSlash(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://MINE", Repo: "O-Marsters-1997/command-center", Branch: "mine"},
		{URL: "sandbox://THEIRS", Repo: "acme/other", Branch: "theirs"},
	}
	storetest.Tickets(t, store, tickets...)
	tracked := track(t, store, named("O-Marsters-1997/command-center", "acme/other")...)
	server := openServer(tracked, fixedClock(testNow), "")

	page := renderPath(t, server, "/board?repo=O-Marsters-1997/command-center")
	if refs := ticketRefs(webtest.BoardRows(t, page)); !slices.Equal(refs, []string{ticketRef("sandbox://MINE")}) {
		t.Errorf("owner/repo scope refs = %q, want [#MINE]", refs)
	}
}

func TestRepoScopeUnknownFallsBackToUnscoped(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/board?repo=bogus")
	refs := ticketRefs(webtest.BoardRows(t, page))
	for _, ticket := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !slices.Contains(refs, ticketRef(ticket)) {
			t.Errorf("?repo=bogus lacks %s; refs = %q", ticket, refs)
		}
	}
}

func threeFeatureStore(t *testing.T) *storepkg.Store {
	t.Helper()

	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://ROOT", Feature: "board-scope", Branch: "root"},
		{URL: "sandbox://CHILD", Feature: "board-scope", Branch: "child", BlockedBy: []string{"sandbox://ROOT"}},
		{URL: "sandbox://LONE", Feature: "sqlc-migration", Branch: "lone"},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(t.Context(), plan.Observation{ObservedAt: testNow}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func threeFeatureServer(t *testing.T) *web.Server {
	t.Helper()
	at := testNow
	return newServer(threeFeatureStore(t), at)
}

func TestFeatureScopeUnknownIs404(t *testing.T) {
	t.Parallel()

	if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus", req{}); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus = %d, want 404", rec.Code)
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
	storetest.Tickets(t, store, tickets...)
	at := testNow
	if err := store.SaveObservation(ctx, plan.Observation{ObservedAt: at}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	repos := named("repo", "other")
	server := openServer(track(t, store, repos...), fixedClock(at), "")

	page := renderPath(t, server, "/board?feature=board-scope&repo=repo")
	if refs := ticketRefs(webtest.BoardRows(t, page)); !slices.Equal(refs, []string{ticketRef("sandbox://MATCH")}) {
		t.Errorf("composed scope refs = %q, want [#MATCH]", refs)
	}
}

func TestClearingOneScopeAxisLeavesTheOtherApplied(t *testing.T) {
	t.Parallel()

	server := threeRepoServer(t)

	page := renderPath(t, server, "/board?repo=repo&feature=")
	refs := ticketRefs(webtest.BoardRows(t, page))
	for _, ticket := range []string{"sandbox://ROOT", "sandbox://CHILD"} {
		if !slices.Contains(refs, ticketRef(ticket)) {
			t.Errorf("cleared feature scope lacks %s; refs = %q", ticket, refs)
		}
	}
	if slices.Contains(refs, ticketRef("sandbox://LONE")) {
		t.Errorf("cleared feature scope includes unrelated LONE; refs = %q", refs)
	}
}

func TestSidebarOmitsFeatureLinksWithNoTicketCarryingAFeature(t *testing.T) {
	t.Parallel()

	page := renderPath(t, seededServer(t), "/tickets")
	sidebar := htmltest.Parse(t, page).One(t, "aside#sidebar")
	for _, link := range sidebar.Find("a[href]") {
		if href, _ := link.Attr("href"); strings.HasPrefix(href, "/f/") {
			t.Errorf("sidebar rendered an unexpected feature link: %s", link.HTML())
		}
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
	rec := do(t, server, http.MethodGet, "/graph.json?repo=repo", req{})
	var groups []scopeJSONGroup
	if err := json.Unmarshal([]byte(rec.Body), &groups); err != nil {
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

func TestRepoScopeSurvivesTheBoardsOwnPollAndRowPaths(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeRepoServer(t), "/board?repo=repo")
	board := htmltest.Parse(t, page).One(t, "table#board")
	if got, _ := board.Attr("hx-get"); got != "/board?repo=repo" {
		t.Errorf("board poll hx-get = %q, want /board?repo=repo", got)
	}
	row := webtest.RowFor(t, page, "sandbox://ROOT").Row
	selection := row.One(t, `input[name="ticket"]`)
	if got, _ := selection.Attr("hx-get"); !strings.Contains(got, "repo=repo") {
		t.Errorf("ROOT row selection hx-get = %q: %s", got, selection.HTML())
	}
}
