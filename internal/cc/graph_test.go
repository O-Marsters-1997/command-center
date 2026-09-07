package cc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

const goldenGraph = "testdata/graph.golden.json"

// jsonRow is a loose decode of one row: the test reads only the fields it needs to cross-check
// against the board's own rendered HTML, not the whole shape.
type jsonRow struct {
	URL      string   `json:"url"`
	State    string   `json:"state"`
	Tone     string   `json:"tone"`
	Blocking []string `json:"blocking"`
}

type jsonGroup struct {
	Root     *jsonRow  `json:"root"`
	Children []jsonRow `json:"children"`
}

// boardPillText reads a row's bare state-pill text off the board, without rowState's own
// "· verb queued" suffix: row.State on the JSON side never carries that, it is PendingVerbs.
func boardPillText(t *testing.T, page, ticketURL string) string {
	t.Helper()
	cell := rowCellAt(t, page, ticketURL, 1)
	pill := pillTextRE.FindStringSubmatch(cell)
	if pill == nil {
		t.Fatalf("no state pill found for %s in cell:\n%s", ticketURL, cell)
	}
	return pill[1]
}

func fetchGraph(t *testing.T, server *cc.Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/graph.json", nil))
	return rec
}

// TestGraphJSONMarshalsTheSameGroupsTheBoardRangesOver covers the route's central acceptance
// criterion by cross-referencing the two renders of one fixture, rather than trusting either in
// isolation: CC-1 blocks CC-2, so /board nests CC-2 under CC-1 and /graph.json must carry the
// same blocker edge on the same two URLs.
func TestGraphJSONMarshalsTheSameGroupsTheBoardRangesOver(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	board := renderBoard(t, server)
	rec := fetchGraph(t, server)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /graph.json: status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var groups []jsonGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode /graph.json: %v\n%s", err, rec.Body)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (CC-2's only blocker is CC-1)", len(groups))
	}
	group := groups[0]
	if group.Root == nil || group.Root.URL != "sandbox://CC-1" {
		t.Fatalf("group.Root = %+v, want sandbox://CC-1", group.Root)
	}
	if got := boardPillText(t, board, "sandbox://CC-1"); got != group.Root.State {
		t.Errorf("graph root state = %q, want the board's own %q", group.Root.State, got)
	}
	if len(group.Children) != 1 || group.Children[0].URL != "sandbox://CC-2" {
		t.Fatalf("group.Children = %+v, want one row for sandbox://CC-2", group.Children)
	}
	child := group.Children[0]
	if got := boardPillText(t, board, "sandbox://CC-2"); got != child.State {
		t.Errorf("graph child state = %q, want the board's own %q", child.State, got)
	}
	if len(child.Blocking) != 1 || child.Blocking[0] != "sandbox://CC-1" {
		t.Errorf("child.Blocking = %v, want [sandbox://CC-1]", child.Blocking)
	}
}

// TestGraphJSONGoldensAFixedFixture pins the wire shape byte for byte: any change to row's own
// json tags, or to which fields it carries, is a diff here rather than a silent break in the
// island's fetch.
func TestGraphJSONGoldensAFixedFixture(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	rec := fetchGraph(t, server)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /graph.json: status = %d: %s", rec.Code, rec.Body)
	}
	assertGolden(t, goldenGraph, rec.Body.Bytes())
}

var hyphenatedTagRE = regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9]*-[a-zA-Z0-9-]*[\s>/]`)

// TestBoardFragmentNeverContainsACustomElementTag enforces the rule an island is never a
// descendant of a swap target: GET /board is the htmx poll's own swap target, so a custom
// element's tag name -- the only HTML tag shape that carries a hyphen -- must never appear in it.
func TestBoardFragmentNeverContainsACustomElementTag(t *testing.T) {
	t.Parallel()

	board := renderBoard(t, seededServer(t))
	if hyphenatedTagRE.MatchString(board) {
		t.Errorf("GET /board contains a hyphenated tag name:\n%s", board)
	}
}

// TestPageRendersCcGraphOutsideTheBoardOnlyForTheGraphView covers the other half of the same
// rule from the shell's side: <cc-graph> appears only under ?view=graph, and even there sits
// outside the #board table rather than inside it.
func TestPageRendersCcGraphOutsideTheBoardOnlyForTheGraphView(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	boardView := renderPath(t, server, "/")
	if strings.Contains(boardView, "<cc-graph") {
		t.Errorf("GET / with the default board view rendered <cc-graph>:\n%s", boardView)
	}

	graphView := renderPath(t, server, "/?view=graph")
	if !strings.Contains(graphView, "<cc-graph") {
		t.Fatalf("GET /?view=graph did not render <cc-graph>:\n%s", graphView)
	}
	if strings.Contains(graphView, `id="board"`) {
		t.Errorf("GET /?view=graph still rendered the board table:\n%s", graphView)
	}
}
