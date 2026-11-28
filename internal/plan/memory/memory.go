package memory

import (
	"cmp"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

const (
	SampleInterval = 30 * time.Second
	SpikeRatio     = 0.9
)

type SampleID int64

type ProcFact struct {
	RunID int64
	Pid   int
	Name  string
	Args  string
	RSS   int64
}

type Sample struct {
	At          time.Time
	Total, Used int64
	Procs       []ProcFact
}

type Preset string

const (
	Preset1h  Preset = "1h"
	Preset6h  Preset = "6h"
	Preset24h Preset = "24h"
	Preset7d  Preset = "7d"
)

type Window struct{ From, To time.Time }

func PresetWindow(p Preset, now time.Time) Window {
	span := 24 * time.Hour
	switch p {
	case Preset24h:
	case Preset1h:
		span = time.Hour
	case Preset6h:
		span = 6 * time.Hour
	case Preset7d:
		span = 7 * 24 * time.Hour
	}
	return Window{From: now.Add(-span), To: now}
}

func ParseWindow(from, to string, now time.Time) Window {
	f, ferr := time.Parse(time.RFC3339, from)
	t, terr := time.Parse(time.RFC3339, to)
	if ferr != nil || terr != nil || !f.Before(t) {
		return PresetWindow(Preset24h, now)
	}
	return Window{From: f, To: t}
}

func (w Window) Step() time.Duration {
	switch span := w.To.Sub(w.From); {
	case span <= time.Hour:
		return time.Minute
	case span <= 6*time.Hour:
		return 5 * time.Minute
	case span <= 24*time.Hour:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func (w Window) Live(now time.Time) bool {
	return now.Sub(w.To) <= SampleInterval
}

type BucketPeak struct {
	Start       time.Time
	Sample      SampleID
	Total, Used int64
}

type Bucket struct {
	Index      int
	Start, End time.Time
	Peak       option.Option[BucketPeak]
	Spike      bool
}

type Culprit struct {
	Name string
	RSS  int64
	Runs int
}

func Buckets(w Window, peaks []BucketPeak) []Bucket {
	step := w.Step()
	n := int((w.To.Sub(w.From) + step - 1) / step)
	out := make([]Bucket, max(n, 0))
	for i := range out {
		start := w.From.Add(time.Duration(i) * step)
		out[i] = Bucket{Index: i, Start: start, End: start.Add(step), Peak: option.None[BucketPeak]()}
	}
	for _, p := range peaks {
		if p.Start.Before(w.From) {
			continue
		}
		i := int(p.Start.Sub(w.From) / step)
		if i >= len(out) {
			continue
		}
		if cur, ok := out[i].Peak.Get(); !ok || p.Used > cur.Used {
			out[i].Peak = option.Some(p)
		}
	}
	for i := range out {
		if p, ok := out[i].Peak.Get(); ok {
			out[i].Spike = p.Total > 0 && float64(p.Used) >= SpikeRatio*float64(p.Total)
		}
	}
	return out
}

func Select(buckets []Bucket, at option.Option[time.Time]) int {
	if t, ok := at.Get(); ok {
		for _, b := range buckets {
			if !t.Before(b.Start) && t.Before(b.End) {
				return b.Index
			}
		}
	}
	return len(buckets) - 1
}

func CulpritOf(procs []ProcFact) option.Option[Culprit] {
	type tally struct {
		rss  int64
		runs map[int64]struct{}
	}
	byName := map[string]*tally{}
	for _, p := range procs {
		t := byName[p.Name]
		if t == nil {
			t = &tally{runs: map[int64]struct{}{}}
			byName[p.Name] = t
		}
		t.rss += p.RSS
		t.runs[p.RunID] = struct{}{}
	}
	cs := make([]Culprit, 0, len(byName))
	for name, t := range byName {
		cs = append(cs, Culprit{Name: name, RSS: t.rss, Runs: len(t.runs)})
	}
	if len(cs) == 0 {
		return option.None[Culprit]()
	}
	return option.Some(slices.MaxFunc(cs, func(a, b Culprit) int {
		return cmp.Or(cmp.Compare(a.RSS, b.RSS), cmp.Compare(b.Name, a.Name))
	}))
}
