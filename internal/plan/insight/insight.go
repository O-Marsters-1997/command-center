package insight

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

type Range string

const (
	Range7d  Range = "7d"
	Range30d Range = "30d"
	Range90d Range = "90d"
)

func ParseRange(s string) (Range, error) {
	switch r := Range(s); r {
	case Range7d, Range30d, Range90d:
		return r, nil
	}
	return "", fmt.Errorf("parse range %q: want 7d, 30d or 90d", s)
}

func (r Range) days() int {
	switch r {
	case Range7d:
		return 7
	case Range30d:
		return 30
	case Range90d:
		return 90
	}
	return 0
}

type Window struct{ Since, Until time.Time }

func Windows(r Range, now time.Time) (current, prior Window) {
	span := time.Duration(r.days()) * 24 * time.Hour
	return Window{Since: now.Add(-span), Until: now}, Window{Since: now.Add(-2 * span), Until: now.Add(-span)}
}

type Metric string

const (
	MetricTokens Metric = "tokens"
	MetricPct    Metric = "pct"
	MetricWall   Metric = "wall"
	MetricTools  Metric = "tools"
)

type View string

const (
	ViewTicket View = "ticket"
	ViewWeek   View = "week"
)

type Tone string

const (
	ToneFlat Tone = "flat"
	ToneGood Tone = "good"
	ToneBad  Tone = "bad"
)

const MinChangeTickets = 10

const rollingTickets = 10

type TicketFact struct {
	Ticket, Title, Repo, Feature string
	FirstRunAt, MergedAt         time.Time
	WeightUSD                    float64
	Tokens                       int64
	ToolCalls                    int
}

type Change struct {
	Fraction float64
	Tone     Tone
}

type Tile struct {
	Metric Metric
	Median float64
	N      int
	Change option.Option[Change]
	Spark  []float64
}

type Dot struct {
	Ticket, Title string
	MergedAt      time.Time
	Value         float64
	Rolling       float64
}

type Bucket struct {
	Start  time.Time
	Median float64
	N      int
}

type Cost struct {
	Tiles       []Tile
	Selected    Metric
	Dots        []Dot
	Buckets     []Bucket
	RangeMedian float64
	Calibrating bool
}

var metrics = []Metric{MetricTokens, MetricPct, MetricWall, MetricTools}

func DeriveCost(current, prior []TicketFact, r Range, metric Metric, fit spend.Result) Cost {
	calibrating := fit.Samples < spend.MinSamples
	valueOf := func(m Metric) func(TicketFact) float64 {
		return func(f TicketFact) float64 {
			switch m {
			case MetricTokens:
				return float64(f.Tokens)
			case MetricPct:
				if calibrating {
					return 0
				}
				return spend.PctWeek(f.WeightUSD, fit.Factor)
			case MetricWall:
				return f.MergedAt.Sub(f.FirstRunAt).Seconds()
			case MetricTools:
				return float64(f.ToolCalls)
			}
			return 0
		}
	}
	sorted := slices.Clone(current)
	slices.SortStableFunc(sorted, func(a, b TicketFact) int { return a.MergedAt.Compare(b.MergedAt) })

	cost := Cost{Selected: metric, Calibrating: calibrating}
	for _, m := range metrics {
		value := valueOf(m)
		cur, pri := values(sorted, value), values(prior, value)
		curMedian := median(cur)
		tile := Tile{Metric: m, Median: curMedian, N: len(cur), Change: change(curMedian, median(pri), len(cur), len(pri))}
		for _, b := range buckets(sorted, r, value) {
			tile.Spark = append(tile.Spark, b.Median)
		}
		cost.Tiles = append(cost.Tiles, tile)
		if m == metric {
			cost.RangeMedian = tile.Median
		}
	}

	selected := valueOf(metric)
	all := values(sorted, selected)
	for i, f := range sorted {
		cost.Dots = append(cost.Dots, Dot{
			Ticket: f.Ticket, Title: f.Title, MergedAt: f.MergedAt, Value: all[i],
			Rolling: median(all[max(0, i-rollingTickets+1) : i+1]),
		})
	}
	cost.Buckets = buckets(sorted, r, selected)
	return cost
}

func values(facts []TicketFact, value func(TicketFact) float64) []float64 {
	out := make([]float64, len(facts))
	for i, f := range facts {
		out[i] = value(f)
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	if n := len(s); n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[len(s)/2]
}

func change(cur, prior float64, nCur, nPrior int) option.Option[Change] {
	if nCur < MinChangeTickets || nPrior < MinChangeTickets || prior == 0 {
		return option.None[Change]()
	}
	fraction := (cur - prior) / prior
	return option.Some(Change{Fraction: fraction, Tone: toneOf(fraction)})
}

func toneOf(fraction float64) Tone {
	switch cmp.Compare(fraction, 0) {
	case -1:
		return ToneGood
	case 1:
		return ToneBad
	}
	return ToneFlat
}

func buckets(sorted []TicketFact, r Range, value func(TicketFact) float64) []Bucket {
	if len(sorted) == 0 {
		return nil
	}
	count, step := 7, 1
	if r != Range7d {
		count, step = 5, 7
		if r == Range90d {
			count = 13
		}
	}
	last := bucketStart(sorted[len(sorted)-1].MergedAt, step)
	out := make([]Bucket, count)
	groups := make([][]float64, count)
	for i := range out {
		out[i].Start = last.AddDate(0, 0, (i-count+1)*step)
	}
	for _, f := range sorted {
		start := bucketStart(f.MergedAt, step)
		if i := slices.IndexFunc(out, func(b Bucket) bool { return b.Start.Equal(start) }); i >= 0 {
			groups[i] = append(groups[i], value(f))
		}
	}
	for i, g := range groups {
		out[i].Median, out[i].N = median(g), len(g)
	}
	return out
}

func bucketStart(t time.Time, stepDays int) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if stepDays == 1 {
		return day
	}
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}
