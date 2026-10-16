package web_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

type jsonRow struct {
	URL      string   `json:"url"`
	Label    string   `json:"label"`
	Glyph    string   `json:"glyph"`
	Blocking []string `json:"blocking"`
}

type jsonGroup struct {
	Root     *jsonRow  `json:"root"`
	Children []jsonRow `json:"children"`
}

func TestBoardFragment(t *testing.T) {
	t.Parallel()

	board := htmltest.Parse(t, renderBoard(t, seededServer(t))).One(t, "table#board")
	root, err := html.Parse(strings.NewReader(board.HTML()))
	if err != nil {
		t.Fatalf("parse board elements: %v", err)
	}
	var check func(*html.Node)
	check = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.Contains(node.Data, "-") {
			t.Errorf("GET /board contains a hyphenated element <%s>", node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			check(child)
		}
	}
	check(root)
}

func TestGraphJSON(t *testing.T) {
	t.Parallel()

	t.Run("graph JSON marshals the same groups the board ranges over", func(t *testing.T) {
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
		if got := rootRow.Row.One(t, ".glyph").Text(); got != group.Root.Label {
			t.Errorf("graph root label = %q, want board %q:\n%s", group.Root.Label, got, rootRow.Row.HTML())
		}
		if len(group.Children) != 1 || group.Children[0].URL != "sandbox://CC-2" {
			t.Fatalf("group.Children = %+v, want one row for sandbox://CC-2", group.Children)
		}
		child := group.Children[0]
		childRow := webtest.RowFor(t, board, "sandbox://CC-2")
		if got := childRow.Row.One(t, ".glyph").Text(); got != child.Label {
			t.Errorf("graph child label = %q, want board %q:\n%s", child.Label, got, childRow.Row.HTML())
		}
		if len(child.Blocking) != 1 || child.Blocking[0] != "sandbox://CC-1" {
			t.Errorf("child.Blocking = %v, want [sandbox://CC-1]", child.Blocking)
		}
	})

	t.Run("graph JSON sends only island fields and canonical session path", func(t *testing.T) {
		t.Parallel()

		server := newServer(stackStore(t), testNow)
		rec := do(t, server, http.MethodGet, "/graph.json?feature=checkout", req{})
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /graph.json?feature=checkout: status = %d: %s", rec.Code, rec.Body)
		}
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rec.Body), &groups); err != nil {
			t.Fatalf("decode /graph.json?feature=checkout: %v", err)
		}
		if len(groups) != 1 {
			t.Fatalf("groups = %d, want 1", len(groups))
		}
		var rows []map[string]json.RawMessage
		var root map[string]json.RawMessage
		if err := json.Unmarshal(groups[0]["root"], &root); err != nil {
			t.Fatalf("decode graph root: %v", err)
		}
		rows = append(rows, root)
		var children []map[string]json.RawMessage
		if err := json.Unmarshal(groups[0]["children"], &children); err != nil {
			t.Fatalf("decode graph children: %v", err)
		}
		rows = append(rows, children...)
		if len(rows) != 4 {
			t.Fatalf("graph rows = %d, want 4", len(rows))
		}
		wantKeys := []string{
			"after", "alive", "blocking", "detail", "elapsed", "glyph", "label", "pr_number", "reason", "ref",
			"session_path", "state", "title", "url",
		}
		for _, row := range rows {
			keys := make([]string, 0, len(row))
			for key := range row {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, wantKeys) {
				t.Errorf("graph row keys = %v, want %v", keys, wantKeys)
			}
			var ticketURL, ref, sessionPath string
			if err := json.Unmarshal(row["url"], &ticketURL); err != nil {
				t.Fatalf("decode graph url: %v", err)
			}
			if err := json.Unmarshal(row["ref"], &ref); err != nil {
				t.Fatalf("decode graph ref: %v", err)
			}
			if err := json.Unmarshal(row["session_path"], &sessionPath); err != nil {
				t.Fatalf("decode graph session_path: %v", err)
			}
			wantRef := ticketRef(ticketURL)
			wantPath := "/s/acme/web/" + ticketURL[len("https://github.com/acme/web/issues/"):]
			if ref != wantRef || sessionPath != wantPath {
				t.Errorf("graph row %q ref and session_path = %q, %q, want %q, %q", ticketURL, ref, sessionPath, wantRef, wantPath)
			}
		}
	})
}
