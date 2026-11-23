package insight_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/insight"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

var (
	day0      = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	calibrate = spend.Result{Factor: 0.01, Samples: spend.MinSamples}
)

func fact(merged time.Time, tokens int64, tools int, wall time.Duration, usd float64) insight.TicketFact {
	return insight.TicketFact{
		Ticket: "t", MergedAt: merged, FirstRunAt: merged.Add(-wall),
		Tokens: tokens, ToolCalls: tools, WeightUSD: usd,
	}
}

func facts(n int, tokens int64) []insight.TicketFact {
	out := make([]insight.TicketFact, n)
	for i := range out {
		out[i] = fact(day0.Add(time.Duration(i)*time.Hour), tokens, 1, time.Minute, 1)
	}
	return out
}

func tile(t *testing.T, c insight.Cost, m insight.Metric) insight.Tile {
	t.Helper()
	for _, tl := range c.Tiles {
		if tl.Metric == m {
			return tl
		}
	}
	t.Fatalf("no tile for %s", m)
	return insight.Tile{}
}

func TestParseRange(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"7d", "30d", "90d"} {
		if got, err := insight.ParseRange(s); err != nil || string(got) != s {
			t.Errorf("ParseRange(%q) = %q, %v, want it back", s, got, err)
		}
	}
	if _, err := insight.ParseRange("1y"); err == nil {
		t.Error("ParseRange(1y) = nil error, want one")
	}
}

func TestWindows(t *testing.T) {
	t.Parallel()
	cur, prior := insight.Windows(insight.Range7d, day0)
	if !cur.Until.Equal(day0) || !cur.Since.Equal(day0.AddDate(0, 0, -7)) {
		t.Errorf("current = %+v, want the 7 days to now", cur)
	}
	if !prior.Until.Equal(cur.Since) || !prior.Since.Equal(day0.AddDate(0, 0, -14)) {
		t.Errorf("prior = %+v, want the 7 days before current", prior)
	}
}

func TestDeriveCost_Medians(t *testing.T) {
	t.Parallel()
	cur := []insight.TicketFact{
		fact(day0, 100, 1, 10*time.Minute, 1),
		fact(day0.Add(time.Hour), 300, 5, 30*time.Minute, 3),
		fact(day0.Add(2*time.Hour), 200, 3, 20*time.Minute, 2),
		fact(day0.Add(3*time.Hour), 400, 7, 40*time.Minute, 4),
	}
	c := insight.DeriveCost(cur, nil, insight.Range7d, insight.MetricTokens, calibrate)
	tests := []struct {
		metric insight.Metric
		want   float64
	}{
		{insight.MetricTokens, 250},
		{insight.MetricPct, 2.5},
		{insight.MetricWall, 1500},
		{insight.MetricTools, 4},
	}
	for _, tt := range tests {
		t.Run(string(tt.metric), func(t *testing.T) {
			t.Parallel()
			got := tile(t, c, tt.metric)
			if got.Median != tt.want || got.N != 4 {
				t.Errorf("%s tile = median %v over %d, want %v over 4", tt.metric, got.Median, got.N, tt.want)
			}
		})
	}
	if c.RangeMedian != 250 {
		t.Errorf("RangeMedian = %v, want the selected metric's median 250", c.RangeMedian)
	}
}

func TestDeriveCost_Change(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cur, pri []insight.TicketFact
		want     option.Option[insight.Change]
	}{
		{"lower is good", facts(10, 80), facts(10, 100), option.Some(insight.Change{Fraction: -0.2, Tone: insight.ToneGood})},
		{"higher is bad", facts(10, 150), facts(10, 100), option.Some(insight.Change{Fraction: 0.5, Tone: insight.ToneBad})},
		{"equal is flat", facts(10, 100), facts(10, 100), option.Some(insight.Change{Fraction: 0, Tone: insight.ToneFlat})},
		{"current under the floor", facts(9, 80), facts(10, 100), option.None[insight.Change]()},
		{"prior under the floor", facts(10, 80), facts(9, 100), option.None[insight.Change]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := insight.DeriveCost(tt.cur, tt.pri, insight.Range30d, insight.MetricTokens, calibrate)
			got := tile(t, c, insight.MetricTokens).Change
			if got.IsSome() != tt.want.IsSome() || (got.IsSome() && cmpChange(got.Unwrap(), tt.want.Unwrap())) {
				t.Errorf("Change = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func cmpChange(a, b insight.Change) bool {
	const eps = 1e-9
	d := a.Fraction - b.Fraction
	return a.Tone != b.Tone || d > eps || d < -eps
}

func TestDeriveCost_Buckets(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cur := []insight.TicketFact{
		fact(monday.Add(2*time.Hour), 10, 1, time.Minute, 1),
		fact(monday.Add(26*time.Hour), 30, 1, time.Minute, 1),
		fact(monday.Add(27*time.Hour), 50, 1, time.Minute, 1),
		fact(monday.AddDate(0, 0, 6).Add(time.Hour), 70, 1, time.Minute, 1),
	}
	tests := []struct {
		r         insight.Range
		wantCount int
		wantStart time.Time
		wantLast  insight.Bucket
	}{
		{insight.Range7d, 7, monday, insight.Bucket{Start: monday.AddDate(0, 0, 6), Median: 70, N: 1}},
		{insight.Range30d, 5, monday.AddDate(0, 0, -28), insight.Bucket{Start: monday, Median: 40, N: 4}},
		{insight.Range90d, 13, monday.AddDate(0, 0, -84), insight.Bucket{Start: monday, Median: 40, N: 4}},
	}
	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			t.Parallel()
			c := insight.DeriveCost(cur, nil, tt.r, insight.MetricTokens, calibrate)
			if len(c.Buckets) != tt.wantCount || len(tile(t, c, insight.MetricTokens).Spark) != tt.wantCount {
				t.Fatalf("buckets = %d, spark = %d, want %d of each",
					len(c.Buckets), len(tile(t, c, insight.MetricTokens).Spark), tt.wantCount)
			}
			if !c.Buckets[0].Start.Equal(tt.wantStart) {
				t.Errorf("first bucket starts %v, want %v", c.Buckets[0].Start, tt.wantStart)
			}
			if diff := cmp.Diff(tt.wantLast, c.Buckets[len(c.Buckets)-1]); diff != "" {
				t.Errorf("last bucket (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeriveCost_Rolling(t *testing.T) {
	t.Parallel()
	cur := make([]insight.TicketFact, 12)
	for i := range cur {
		cur[i] = fact(day0.Add(time.Duration(i)*time.Hour), int64(i+1), 1, time.Minute, 1)
	}
	slices.Reverse(cur)
	c := insight.DeriveCost(cur, nil, insight.Range7d, insight.MetricTokens, calibrate)
	if len(c.Dots) != 12 {
		t.Fatalf("Dots = %d, want 12", len(c.Dots))
	}
	if got := c.Dots[0]; got.Value != 1 || got.Rolling != 1 {
		t.Errorf("first dot = %+v, want value and rolling 1 in merge order", got)
	}
	if got := c.Dots[11].Rolling; got != 7.5 {
		t.Errorf("last dot rolling = %v, want 7.5, the median of values 3 to 12", got)
	}
}

func TestDeriveCost_Calibrating(t *testing.T) {
	t.Parallel()
	cur := facts(3, 100)
	fit := spend.Result{Factor: 1, Samples: spend.MinSamples - 1}
	c := insight.DeriveCost(cur, nil, insight.Range7d, insight.MetricPct, fit)
	if !c.Calibrating {
		t.Error("Calibrating = false, want true under MinSamples")
	}
	if got := tile(t, c, insight.MetricPct).Median; got != 0 {
		t.Errorf("pct median = %v, want 0 while calibrating", got)
	}
	if got := tile(t, c, insight.MetricTokens).Median; got != 100 {
		t.Errorf("tokens median = %v, want 100 while calibrating", got)
	}
	if got := c.Dots[0].Value; got != 0 {
		t.Errorf("selected pct dot = %v, want 0", got)
	}
}
