package web_test

import (
	"net/http"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestFeatureGraphPageMarksTheGraphTabCurrentAndScopesItsData(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope/graph")

	main := htmltest.Parse(t, page).One(t, "main#main")
	for _, tt := range []struct{ href, current string }{
		{"/f/board-scope", ""}, {"/f/board-scope/graph", "page"},
	} {
		link := main.One(t, `nav[aria-label="feature views"] a[href="`+tt.href+`"]`)
		attrs := map[string]string{
			"hx-get": tt.href, "hx-select": "#main", "hx-target": "#main",
			"hx-swap": "outerHTML", "hx-push-url": "true",
		}
		for name, want := range attrs {
			if got, _ := link.Attr(name); got != want {
				t.Errorf("%s %s = %q, want %q: %s", tt.href, name, got, want, link.HTML())
			}
		}
		if got, _ := link.Attr("aria-current"); got != tt.current {
			t.Errorf("%s aria-current = %q: %s", tt.href, got, link.HTML())
		}
	}
	main.One(t, `cc-graph[data-src="/graph.json?feature=board-scope"]`)
	main.One(t, `script[src="/assets/dist/graph.js"]`)
}

func TestFeatureGraphPageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus/graph", req{}); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus/graph: status = %d, want 404", rec.Code)
	}
}

func TestFeatureGraphPageEncodesTheFeatureInItsDataURL(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	storetest.Tickets(t, store, storepkg.Ticket{URL: "sandbox://CC-1", Feature: "project:two-tasks", Branch: "cc-1"})
	page := renderPath(t, newServer(store, testNow), "/f/project:two-tasks/graph")
	htmltest.Parse(t, page).One(t, `cc-graph[data-src="/graph.json?feature=project%3atwo-tasks"]`)
}
