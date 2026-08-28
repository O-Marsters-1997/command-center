package cc

import (
	"net/url"
	"testing"
)

func TestParseViewParamsTakesTheFirstSel(t *testing.T) {
	t.Parallel()

	q, err := url.ParseQuery("sel=a&sel=b&task=x&task=y&view=graph")
	if err != nil {
		t.Fatal(err)
	}
	got := parseViewParams(q)
	want := viewParams{Sel: "a", Tasks: []string{"x", "y"}, View: "graph"}
	if got.Sel != want.Sel || got.View != want.View || len(got.Tasks) != 2 ||
		got.Tasks[0] != "x" || got.Tasks[1] != "y" {
		t.Errorf("parseViewParams(%q) = %+v, want %+v", q, got, want)
	}
}

func TestParseViewParamsDefaultsViewToBoard(t *testing.T) {
	t.Parallel()

	got := parseViewParams(url.Values{})
	if got.View != "board" {
		t.Errorf("View = %q, want board", got.View)
	}
	if got.Sel != "" || got.Tasks != nil {
		t.Errorf("parseViewParams({}) = %+v, want the zero selection", got)
	}
}

func TestViewParamsQueryRoundTripsThroughParse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		v    viewParams
		want string
	}{
		{"empty", viewParams{View: "board"}, ""},
		{"sel only", viewParams{Sel: "https://x/1", View: "board"}, "sel=https%3A%2F%2Fx%2F1"},
		{
			"sel, tasks and a non-default view",
			viewParams{Sel: "a", Tasks: []string{"b", "c"}, View: "graph"},
			"sel=a&task=b&task=c&view=graph",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.v.query(); got != tc.want {
				t.Errorf("query() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestViewParamsBoardPathAndPagePathOmitTheQuestionMarkWhenEmpty(t *testing.T) {
	t.Parallel()

	empty := viewParams{View: "board"}
	if got := empty.boardPath(); got != "/board" {
		t.Errorf("boardPath() = %q, want /board", got)
	}
	if got := empty.pagePath(); got != "/" {
		t.Errorf("pagePath() = %q, want /", got)
	}

	selected := viewParams{Sel: "a", View: "board"}
	if got := selected.boardPath(); got != "/board?sel=a" {
		t.Errorf("boardPath() = %q, want /board?sel=a", got)
	}
	if got := selected.pagePath(); got != "/?sel=a" {
		t.Errorf("pagePath() = %q, want /?sel=a", got)
	}
}

func TestViewParamsToggleSelSelectsThenCollapses(t *testing.T) {
	t.Parallel()

	base := viewParams{Sel: "a", View: "board"}
	selectB := base.toggleSel("b")
	if selectB.Sel != "b" {
		t.Errorf("toggling an unselected row = %q, want it selected", selectB.Sel)
	}
	collapse := base.toggleSel("a")
	if collapse.Sel != "" {
		t.Errorf("toggling the already-selected row = %q, want the selection cleared", collapse.Sel)
	}
	if base.Sel != "a" {
		t.Errorf("toggleSel mutated the receiver: Sel = %q", base.Sel)
	}
}

func TestViewParamsToggleTaskAddsThenRemoves(t *testing.T) {
	t.Parallel()

	base := viewParams{Tasks: []string{"a", "b"}, View: "board"}
	added := base.toggleTask("c")
	if want := []string{"a", "b", "c"}; !equalStrings(added.Tasks, want) {
		t.Errorf("toggleTask(c) = %v, want %v", added.Tasks, want)
	}
	removed := base.toggleTask("a")
	if want := []string{"b"}; !equalStrings(removed.Tasks, want) {
		t.Errorf("toggleTask(a) = %v, want %v", removed.Tasks, want)
	}
	if want := []string{"a", "b"}; !equalStrings(base.Tasks, want) {
		t.Errorf("toggleTask mutated the receiver's own slice: Tasks = %v", base.Tasks)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
