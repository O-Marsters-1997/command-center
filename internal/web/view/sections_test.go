package view_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestReader_RailCountAgreesWithAllTab(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := cctest.Store(t)
	storetest.Tickets(t, st,
		store.Ticket{URL: "https://github.com/acme/web/issues/1", Repo: "acme/web", Branch: "cc-1", Feature: "alpha"},
		store.Ticket{URL: "https://github.com/acme/web/issues/2", Repo: "acme/web", Branch: "cc-2", Feature: "beta"},
	)
	reader := view.NewReader(st, "", view.ReaderSettings{})
	params := view.Params{Feature: "alpha"}

	board, err := reader.Board(ctx, testNow, params)
	if err != nil {
		t.Fatalf("Board(feature alpha): %v", err)
	}
	chrome, err := reader.Chrome(ctx, testNow, params)
	if err != nil {
		t.Fatalf("Chrome(feature alpha): %v", err)
	}
	if got, want := chrome.OpenTickets, board.Tabs[0].Count; got != want || want != 1 {
		t.Errorf("rail count = %d, All tab = %d, want both 1", got, want)
	}
}

func TestFilterTabs(t *testing.T) {
	t.Parallel()

	sections := []view.Section{
		{Key: view.SectionReady, Rows: []view.Row{{URL: "a"}}},
		{Key: view.SectionBlocked, Rows: []view.Row{{URL: "b"}}},
	}
	tests := []struct {
		name   string
		filter view.SectionKey
		want   []view.SectionKey
	}{
		{
			name:   "drops zero-count tabs",
			filter: view.SectionUnknown,
			want:   []view.SectionKey{view.SectionUnknown, view.SectionReady, view.SectionBlocked},
		},
		{
			name:   "keeps the selected zero-count tab",
			filter: view.SectionDone,
			want:   []view.SectionKey{view.SectionUnknown, view.SectionReady, view.SectionBlocked, view.SectionDone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []view.SectionKey
			for _, tab := range view.FilterTabs(sections, view.Params{Filter: tt.filter}) {
				got = append(got, tab.Key)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FilterTabs() keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

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
