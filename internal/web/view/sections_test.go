package view_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestSectioned(t *testing.T) {
	t.Parallel()

	t.Run("orders by attention and drops empty sections", func(t *testing.T) {
		t.Parallel()
		rows := []view.Row{
			{URL: "done", Glyph: plan.GlyphDone},
			{URL: "blocked", Glyph: plan.GlyphBlocked},
			{URL: "running", Glyph: plan.GlyphRunning},
			{URL: "failed", Glyph: plan.GlyphFailed},
			{URL: "attention", Glyph: plan.GlyphAttention},
			{URL: "checking", Glyph: plan.GlyphChecking},
		}
		type section struct {
			Key  view.SectionKey
			URLs []string
		}
		var got []section
		for _, s := range view.Sectioned(rows) {
			var urls []string
			for _, r := range s.Rows {
				urls = append(urls, r.URL)
			}
			got = append(got, section{Key: s.Key, URLs: urls})
		}
		want := []section{
			{Key: view.SectionNeedsYou, URLs: []string{"failed", "attention"}},
			{Key: view.SectionInProgress, URLs: []string{"running", "checking"}},
			{Key: view.SectionBlocked, URLs: []string{"blocked"}},
			{Key: view.SectionDone, URLs: []string{"done"}},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Sectioned() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("bands", func(t *testing.T) {
		t.Parallel()
		type band struct {
			Key   view.SectionKey
			Glyph plan.GlyphWord
			Stat  string
			Title string
		}
		tests := []struct {
			glyph plan.GlyphWord
			want  band
		}{
			{glyph: plan.GlyphAttention, want: band{view.SectionNeedsYou, plan.GlyphAttention, "need you", "Needs you"}},
			{glyph: plan.GlyphRunning, want: band{view.SectionInProgress, plan.GlyphRunning, "in progress", "In progress"}},
			{glyph: plan.GlyphReady, want: band{view.SectionReady, plan.GlyphReady, "ready", "Ready to launch"}},
			{glyph: plan.GlyphBlocked, want: band{view.SectionBlocked, plan.GlyphBlocked, "blocked", "Blocked"}},
			{glyph: plan.GlyphDone, want: band{view.SectionDone, plan.GlyphDone, "merged", "Done"}},
		}
		for _, tt := range tests {
			t.Run(string(tt.glyph), func(t *testing.T) {
				t.Parallel()
				got := view.Sectioned([]view.Row{{Glyph: tt.glyph}})
				if len(got) != 1 {
					t.Fatalf("Sectioned(%s) = %v, want one section", tt.glyph, got)
				}
				s := got[0]
				if diff := cmp.Diff(tt.want, band{Key: s.Key, Glyph: s.Glyph, Stat: s.Stat, Title: s.Title}); diff != "" {
					t.Errorf("Sectioned(%s) mismatch (-want +got):\n%s", tt.glyph, diff)
				}
			})
		}
	})
}
