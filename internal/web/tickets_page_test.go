package web_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
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

func TestTicketsPage(t *testing.T) {
	t.Parallel()

	t.Run("shows every feature's tickets in sections with their derived states", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, threeFeatureServer(t), "/tickets")

		got := map[string]string{}
		for _, ticketURL := range []string{"sandbox://ROOT", "sandbox://CHILD", "sandbox://LONE"} {
			got[ticketURL] = webtest.RowFor(t, page, ticketURL).State
		}
		want := map[string]string{"sandbox://ROOT": "ready", "sandbox://CHILD": "blocked", "sandbox://LONE": "ready"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /tickets row states mismatch (-want +got):\n%s", diff)
		}
		if got := sectionKeys(t, page); got != "ready,blocked" {
			t.Errorf("GET /tickets sections = %q, want ready,blocked", got)
		}
	})

	tabs := []struct {
		name, path, tab, boardGet, sections string
	}{
		{name: "all", path: "/tickets", tab: "/tickets", boardGet: "/board?all=1", sections: "ready,blocked"},
		{
			name: "a filter keeps one section", path: "/tickets?filter=blocked", tab: "/tickets?filter=blocked",
			boardGet: "/board?all=1&filter=blocked", sections: "blocked",
		},
		{name: "an unknown filter shows all", path: "/tickets?filter=bogus", tab: "/tickets", sections: "ready,blocked"},
	}
	for _, tt := range tabs {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			page := renderPath(t, threeFeatureServer(t), tt.path)
			root := htmltest.Parse(t, page)

			if got := sectionKeys(t, page); got != tt.sections {
				t.Errorf("GET %s sections = %q, want %q", tt.path, got, tt.sections)
			}
			tab := root.One(t, `nav[aria-label="ticket filters"] a[href="`+tt.tab+`"]`)
			if diff := cmp.Diff(navAttrs(tt.tab), attrs(tab, navAttrNames...)); diff != "" {
				t.Errorf("GET %s tab %s mismatch (-want +got):\n%s", tt.path, tt.tab, diff)
			}
			if tt.boardGet == "" {
				return
			}
			if current, _ := tab.Attr("aria-current"); current != "page" {
				t.Errorf("GET %s tab %s aria-current = %q, want page", tt.path, tt.tab, current)
			}
			if get, _ := root.One(t, "table#board").Attr("hx-get"); get != tt.boardGet {
				t.Errorf("GET %s board hx-get = %q, want %q", tt.path, get, tt.boardGet)
			}
		})
	}
}
