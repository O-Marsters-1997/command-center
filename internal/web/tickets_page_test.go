package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
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
		webtest.RowFor(t, page, url)
	}
	root := htmltest.Parse(t, page)
	all := root.One(t, `nav[aria-label="ticket filters"] a[href="/tickets"]`)
	attrs := map[string]string{
		"hx-get": "/tickets", "hx-select": "#main", "hx-target": "#main",
		"hx-swap": "outerHTML", "hx-push-url": "true",
	}
	for name, want := range attrs {
		if got, _ := all.Attr(name); got != want {
			t.Errorf("All %s = %q, want %q: %s", name, got, want, all.HTML())
		}
	}
	if current, _ := all.Attr("aria-current"); current != "page" {
		t.Errorf("All aria-current = %q: %s", current, all.HTML())
	}
	if get, _ := root.One(t, "table#board").Attr("hx-get"); get != "/board?all=1" {
		t.Errorf("board hx-get = %q", get)
	}
}

func TestTicketsFilterKeepsOneSectionAndMarksItsTab(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=blocked")

	if got := sectionKeys(t, page); got != "blocked" {
		t.Errorf("sections = %q, want blocked", got)
	}
	root := htmltest.Parse(t, page)
	blocked := root.One(t, `nav[aria-label="ticket filters"] a[href="/tickets?filter=blocked"]`)
	attrs := map[string]string{
		"hx-get": "/tickets?filter=blocked", "hx-select": "#main",
		"hx-target": "#main", "hx-swap": "outerHTML", "hx-push-url": "true",
	}
	for name, want := range attrs {
		if got, _ := blocked.Attr(name); got != want {
			t.Errorf("Blocked %s = %q, want %q: %s", name, got, want, blocked.HTML())
		}
	}
	if current, _ := blocked.Attr("aria-current"); current != "page" {
		t.Errorf("Blocked aria-current = %q: %s", current, blocked.HTML())
	}
	if get, _ := root.One(t, "table#board").Attr("hx-get"); get != "/board?all=1&filter=blocked" {
		t.Errorf("filtered board hx-get = %q", get)
	}
}

func TestTicketsUnknownFilterShowsAll(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets?filter=bogus")

	if got := sectionKeys(t, page); got != "ready,blocked" {
		t.Errorf("sections = %q, want ready,blocked", got)
	}
}
