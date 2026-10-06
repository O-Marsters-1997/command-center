package web_test

import (
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web"
)

func renderPage(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/")
}

func renderBoard(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/board")
}

func renderPath(t *testing.T, server *web.Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func ticketRef(ticketURL string) string { return "#" + path.Base(ticketURL) }

// rowHTML finds the whole <tr>...</tr> whose ticket-link button names ticketURL. Go's RE2 engine
// has no lookahead to keep a lazy ".*?" from crossing a row boundary, so this splits on literal
// "<tr" instead of matching in one regexp.
func rowHTML(t *testing.T, page, ticketURL string) string {
	t.Helper()
	ref := ticketRef(ticketURL)
	for _, block := range strings.Split(page, "<tr") {
		if !strings.Contains(block, ref) {
			continue
		}
		end := strings.Index(block, "</tr>")
		if end < 0 {
			continue
		}
		return "<tr" + block[:end+len("</tr>")]
	}
	t.Fatalf("no row found for %s in page:\n%s", ticketURL, page)
	return ""
}

var (
	pillTextRE   = regexp.MustCompile(`<span class="pill[^"]*">([^<]*)</span>`)
	queuedVerbRE = regexp.MustCompile(`·\s*([\w-]+)\s*queued`)
)

func rowState(t *testing.T, page, ticketURL string) string {
	t.Helper()
	cell := rowCellAt(t, page, ticketURL, 1)
	pill := pillTextRE.FindStringSubmatch(cell)
	if pill == nil {
		t.Fatalf("no state pill found for %s in cell:\n%s", ticketURL, cell)
	}
	state := pill[1]
	for _, m := range queuedVerbRE.FindAllStringSubmatch(cell, -1) {
		state += " · " + m[1] + " queued"
	}
	return state
}

func rowCellAt(t *testing.T, page, ticketURL string, column int) string {
	t.Helper()
	row := rowHTML(t, page, ticketURL)
	cells := regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`).FindAllStringSubmatch(row, -1)
	if column >= len(cells) {
		t.Fatalf("no column %d found for %s in page:\n%s", column, ticketURL, page)
	}
	return strings.TrimSpace(cells[column][1])
}
