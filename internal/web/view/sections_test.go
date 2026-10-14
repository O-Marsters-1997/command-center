package view_test

import (
	"testing"

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
		title string
		urls  []string
	}{
		{"Needs you", []string{"failed", "attention"}},
		{"In progress", []string{"running", "checking"}},
		{"Blocked", []string{"blocked"}},
		{"Done", []string{"done"}},
	}

	got := view.Sectioned(rows)

	if len(got) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Title != w.title {
			t.Errorf("section %d = %q, want %q", i, got[i].Title, w.title)
		}
		var urls []string
		for _, r := range got[i].Rows {
			urls = append(urls, r.URL)
		}
		if len(urls) != len(w.urls) {
			t.Fatalf("%s rows = %v, want %v", w.title, urls, w.urls)
		}
		for j := range urls {
			if urls[j] != w.urls[j] {
				t.Errorf("%s rows = %v, want %v", w.title, urls, w.urls)
			}
		}
	}
}

func TestSectionedPresentsTheBoardBands(t *testing.T) {
	t.Parallel()
	want := []struct {
		glyph plan.GlyphWord
		key   view.SectionKey
		color plan.GlyphWord
		stat  string
		title string
	}{
		{plan.GlyphAttention, view.SectionNeedsYou, plan.GlyphAttention, "need you", "Needs you"},
		{plan.GlyphRunning, view.SectionInProgress, plan.GlyphRunning, "in progress", "In progress"},
		{plan.GlyphReady, view.SectionReady, plan.GlyphReady, "ready", "Ready to launch"},
		{plan.GlyphBlocked, view.SectionBlocked, plan.GlyphBlocked, "blocked", "Blocked"},
		{plan.GlyphDone, view.SectionDone, plan.GlyphDone, "merged", "Done"},
	}
	for _, tt := range want {
		t.Run(string(tt.key), func(t *testing.T) {
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
