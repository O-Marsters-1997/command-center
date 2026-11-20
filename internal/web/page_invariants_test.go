package web_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

type renderedPage struct{ name, html string }

func everyPage(t *testing.T) []renderedPage {
	t.Helper()
	failed, _ := sessionStoreEnding(t, sessionRunOpts{
		Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed,
	})
	board := newServer(railStore(t, true), testNow)
	features := threeFeatureServer(t)
	repos := reposServer(t, new(atomic.Int32))
	render := func(server *web.Server, path string) string {
		t.Helper()
		return renderPathWith(t, server, path, http.Header{"Cookie": {sidebarCookies}})
	}
	return []renderedPage{
		{name: "home", html: render(board, "/")},
		{name: "tickets", html: render(board, "/tickets")},
		{name: "board", html: render(board, "/board")},
		{name: "rail", html: render(board, "/rail")},
		{name: "insights", html: render(board, "/insights")},
		{name: "feature", html: render(features, "/f/board-scope")},
		{name: "feature graph", html: render(features, "/f/board-scope/graph")},
		{name: "feature launch", html: render(features, "/f/board-scope/launch")},
		{name: "launch picker", html: render(features, "/launch")},
		{name: "session", html: render(newServer(failed, testNow), "/s/acme/web/1")},
		{name: "repos", html: render(repos, "/repos")},
		{name: "repo", html: render(repos, "/repos/acme/new")},
	}
}

const sidebarCookies = "rail-width=9000; rail-collapsed=1"

func isThirdParty(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "//")
}

func TestPageInvariants(t *testing.T) {
	t.Parallel()
	pages := everyPage(t)

	for _, page := range pages {
		t.Run(page.name+" carries no inline script", func(t *testing.T) {
			t.Parallel()
			for _, tag := range startTags(page.html) {
				attrs := map[string]string{}
				for _, attr := range tag.Attr {
					attrs[attr.Key] = attr.Val
					if strings.HasPrefix(attr.Key, "on") || strings.HasPrefix(attr.Key, "hx-on") {
						t.Errorf("%s has an inline handler: %s", page.name, tag.String())
					}
				}
				if _, external := attrs["src"]; tag.Data == "script" && !external {
					t.Errorf("%s has an inline script: %s", page.name, tag.String())
				}
			}
		})

		t.Run(page.name+" renders the sidebar from its cookies", func(t *testing.T) {
			t.Parallel()
			root := htmltest.Parse(t, page.html)
			apps := root.Find(".app")
			if len(apps) == 0 {
				return
			}
			if _, ok := apps[0].Attr("data-collapsed"); !ok {
				t.Errorf("%s .app is not collapsed under %q", page.name, sidebarCookies)
			}
			if got, _ := root.One(t, ".sidebar-resizer").Attr("aria-valuenow"); got != "440" {
				t.Errorf("%s resizer aria-valuenow = %q, want 440 (clamped)", page.name, got)
			}
			if got, _ := root.One(t, "button[data-sidebar-toggle]").Attr("aria-expanded"); got != "false" {
				t.Errorf("%s sidebar toggle aria-expanded = %q, want false", page.name, got)
			}
		})

		t.Run(page.name+" disables every hx-post form in flight", func(t *testing.T) {
			t.Parallel()
			for _, form := range htmltest.Parse(t, page.html).Find("form[hx-post]") {
				if _, ok := form.Attr("hx-disabled-elt"); !ok {
					t.Errorf("%s hx-post form lacks hx-disabled-elt: %s", page.name, form.HTML())
				}
			}
		})

		t.Run(page.name+" is light only and first party", func(t *testing.T) {
			t.Parallel()
			root := htmltest.Parse(t, page.html)
			if themed := root.Find("[data-theme]"); len(themed) != 0 {
				t.Errorf("%s carries a theme toggle: %s", page.name, themed[0].HTML())
			}
			for _, tag := range startTags(page.html) {
				for _, attr := range tag.Attr {
					switch attr.Key {
					case "src", "href", "poster", "data":
						if isThirdParty(attr.Val) && tag.Data != "a" {
							t.Errorf("%s %s %s = %q requests a third-party origin", page.name, tag.Data, attr.Key, attr.Val)
						}
					case "srcset":
						for _, candidate := range strings.Split(attr.Val, ",") {
							if fields := strings.Fields(candidate); len(fields) > 0 && isThirdParty(fields[0]) {
								t.Errorf("%s %s srcset requests a third-party origin %q", page.name, tag.Data, fields[0])
							}
						}
					}
				}
			}
		})
	}

	t.Run("at least one page renders an hx-post form", func(t *testing.T) {
		t.Parallel()
		forms := 0
		for _, page := range pages {
			forms += len(htmltest.Parse(t, page.html).Find("form[hx-post]"))
		}
		if forms == 0 {
			t.Error("no page renders an hx-post form, so the in-flight check proves nothing")
		}
	})

	t.Run("the htmx config forbids eval and injected styles", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, newServer(seededStore(t, testNow), testNow), "/tickets")
		meta := htmltest.Parse(t, page).One(t, `meta[name="htmx-config"]`)
		config, _ := meta.Attr("content")
		for _, want := range []string{`"allowEval":false`, `"includeIndicatorStyles":false`} {
			if !strings.Contains(config, want) {
				t.Errorf("GET /tickets htmx-config = %q, want %s", config, want)
			}
		}
	})
}
