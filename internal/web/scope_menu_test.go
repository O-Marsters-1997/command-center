package web_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestScopeMenuListsProjectsThenLooseReposWithKeys(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, betaRepo, soloRepo)
	if err := st.CreateProject(t.Context(), storepkg.NewProject{
		Name: "acme", Hue: 1, Repos: []string{"acme/alpha", "acme/beta"}, At: testNow,
	}); err != nil {
		t.Fatalf("CreateProject(acme): %v", err)
	}
	server := openServer(st, fixedClock(testNow), "")

	body := renderPath(t, server, "/tickets")
	page := htmltest.Parse(t, body)
	menu := page.One(t, "[data-scope-menu]")
	if got := menu.One(t, "summary").Text(); got != "All projects · 0 running" {
		t.Errorf("scope summary = %q: %s", got, menu.HTML())
	}
	links := menu.Find("nav a")
	var got []string
	for _, link := range links {
		href, _ := link.Attr("href")
		key, _ := link.Attr("data-key")
		got = append(got, href+" "+key)
	}
	want := []string{"/tickets g 0", "/tickets?project=acme g 1", "/tickets?repo=acme%2Fsolo g 2"}
	if !slices.Equal(got, want) {
		t.Errorf("scope links = %q, want %q: %s", got, want, menu.HTML())
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse scope menu page: %v", err)
	}
	var order []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "data-scope-menu" || attr.Key == "id" && attr.Val == "rail-search" {
					order = append(order, attr.Key)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if !slices.Equal(order, []string{"data-scope-menu", "id"}) {
		t.Errorf("scope menu and rail search DOM order = %q, want menu before search: %s", order, menu.HTML())
	}
}

func TestScopeMenuSummaryNamesTheScope(t *testing.T) {
	t.Parallel()
	st := track(t, openStore(t), alphaRepo, soloRepo)
	if err := st.CreateProject(t.Context(), storepkg.NewProject{
		Name: "acme", Hue: 1, Repos: []string{"acme/alpha"}, At: testNow,
	}); err != nil {
		t.Fatalf("CreateProject(acme): %v", err)
	}
	server := openServer(st, fixedClock(testNow), "")

	project := htmltest.Parse(t, renderPath(t, server, "/tickets?project=acme")).One(t, "[data-scope-menu] summary")
	project.One(t, `[data-hue][aria-hidden="true"]`)
	if got := project.Text(); got != "A acme" {
		t.Errorf("project scope summary = %q: %s", got, project.HTML())
	}
	repo := htmltest.Parse(t, renderPath(t, server, "/tickets?repo=acme%2Fsolo")).One(t, "[data-scope-menu] summary")
	if got := repo.Text(); got != "acme/solo" {
		t.Errorf("repo scope summary = %q: %s", got, repo.HTML())
	}
}
