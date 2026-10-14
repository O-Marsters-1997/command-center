package web_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

type jsonRow struct {
	URL      string   `json:"url"`
	State    string   `json:"state"`
	Glyph    string   `json:"glyph"`
	Blocking []string `json:"blocking"`
}

type jsonGroup struct {
	Root     *jsonRow  `json:"root"`
	Children []jsonRow `json:"children"`
}

func TestGraphJSONMarshalsTheSameGroupsTheBoardRangesOver(t *testing.T) {
	t.Parallel()

	server := seededServer(t)
	board := renderBoard(t, server)
	rec := do(t, server, http.MethodGet, "/graph.json", req{})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /graph.json: status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var groups []jsonGroup
	if err := json.Unmarshal([]byte(rec.Body), &groups); err != nil {
		t.Fatalf("decode /graph.json: %v\n%s", err, rec.Body)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (CC-2's only blocker is CC-1)", len(groups))
	}
	group := groups[0]
	if group.Root == nil || group.Root.URL != "sandbox://CC-1" {
		t.Fatalf("group.Root = %+v, want sandbox://CC-1", group.Root)
	}
	rootRow := webtest.RowFor(t, board, "sandbox://CC-1")
	if got := rootRow.Row.One(t, ".glyph").Text(); got != group.Root.State {
		t.Errorf("graph root state = %q, want board %q:\n%s", group.Root.State, got, rootRow.Row.HTML())
	}
	if len(group.Children) != 1 || group.Children[0].URL != "sandbox://CC-2" {
		t.Fatalf("group.Children = %+v, want one row for sandbox://CC-2", group.Children)
	}
	child := group.Children[0]
	childRow := webtest.RowFor(t, board, "sandbox://CC-2")
	if got := childRow.Row.One(t, ".glyph").Text(); got != child.State {
		t.Errorf("graph child state = %q, want board %q:\n%s", child.State, got, childRow.Row.HTML())
	}
	if len(child.Blocking) != 1 || child.Blocking[0] != "sandbox://CC-1" {
		t.Errorf("child.Blocking = %v, want [sandbox://CC-1]", child.Blocking)
	}
}

var hyphenatedTagRE = regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9]*-[a-zA-Z0-9-]*[\s>/]`)

func TestBoardFragmentNeverContainsACustomElementTag(t *testing.T) {
	t.Parallel()

	board := renderBoard(t, seededServer(t))
	if hyphenatedTagRE.MatchString(board) {
		t.Errorf("GET /board contains a hyphenated tag name:\n%s", board)
	}
}
