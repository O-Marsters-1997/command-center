package view_test

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestParseInsightsParams(t *testing.T) {
	t.Parallel()

	t.Run("an empty query takes the defaults", func(t *testing.T) {
		t.Parallel()
		got := view.ParseInsightsParams(url.Values{})
		if got.Range != insight.Range30d || got.Metric != insight.MetricTokens || got.View != insight.ViewTicket {
			t.Errorf("ParseInsightsParams({}) = %v %v %v, want 30d tokens ticket", got.Range, got.Metric, got.View)
		}
	})

	t.Run("unknown values fall back", func(t *testing.T) {
		t.Parallel()
		got := view.ParseInsightsParams(url.Values{"range": {"1y"}, "metric": {"vibes"}, "view": {"pie"}})
		if got.Range != insight.Range30d || got.Metric != insight.MetricTokens || got.View != insight.ViewTicket {
			t.Errorf("ParseInsightsParams(unknown) = %v %v %v, want the defaults", got.Range, got.Metric, got.View)
		}
	})

	t.Run("a path keeps every choice that is not a default", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"range": {"7d"}, "metric": {"wall"}, "view": {"week"}, "repo": {"cc"}, "feature": {"f"}}
		p := view.ParseInsightsParams(q)
		want := "/insights?feature=f&metric=wall&range=7d&repo=cc&view=week"
		if got := p.ViewPath(insight.ViewWeek); got != want {
			t.Errorf("ViewPath(week) = %q, want %q", got, want)
		}
		if got := p.Days(); got != 7 {
			t.Errorf("Days() = %d, want 7", got)
		}
	})
}

func TestBuildTiles(t *testing.T) {
	t.Parallel()
	p := view.ParseInsightsParams(url.Values{})
	change := option.Some(insight.Change{Fraction: -0.125, Tone: insight.ToneGood})
	cost := insight.Cost{
		Selected: insight.MetricTokens,
		Buckets:  []insight.Bucket{{N: 2}, {N: 0}, {N: 1}},
		Tiles: []insight.Tile{
			{Metric: insight.MetricTokens, Median: 12500, N: 12, Change: change, Spark: []float64{1, 3, 2}},
			{Metric: insight.MetricWall, Median: 4500, N: 12, Spark: []float64{1, 3, 2}},
			{Metric: insight.MetricTools, Median: 0, N: 0},
		},
	}

	tiles := view.BuildTiles(p, cost, spend.Result{})
	figures := make([]string, len(tiles))
	chips := make([]string, len(tiles))
	for i, tile := range tiles {
		figures[i], chips[i] = tile.Figure, tile.Chip
	}
	if diff := cmp.Diff([]string{"12.5k", "1h 15m", "—"}, figures); diff != "" {
		t.Errorf("BuildTiles() figures mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"↘ -12.5%", "n too small", ""}, chips); diff != "" {
		t.Errorf("BuildTiles() chips mismatch (-want +got):\n%s", diff)
	}
	if tiles[0].Spark == "" {
		t.Errorf("BuildTiles() tokens spark is empty, want a path over two populated buckets")
	}

	cost.Buckets = []insight.Bucket{{N: 3}, {N: 0}, {N: 0}}
	if got := view.BuildTiles(p, cost, spend.Result{})[0].Spark; got != "" {
		t.Errorf("BuildTiles(one populated bucket) spark = %q, want it hidden", got)
	}

	cost.Calibrating = true
	cost.Tiles = []insight.Tile{{Metric: insight.MetricPct, Median: 0, N: 4}}
	got := view.BuildTiles(p, cost, spend.Result{Samples: 3})[0].Figure
	if want := "Calibrating (3 intervals)"; got != want {
		t.Errorf("BuildTiles(calibrating) pct figure = %q, want %q", got, want)
	}
}
