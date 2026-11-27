package view_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestReader_ObserveStale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		age      time.Duration
		interval time.Duration
		want     bool
	}{
		{name: "a normal tick gap is fresh", age: 40 * time.Second, want: false},
		{name: "past the floor is stale", age: 70 * time.Second, want: true},
		{name: "slow ticks stretch the threshold", age: 70 * time.Second, interval: 40 * time.Second, want: false},
		{name: "past twice a slow tick is stale", age: 90 * time.Second, interval: 40 * time.Second, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := cctest.Store(t)
			obs := plan.Observation{ObservedAt: testNow.Add(-tt.age), TickInterval: tt.interval}
			if err := st.SaveObservation(t.Context(), obs); err != nil {
				t.Fatalf("SaveObservation: %v", err)
			}
			chrome, err := view.NewReader(st, "", view.ReaderSettings{}).Chrome(t.Context(), testNow, view.Params{})
			if err != nil {
				t.Fatalf("Chrome: %v", err)
			}
			if chrome.ObserveStale != tt.want {
				t.Errorf("ObserveStale at age %s, interval %s = %t, want %t", tt.age, tt.interval, chrome.ObserveStale, tt.want)
			}
		})
	}
}

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
