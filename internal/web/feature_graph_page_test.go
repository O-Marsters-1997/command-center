package web_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestFeatureGraphPageMarksTheGraphTabCurrentAndScopesItsData(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope/graph")

	for _, want := range []string{
		`<a class="pill-tab" href="/f/board-scope" data-key="v" hx-get="/f/board-scope"` +
			navBundleTail + `>Board</a>`,
		`<a class="pill-tab" href="/f/board-scope/graph" hx-get="/f/board-scope/graph"` +
			navBundleTail + ` aria-current="page">Graph</a>`,
		`<cc-graph data-src="/graph.json?feature=board-scope">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("graph page missing %s:\n%s", want, page)
		}
	}
}

func TestFeatureGraphPageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus/graph", req{}); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus/graph: status = %d, want 404", rec.Code)
	}
}
