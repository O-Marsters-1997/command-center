package web_test

import (
	"net/http"
	"path"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func renderPage(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/tickets")
}

func renderBoard(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/board")
}

func renderPath(t *testing.T, server *web.Server, path string) string {
	t.Helper()
	r := req{}
	if isFragmentRoute(path) {
		r.Header = http.Header{"Hx-Request": {"true"}}
	}
	rec := do(t, server, http.MethodGet, path, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
	}
	return rec.Body
}

func ticketRef(ticketURL string) string { return "#" + path.Base(ticketURL) }

func attrs(n htmltest.Node, names ...string) map[string]string {
	got := make(map[string]string, len(names))
	for _, name := range names {
		got[name], _ = n.Attr(name)
	}
	return got
}

func navAttrs(href string) map[string]string {
	return map[string]string{
		"hx-get": href, "hx-select": "#main", "hx-target": "#main", "hx-swap": "outerHTML", "hx-push-url": "true",
	}
}

var navAttrNames = []string{"hx-get", "hx-select", "hx-target", "hx-swap", "hx-push-url"}
