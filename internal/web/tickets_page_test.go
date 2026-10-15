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

	for _, ticketURL := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
		webtest.RowFor(t, page, ticketURL)
	}
	root := htmltest.Parse(t, page)
	all := root.One(t, `nav[aria-label="ticket filters"] a[href="/tickets"]`)
	for _, tt := range []struct{ name, want string }{
		{name: "hx-get", want: "/tickets"},
		{name: "hx-select", want: "#main"},
		{name: "hx-target", want: "#main"},
		{name: "hx-swap", want: "outerHTML"},
		{name: "hx-push-url", want: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := all.Attr(tt.name); got != tt.want {
				t.Errorf("All %s = %q, want %q: %s", tt.name, got, tt.want, all.HTML())
			}
		})
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
	for _, tt := range []struct{ name, want string }{
		{name: "hx-get", want: "/tickets?filter=blocked"},
		{name: "hx-select", want: "#main"},
		{name: "hx-target", want: "#main"},
		{name: "hx-swap", want: "outerHTML"},
		{name: "hx-push-url", want: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := blocked.Attr(tt.name); got != tt.want {
				t.Errorf("Blocked %s = %q, want %q: %s", tt.name, got, tt.want, blocked.HTML())
			}
		})
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
