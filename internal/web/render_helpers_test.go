package web_test

import (
	"net/http"
	"path"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web"
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
		t.Fatalf("GET %s: status = %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body
}

func ticketRef(ticketURL string) string { return "#" + path.Base(ticketURL) }
