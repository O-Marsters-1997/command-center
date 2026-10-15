package web_test

import (
	"net/url"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func TestProjectScopeKeepsAGroupWholeAndCarriesItInEveryLink(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
	tickets := []storepkg.Ticket{
		{URL: "https://github.com/acme/solo/issues/3", Repo: "acme/solo", Branch: "s-3"},
		{URL: "https://github.com/acme/alpha/issues/1", Repo: "acme/alpha", Branch: "a-1",
			BlockedBy: []string{"https://github.com/acme/solo/issues/3"}},
		{URL: "https://github.com/acme/solo/issues/4", Repo: "acme/solo", Branch: "s-4"},
	}
	storetest.Tickets(t, st, tickets...)
	if err := st.CreateProject(t.Context(), storepkg.NewProject{
		Name: "acme", Hue: 1, Repos: []string{"acme/alpha", "acme/beta"}, At: testNow,
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	server := openServer(st, fixedClock(testNow), "")

	body := renderPath(t, server, "/tickets?project=ACME")
	root := htmltest.Parse(t, body)
	board := root.One(t, "table#board")
	for _, href := range []string{
		"/s/acme/alpha/1?project=acme", "/tickets?project=acme", "/insights?project=acme",
		"/repos?project=acme", "/launch?project=acme",
	} {
		if links := root.Find(`a[href="` + href + `"]`); len(links) == 0 {
			t.Errorf("GET /tickets?project=ACME has no link to %s", href)
		}
	}
	if get, _ := board.Attr("hx-get"); get != "/board?all=1&project=acme" {
		t.Errorf("board hx-get = %q, want /board?all=1&project=acme", get)
	}
	avatar := board.One(t, `tr.group [data-hue="1"][aria-hidden="true"]`)
	if got := avatar.Text(); got != "A" {
		t.Errorf("project avatar = %q: %s", got, avatar.HTML())
	}
	if others := board.Find(`a[href="/s/acme/solo/4"]`); len(others) != 0 {
		t.Errorf("project board includes unrelated ticket: %s", others[0].HTML())
	}

	unknown := renderPath(t, server, "/tickets?project="+url.QueryEscape("nope"))
	webtest.RowFor(t, unknown, "https://github.com/acme/solo/issues/4")
	unknownBoard := htmltest.Parse(t, unknown).One(t, "table#board")
	if get, _ := unknownBoard.Attr("hx-get"); get != "/board?all=1" {
		t.Errorf("unknown scope board hx-get = %q, want /board?all=1", get)
	}
}
