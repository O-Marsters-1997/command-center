package web_test

import (
	"regexp"
	"strings"
	"testing"
)

func sectionKeys(page string) string {
	var got []string
	for _, m := range regexp.MustCompile(`data-section="([a-z-]+)"`).FindAllStringSubmatch(page, -1) {
		got = append(got, m[1])
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
	if !strings.Contains(page, `href="/tickets" aria-current="page">All`) {
		t.Errorf("/tickets does not mark All current:\n%s", page)
	}
	if !strings.Contains(page, `hx-get="/board?all=1"`) {
		t.Errorf("/tickets board does not poll its own fragment:\n%s", page)
	}
}

func TestTicketsFilterKeepsOneSectionAndMarksItsTab(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=blocked")

	if got := sectionKeys(page); got != "blocked" {
		t.Errorf("sections = %q, want blocked", got)
	}
	if !strings.Contains(page, `href="/tickets?filter=blocked" aria-current="page">Blocked`) {
		t.Errorf("blocked tab not current:\n%s", page)
	}
	if !strings.Contains(page, `hx-get="/board?all=1&amp;filter=blocked"`) {
		t.Errorf("filtered board polls the wrong fragment:\n%s", page)
	}
	assertGolden(t, "testdata/tickets_blocked.golden.html", []byte(page))
}

func TestTicketsUnknownFilterShowsAll(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=bogus")

	if got := sectionKeys(page); got != "ready,blocked" {
		t.Errorf("sections = %q, want ready,blocked", got)
	}
}
