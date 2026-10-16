package web_test

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestFeatureGraphPage(t *testing.T) {
	t.Parallel()

	t.Run("marks the graph tab current and scopes its data", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope/graph")).One(t, "main#main")

		for _, tt := range []struct{ href, current string }{
			{"/f/board-scope", ""}, {"/f/board-scope/graph", "page"},
		} {
			link := main.One(t, `nav[aria-label="feature views"] a[href="`+tt.href+`"]`)
			want := navAttrs(tt.href)
			want["aria-current"] = tt.current
			if diff := cmp.Diff(want, attrs(link, append(navAttrNames, "aria-current")...)); diff != "" {
				t.Errorf("GET /f/board-scope/graph tab %s mismatch (-want +got):\n%s", tt.href, diff)
			}
		}
		main.One(t, `cc-graph[data-src="/graph.json?feature=board-scope"]`)
		main.One(t, `script[type="module"][src="/assets/dist/graph.js"]`)
	})

	t.Run("an unknown feature is 404", func(t *testing.T) {
		t.Parallel()
		if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus/graph", req{}); rec.Code != http.StatusNotFound {
			t.Errorf("GET /f/bogus/graph status = %d, want 404", rec.Code)
		}
	})
}
