package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func testLayoutTitleAndCurrentNav(t *testing.T) {
	t.Parallel()

	server := threeFeatureServer(t)
	tests := []struct {
		name        string
		path        string
		wantTitle   string
		wantCurrent string
	}{
		{name: "tickets", path: "/tickets", wantTitle: "Tickets · Command Centre", wantCurrent: "/tickets"},
		{name: "insights", path: "/insights", wantTitle: "Insights · Command Centre", wantCurrent: "/insights"},
		{name: "repos", path: "/repos", wantTitle: "Repos · Command Centre", wantCurrent: "/repos"},
		{name: "launch", path: "/launch", wantTitle: "Launch · Command Centre", wantCurrent: "/tickets"},
		{name: "feature", path: "/f/board-scope", wantTitle: "board-scope · Command Centre", wantCurrent: "/tickets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			page := renderPath(t, server, tt.path)

			root := htmltest.Parse(t, page)
			if got := root.One(t, "title").Text(); got != tt.wantTitle {
				t.Errorf("GET %s title = %q, want %q", tt.path, got, tt.wantTitle)
			}
			current := root.Find(`nav#sidebar-nav a[aria-current="page"]`)
			if len(current) != 1 {
				t.Errorf("GET %s current sidebar items = %d, want one", tt.path, len(current))
			} else if href, _ := current[0].Attr("href"); !strings.HasPrefix(href, tt.wantCurrent) {
				t.Errorf("GET %s current sidebar href = %q, want prefix %q: %s", tt.path, href, tt.wantCurrent, current[0].HTML())
			}
		})
	}
}

func testLayoutSidebarOOB(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets")

	root := htmltest.Parse(t, page)
	root.One(t, "#sidebar-head")
	root.One(t, "#sidebar-nav")
	app := root.One(t, "[hx-select-oob]")
	if got, _ := app.Attr("hx-select-oob"); got != "#sidebar-head,#sidebar-nav" {
		t.Errorf("app hx-select-oob = %q: %s", got, app.HTML())
	}
}
