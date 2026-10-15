package web_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type renderedPage struct{ name, html string }

func everyPage(t *testing.T) []renderedPage {
	t.Helper()
	failed, _ := sessionStoreEnding(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Outcome: plan.OutcomeFailed})
	board := newServer(railStore(t, true), testNow)
	features := threeFeatureServer(t)
	repos := reposServer(t, new(atomic.Int32))
	return []renderedPage{
		{name: "home", html: renderPath(t, board, "/")},
		{name: "tickets", html: renderPath(t, board, "/tickets")},
		{name: "board", html: renderPath(t, board, "/board")},
		{name: "rail", html: renderPath(t, board, "/rail")},
		{name: "insights", html: renderPath(t, board, "/insights")},
		{name: "feature", html: renderPath(t, features, "/f/board-scope")},
		{name: "feature graph", html: renderPath(t, features, "/f/board-scope/graph")},
		{name: "feature launch", html: renderPath(t, features, "/f/board-scope/launch")},
		{name: "launch picker", html: renderPath(t, features, "/launch")},
		{name: "session", html: renderPath(t, newServer(failed, testNow), "/s/acme/web/1")},
		{name: "repos", html: renderPath(t, repos, "/repos")},
		{name: "repo", html: renderPath(t, repos, "/repos/acme/new")},
	}
}

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
		meta := htmltest.Parse(t, renderPath(t, newServer(seededStore(t, testNow), testNow), "/tickets")).One(t, `meta[name="htmx-config"]`)
		config, _ := meta.Attr("content")
		for _, want := range []string{`"allowEval":false`, `"includeIndicatorStyles":false`} {
			if !strings.Contains(config, want) {
				t.Errorf("GET /tickets htmx-config = %q, want %s", config, want)
			}
		}
	})
}
