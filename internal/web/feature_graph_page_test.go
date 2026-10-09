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
		`<a class="pill-tab" href="/f/board-scope">Board</a>`,
		`<a class="pill-tab" href="/f/board-scope/graph" aria-current="page">Graph</a>`,
		`<cc-graph data-src="/graph.json?feature=board-scope">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("graph page missing %s:\n%s", want, page)
		}
	}
}

func TestFeatureGraphPageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	if rec := get(t, threeFeatureServer(t), "/f/bogus/graph"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus/graph: status = %d, want 404", rec.Code)
	}
}
