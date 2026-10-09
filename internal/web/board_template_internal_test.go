package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestBoardNamesAnOutOfScopeGroupMembersOwnRepo(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Repo: "repo", State: "ready", Glyph: "ready"}
	child := view.Row{
		URL: "sandbox://CHILD", Repo: "services", State: "ready", Glyph: "ready", Blocking: []string{root.URL},
	}
	view := view.Board{
		Sections: view.Sectioned([]view.Row{root, child}), BoardPath: "/board?repo=repo",
		Chrome: view.Chrome{RepoScope: "repo"},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "board", view); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, ">services<") {
		t.Errorf("board did not name CHILD's own repo though it is out of the repo scope:\n%s", html)
	}
	rootRow := html[:strings.Index(html, "#CHILD")]
	if strings.Contains(rootRow, ">repo<") {
		t.Errorf("board named ROOT's own repo though it matches the scope:\n%s", rootRow)
	}
}

func TestBoardNamesAnOutOfScopeGroupMembersOwnFeature(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Feature: "board-scope", State: "ready", Glyph: "ready"}
	child := view.Row{
		URL: "sandbox://CHILD", Feature: "sqlc-migration", State: "ready", Glyph: "ready", Blocking: []string{root.URL},
	}
	view := view.Board{
		Sections:  view.Sectioned([]view.Row{root, child}),
		BoardPath: "/board?feature=board-scope",
		Chrome:    view.Chrome{FeatureScope: "board-scope"},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "board", view); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, ">sqlc-migration<") {
		t.Errorf("board did not name CHILD's own feature though it is out of the feature scope:\n%s", html)
	}
	rootRow := html[:strings.Index(html, "#CHILD")]
	if strings.Contains(rootRow, ">board-scope<") {
		t.Errorf("board named ROOT's own feature though it matches the scope:\n%s", rootRow)
	}
}

func TestBoardTemplateStacksTheSpendBarByKindAndMarksAnOpenTicket(t *testing.T) {
	t.Parallel()

	r := view.Row{
		URL: "sandbox://CC-1", State: "merged", Glyph: "done",
		AgentPctWeek: 1.5, ResolvePctWeek: 0.5, FollowUpPctWeek: 0.25,
		SpendPctWeek: 2.25, TicketOpen: true,
	}
	board := view.Board{Sections: view.Sectioned([]view.Row{r})}
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "board", board); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := buf.String()
	wants := []string{
		`data-kind="agent" style="flex-grow: 1.5"`,
		`data-kind="resolve" style="flex-grow: 0.5"`,
		`data-kind="follow_up" style="flex-grow: 0.25"`,
		"2.25% week",
		"open",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("board does not contain %q:\n%s", want, got)
		}
	}
}

func TestBoardTemplateOmitsOpenForAMergedTicket(t *testing.T) {
	t.Parallel()

	r := view.Row{
		URL: "sandbox://CC-1", State: "merged", Glyph: "done",
		AgentPctWeek: 1, SpendPctWeek: 1, TicketOpen: false,
	}
	board := view.Board{Sections: view.Sectioned([]view.Row{r})}
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "board", board); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := buf.String(); strings.Contains(got, "open") {
		t.Errorf("board contains \"open\" for a merged ticket:\n%s", got)
	}
}
