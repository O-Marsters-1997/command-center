package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

// TestBoardNamesAnOutOfScopeGroupMembersOwnRepo covers issue #219 AC2 at the template's own
// seam: plan.Unlocked only ever counts a same-repo blocker (plan.go:60), so a group groupRows
// forms can never itself straddle two repos through today's live blocking edges -- this drives
// board.tmpl's row template directly, over a hand-built group, the way ADR 7 describes one.
func TestBoardNamesAnOutOfScopeGroupMembersOwnRepo(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Repo: "repo", State: "ready", Tone: "idle"}
	child := view.Row{URL: "sandbox://CHILD", Repo: "services", State: "ready", Tone: "idle", Blocking: []string{root.URL}}
	view := view.Board{
		Groups: []view.Group{{Root: &root, Children: []view.Row{child}}}, BoardPath: "/board?repo=repo",
		Chrome: view.Chrome{RepoScope: "repo"},
	}

	var buf bytes.Buffer
	if err := boardFragment.Execute(&buf, view); err != nil {
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

// TestBoardNamesAnOutOfScopeGroupMembersOwnFeature covers issue #220 AC2, following
// TestBoardNamesAnOutOfScopeGroupMembersOwnRepo.
func TestBoardNamesAnOutOfScopeGroupMembersOwnFeature(t *testing.T) {
	t.Parallel()

	root := view.Row{URL: "sandbox://ROOT", Feature: "board-scope", State: "ready", Tone: "idle"}
	child := view.Row{
		URL: "sandbox://CHILD", Feature: "sqlc-migration", State: "ready", Tone: "idle", Blocking: []string{root.URL},
	}
	view := view.Board{
		Groups:    []view.Group{{Root: &root, Children: []view.Row{child}}},
		BoardPath: "/board?feature=board-scope",
		Chrome:    view.Chrome{FeatureScope: "board-scope"},
	}

	var buf bytes.Buffer
	if err := boardFragment.Execute(&buf, view); err != nil {
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
		URL: "sandbox://CC-1", State: "merged", Tone: "done",
		AgentPctWeek: 1.5, ResolvePctWeek: 0.5, FollowUpPctWeek: 0.25,
		SpendPctWeek: 2.25, TicketOpen: true,
	}
	var buf bytes.Buffer
	if err := boardFragment.Execute(&buf, view.Board{Groups: []view.Group{{Children: []view.Row{r}}}}); err != nil {
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
		URL: "sandbox://CC-1", State: "merged", Tone: "done",
		AgentPctWeek: 1, SpendPctWeek: 1, TicketOpen: false,
	}
	var buf bytes.Buffer
	if err := boardFragment.Execute(&buf, view.Board{Groups: []view.Group{{Children: []view.Row{r}}}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := buf.String(); strings.Contains(got, "open") {
		t.Errorf("board contains \"open\" for a merged ticket:\n%s", got)
	}
}
