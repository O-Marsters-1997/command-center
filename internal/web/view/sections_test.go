package view_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestSectionedOrdersByAttentionAndDropsEmptySections(t *testing.T) {
	t.Parallel()

	rows := []view.Row{
		{URL: "done", Glyph: plan.GlyphDone},
		{URL: "blocked", Glyph: plan.GlyphBlocked},
		{URL: "running", Glyph: plan.GlyphRunning},
		{URL: "failed", Glyph: plan.GlyphFailed},
		{URL: "attention", Glyph: plan.GlyphAttention},
		{URL: "checking", Glyph: plan.GlyphChecking},
	}
	want := []struct {
		name  string
		title string
		urls  []string
	}{
		{name: "needs you", title: "Needs you", urls: []string{"failed", "attention"}},
		{name: "in progress", title: "In progress", urls: []string{"running", "checking"}},
		{name: "blocked", title: "Blocked", urls: []string{"blocked"}},
		{name: "done", title: "Done", urls: []string{"done"}},
	}

	got := view.Sectioned(rows)

	if len(got) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()

			if got[i].Title != w.title {
				t.Errorf("section %d = %q, want %q", i, got[i].Title, w.title)
			}
			var urls []string
			for _, r := range got[i].Rows {
				urls = append(urls, r.URL)
			}
			if diff := cmp.Diff(w.urls, urls); diff != "" {
				t.Errorf("%s rows (-want +got):\n%s", w.title, diff)
			}
		})
	}
}

func TestSectionedPresentsTheBoardBands(t *testing.T) {
	t.Parallel()
	want := []struct {
		name  string
		glyph plan.GlyphWord
		key   view.SectionKey
		color plan.GlyphWord
		stat  string
		title string
	}{
		{name: "needs you", glyph: plan.GlyphAttention, key: view.SectionNeedsYou,
			color: plan.GlyphAttention, stat: "need you", title: "Needs you"},
		{name: "in progress", glyph: plan.GlyphRunning, key: view.SectionInProgress,
			color: plan.GlyphRunning, stat: "in progress", title: "In progress"},
		{name: "ready", glyph: plan.GlyphReady, key: view.SectionReady,
			color: plan.GlyphReady, stat: "ready", title: "Ready to launch"},
		{name: "blocked", glyph: plan.GlyphBlocked, key: view.SectionBlocked,
			color: plan.GlyphBlocked, stat: "blocked", title: "Blocked"},
		{name: "done", glyph: plan.GlyphDone, key: view.SectionDone,
			color: plan.GlyphDone, stat: "merged", title: "Done"},
	}
	for _, tt := range want {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := view.Sectioned([]view.Row{{Glyph: tt.glyph}})
			if len(got) != 1 {
				t.Fatalf("Sectioned(%s) = %v, want one section", tt.glyph, got)
			}
			if got[0].Key != tt.key || got[0].Glyph != tt.color || got[0].Stat != tt.stat || got[0].Title != tt.title {
				t.Errorf("Sectioned(%s) = %+v, want key=%s glyph=%s stat=%q title=%q",
					tt.glyph, got[0], tt.key, tt.color, tt.stat, tt.title)
			}
		})
	}
}
