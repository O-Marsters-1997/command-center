package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func renderBoardForTest(t *testing.T, board view.Board) string {
	t.Helper()
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "board", board); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func hasSpanText(node htmltest.Node, text string) bool {
	for _, span := range node.Find("span") {
		if span.Text() == text {
			return true
		}
	}
	return false
}

func TestBoardNamesAnOutOfScopeGroupMembersOwnRepo(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Repo: "repo", State: "ready", Glyph: "ready"}
	child := view.Row{
		URL: "sandbox://CHILD", Repo: "services", State: "ready", Glyph: "ready", Blocking: []string{root.URL},
	}
	board := view.Board{
		Sections: view.Sectioned([]view.Row{root, child}), BoardPath: "/board?repo=repo",
		Chrome: view.Chrome{RepoScope: "repo"},
	}

	html := renderBoardForTest(t, board)
	childRow := webtest.RowFor(t, html, child.URL)
	rootRow := webtest.RowFor(t, html, root.URL)
	childRef := childRow.Row.Find("td")[2]
	rootRef := rootRow.Row.Find("td")[2]
	if !hasSpanText(childRef, "services") {
		t.Errorf("CHILD ref cell missing out-of-scope repo: %s", childRef.HTML())
	}
	if hasSpanText(rootRef, "repo") {
		t.Errorf("ROOT ref cell named in-scope repo: %s", rootRef.HTML())
	}
}

func TestBoardNamesAnOutOfScopeGroupMembersOwnFeature(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Feature: "board-scope", State: "ready", Glyph: "ready"}
	child := view.Row{
		URL: "sandbox://CHILD", Feature: "sqlc-migration", State: "ready", Glyph: "ready", Blocking: []string{root.URL},
	}
	board := view.Board{
		Sections:  view.Sectioned([]view.Row{root, child}),
		BoardPath: "/board?feature=board-scope",
		Chrome:    view.Chrome{FeatureScope: "board-scope"},
	}

	html := renderBoardForTest(t, board)
	childRow := webtest.RowFor(t, html, child.URL)
	rootRow := webtest.RowFor(t, html, root.URL)
	childRef := childRow.Row.Find("td")[2]
	rootRef := rootRow.Row.Find("td")[2]
	if !hasSpanText(childRef, "sqlc-migration") {
		t.Errorf("CHILD ref cell missing out-of-scope feature: %s", childRef.HTML())
	}
	if hasSpanText(rootRef, "board-scope") {
		t.Errorf("ROOT ref cell named in-scope feature: %s", rootRef.HTML())
	}
}

func TestBoardTemplateStacksTheSpendBarByKindAndMarksAnOpenTicket(t *testing.T) {
	t.Parallel()

	r := view.Row{
		URL: "sandbox://CC-1", State: "merged", Glyph: "done",
		AgentPctWeek: 1.5, FollowUpPctWeek: 0.25,
		SpendPctWeek: 1.75, TicketOpen: true,
	}
	board := view.Board{Sections: view.Sectioned([]view.Row{r})}
	spend := webtest.RowFor(t, renderBoardForTest(t, board), r.URL).Spend
	for _, tt := range []struct{ kind, grow string }{
		{"agent", "flex-grow: 1.5"},
		{"follow_up", "flex-grow: 0.25"},
	} {
		segments := spend.Find(`span[data-kind="` + tt.kind + `"]`)
		if len(segments) != 1 {
			t.Errorf("spend has %d %s segments, want one: %s", len(segments), tt.kind, spend.HTML())
			continue
		}
		if got, _ := segments[0].Attr("style"); got != tt.grow {
			t.Errorf("%s segment style = %q, want %q", tt.kind, got, tt.grow)
		}
	}
	if !strings.Contains(spend.Text(), "1.75% week") {
		t.Errorf("spend text = %q, want 1.75%% week", spend.Text())
	}
	if !hasSpanText(spend, "open") {
		t.Errorf("open ticket spend missing open marker: %s", spend.HTML())
	}
}

func TestBoardTemplateOmitsOpenForAMergedTicket(t *testing.T) {
	t.Parallel()

	r := view.Row{
		URL: "sandbox://CC-1", State: "merged", Glyph: "done",
		AgentPctWeek: 1, SpendPctWeek: 1, TicketOpen: false,
	}
	board := view.Board{Sections: view.Sectioned([]view.Row{r})}
	spend := webtest.RowFor(t, renderBoardForTest(t, board), r.URL).Spend
	if hasSpanText(spend, "open") {
		t.Errorf("merged ticket spend contains open marker: %s", spend.HTML())
	}
}
