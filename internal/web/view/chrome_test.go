package view_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestParseSidebar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		width     string
		collapsed string
		want      view.Sidebar
	}{
		{name: "no cookies", want: view.Sidebar{Width: 300}},
		{name: "in range", width: "360", collapsed: "0", want: view.Sidebar{Width: 360}},
		{name: "collapsed", width: "260", collapsed: "1", want: view.Sidebar{Width: 260, Collapsed: true}},
		{name: "too narrow", width: "40", want: view.Sidebar{Width: 220}},
		{name: "too wide", width: "9000", want: view.Sidebar{Width: 440}},
		{name: "negative", width: "-300", want: view.Sidebar{Width: 220}},
		{name: "not a number", width: "wide", collapsed: "yes", want: view.Sidebar{Width: 300}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := view.ParseSidebar(tt.width, tt.collapsed); got != tt.want {
				t.Errorf("ParseSidebar(%q, %q) = %+v, want %+v", tt.width, tt.collapsed, got, tt.want)
			}
		})
	}
}
