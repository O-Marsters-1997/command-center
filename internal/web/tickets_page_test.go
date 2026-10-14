package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func sectionKeys(t *testing.T, page string) string {
	t.Helper()
	var got []string
	for _, section := range htmltest.Parse(t, page).One(t, "table#board").Find("tbody[data-section]") {
		key, _ := section.Attr("data-section")
		got = append(got, key)
	}
	return strings.Join(got, ",")
}

func TestTicketsPageShowsEveryFeatureInSections(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets")

	for _, url := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		if !strings.Contains(page, ticketRef(url)) {
			t.Errorf("/tickets missing %s:\n%s", ticketRef(url), page)
		}
	}
	if !strings.Contains(page, `href="/tickets" hx-get="/tickets"`+navBundleTail+` aria-current="page">All`) {
		t.Errorf("/tickets does not mark All current:\n%s", page)
	}
	if !strings.Contains(page, `hx-get="/board?all=1"`) {
		t.Errorf("/tickets board does not poll its own fragment:\n%s", page)
	}
}

func TestTicketsFilterKeepsOneSectionAndMarksItsTab(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=blocked")

	if got := sectionKeys(t, page); got != "blocked" {
		t.Errorf("sections = %q, want blocked", got)
	}
	want := `href="/tickets?filter=blocked" hx-get="/tickets?filter=blocked"` + navBundleTail +
		` aria-current="page">Blocked`
	if !strings.Contains(page, want) {
		t.Errorf("blocked tab not current:\n%s", page)
	}
	if !strings.Contains(page, `hx-get="/board?all=1&amp;filter=blocked"`) {
		t.Errorf("filtered board polls the wrong fragment:\n%s", page)
	}
}

func TestTicketsUnknownFilterShowsAll(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=bogus")

	if got := sectionKeys(t, page); got != "ready,blocked" {
		t.Errorf("sections = %q, want ready,blocked", got)
	}
}
