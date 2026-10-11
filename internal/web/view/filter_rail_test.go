package view

import (
	"slices"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func searchRows() []Row {
	rows := []Row{
		railRow("12", "billing", plan.GlyphFailed),
		railRow("2", "billing", plan.GlyphRunning),
		railRow("3", "billing", plan.GlyphRunning),
		railRow("40", "checkout", plan.GlyphRunning),
		railRow("5", "checkout", plan.GlyphDone),
	}
	rows[0].Title = "Fix invoice rounding"
	rows[1].Title = "Add tax lines"
	rows[2].Title = "Refund flow"
	rows[3].Title = "Cart totals"
	rows[4].Title = "Old thing"
	rows[4].Worktree = "/wt"
	rows[3].Repo = "web"
	return rows
}

func TestFilterRailNarrowsEachSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query    string
		needsYou []string
		live     map[string][]string
		settled  []string
	}{
		{query: "", needsYou: []string{"#12"}, live: map[string][]string{"billing": {"#2", "#3"}, "checkout": {"#40"}}, settled: []string{"#5"}},
		{query: "#12", needsYou: []string{"#12"}, live: map[string][]string{}},
		{query: "12", needsYou: []string{"#12"}, live: map[string][]string{}},
		{query: "INVOICE", needsYou: []string{"#12"}, live: map[string][]string{}},
		{query: "web", live: map[string][]string{"checkout": {"#40"}}},
		{query: "billing", needsYou: []string{"#12"}, live: map[string][]string{"billing": {"#2", "#3"}}},
		{query: "refund", live: map[string][]string{"billing": {"#3"}}},
		{query: "billing refund", live: map[string][]string{"billing": {"#3"}}},
		{query: "refund cart", live: map[string][]string{}},
		{query: "old", live: map[string][]string{}, settled: []string{"#5"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()

			rail := buildRail(searchRows(), time.Now(), RailParams{Query: tt.query})

			if got := refs(rail.NeedsYou); !slices.Equal(got, tt.needsYou) {
				t.Errorf("NeedsYou = %v, want %v", got, tt.needsYou)
			}
			live := map[string][]string{}
			for _, f := range rail.Features {
				live[f.Name] = refs(f.Live)
			}
			if len(live) != len(tt.live) {
				t.Errorf("features = %v, want %v", live, tt.live)
			}
			for name, want := range tt.live {
				if got := live[name]; !slices.Equal(got, want) {
					t.Errorf("%s live = %v, want %v", name, got, want)
				}
			}
			if got := refs(rail.Settled.Items); !slices.Equal(got, tt.settled) {
				t.Errorf("Settled = %v, want %v", got, tt.settled)
			}
		})
	}
}

func TestFilterRailForcesSurvivorsOpenOnlyUnderAQuery(t *testing.T) {
	t.Parallel()

	rows := searchRows()
	plain := buildRail(rows, time.Now(), RailParams{})
	searched := buildRail(rows, time.Now(), RailParams{Query: "billing"})

	if plain.Query != "" || plain.Features[0].Open || plain.Settled.Open {
		t.Errorf("rail without a query forced something open: %+v", plain)
	}
	if searched.Query != "billing" || !searched.Features[0].Open || !searched.Settled.Open {
		t.Errorf("rail under a query left a survivor closed: %+v", searched)
	}
}

func TestRailNoMatch(t *testing.T) {
	t.Parallel()

	if rail := buildRail(searchRows(), time.Now(), RailParams{Query: "zzz"}); !rail.NoMatch() {
		t.Errorf("NoMatch() = false for a query nothing matches, rail %+v", rail)
	}
	if rail := buildRail(searchRows(), time.Now(), RailParams{}); rail.NoMatch() {
		t.Error("NoMatch() = true without a query")
	}
}
