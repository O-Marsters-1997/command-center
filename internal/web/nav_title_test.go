package web_test

import (
	"regexp"
	"strings"
	"testing"
)

var (
	titleTag    = regexp.MustCompile(`<title>([^<]*)</title>`)
	currentNavs = regexp.MustCompile(`<a class="sidebar-item" href="([^"]*)"[^>]*aria-current="page"`)
)

func TestPageTitleAndCurrentNavFollowThePage(t *testing.T) {
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

			if m := titleTag.FindStringSubmatch(page); m == nil || m[1] != tt.wantTitle {
				t.Errorf("GET %s title = %v, want %q", tt.path, m, tt.wantTitle)
			}
			current := currentNavs.FindAllStringSubmatch(page, -1)
			if len(current) != 1 || !strings.HasPrefix(current[0][1], tt.wantCurrent) {
				t.Errorf("GET %s current sidebar items = %q, want one under %s", tt.path, current, tt.wantCurrent)
			}
		})
	}
}

func TestSidebarSwapsOutOfBandOnNavigation(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/tickets")

	for _, want := range []string{
		`hx-select-oob="#sidebar-head,#sidebar-nav"`,
		`id="sidebar-head"`,
		`id="sidebar-nav"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/tickets missing %s", want)
		}
	}
}
