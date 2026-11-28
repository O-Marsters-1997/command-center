package view

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

const memoryMaxSpan = 31 * 24 * time.Hour

var memoryPresets = []memory.Preset{memory.Preset1h, memory.Preset6h, memory.Preset24h, memory.Preset7d}

type RangeLink struct {
	Label, Path string
	Current     bool
}

type Tick struct {
	Left  float64
	Label string
}

type MemoryCaption struct {
	Label   string
	GB      string
	Spike   bool
	Culprit option.Option[memory.Culprit]
}

type MemoryChart struct {
	Presets  []RangeLink
	Buckets  []memory.Bucket
	Ticks    []Tick
	Selected int
	Caption  option.Option[MemoryCaption]
	Live     bool
	readErr  string
}

func (c MemoryChart) ReadError() string { return c.readErr }

func (c MemoryChart) Threshold() int { return int(memory.SpikeRatio * 100) }

func (c MemoryChart) Pct(b memory.Bucket) int {
	p, ok := b.Peak.Get()
	if !ok || p.Total <= 0 {
		return 0
	}
	return int(min(100, max(0, p.Used*100/p.Total)))
}

func (c MemoryChart) Title(b memory.Bucket) string {
	p, ok := b.Peak.Get()
	if !ok {
		return bucketLabel(b) + ", no samples"
	}
	title := bucketLabel(b) + ", " + gigabytes(p.Used)
	if b.Spike {
		title += fmt.Sprintf(", over %d%%", c.Threshold())
	}
	return title
}

func (p ActivityPage) MemoryBucketPath(b memory.Bucket) string {
	return memoryPath(p.Params, p.memoryFrom(), p.memoryTo(), option.Some(b.Start))
}

func (p ActivityPage) MemoryPollPath() string {
	at := option.None[time.Time]()
	if c := p.Memory; c.Selected < len(c.Buckets)-1 {
		at = option.Some(c.Buckets[c.Selected].Start)
	}
	return memoryPath(p.Params, p.memoryFrom(), p.memoryTo(), at)
}

func (p ActivityPage) memoryFrom() time.Time { return p.Memory.Buckets[0].Start }
func (p ActivityPage) memoryTo() time.Time   { return p.Memory.Buckets[len(p.Memory.Buckets)-1].End }

func memoryPath(p ActivityParams, from, to time.Time, at option.Option[time.Time]) string {
	q, _ := url.ParseQuery(p.Encode())
	q.Set("from", strconv.FormatInt(from.Unix(), 10))
	q.Set("to", strconv.FormatInt(to.Unix(), 10))
	if t, ok := at.Get(); ok {
		q.Set("at", strconv.FormatInt(t.Unix(), 10))
	}
	return withQuery("/activity", q.Encode())
}

func bucketLabel(b memory.Bucket) string {
	return b.Start.Format("Mon 15:04") + "–" + b.End.Format("15:04")
}

func gigabytes(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/(1<<30)) }

func parseMemoryQuery(q url.Values, now time.Time) (memory.Window, option.Option[time.Time]) {
	unix := func(key string) (time.Time, bool) {
		n, err := strconv.ParseInt(q.Get(key), 10, 64)
		return time.Unix(n, 0).In(now.Location()), err == nil
	}
	w := memory.PresetWindow(memory.Preset24h, now)
	from, okFrom := unix("from")
	to, okTo := unix("to")
	if okFrom && okTo && from.Before(to) && to.Sub(from) <= memoryMaxSpan {
		w = memory.Window{From: from, To: to}
	}
	at := option.None[time.Time]()
	if t, ok := unix("at"); ok {
		at = option.Some(t)
	}
	return w, at
}

func slideToNow(w memory.Window, now time.Time) memory.Window {
	to := floorLocal(now, w.Step()).Add(w.Step())
	return memory.Window{From: to.Add(-w.To.Sub(w.From)), To: to}
}

func floorLocal(t time.Time, step time.Duration) time.Time {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return midnight.Add(t.Sub(midnight) / step * step)
}

func memoryTicks(w memory.Window) []Tick {
	span := w.To.Sub(w.From)
	var at []time.Time
	layout := "15:04"
	if span > 36*time.Hour {
		layout = "Mon 2"
		d := w.From
		for t := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, d.Location()); t.Before(w.To); t = t.AddDate(0, 0, 1) {
			at = append(at, t)
		}
	} else {
		step := time.Hour
		switch {
		case span <= 2*time.Hour:
			step = 15 * time.Minute
		case span > 12*time.Hour:
			step = 3 * time.Hour
		}
		for t := floorLocal(w.From, step).Add(step); t.Before(w.To); t = t.Add(step) {
			at = append(at, t)
		}
	}
	ticks := make([]Tick, len(at))
	for i, t := range at {
		ticks[i] = Tick{Left: float64(t.Sub(w.From)) / float64(span) * 100, Label: t.Format(layout)}
	}
	return ticks
}

func memoryPresetLinks(p ActivityParams, current memory.Window, now time.Time) []RangeLink {
	links := make([]RangeLink, len(memoryPresets))
	for i, preset := range memoryPresets {
		w := slideToNow(memory.PresetWindow(preset, now), now)
		links[i] = RangeLink{
			Label: string(preset), Path: memoryPath(p, w.From, w.To, option.None[time.Time]()),
			Current: w.To.Sub(w.From) == current.To.Sub(current.From),
		}
	}
	return links
}

func (r *Reader) memoryChart(ctx context.Context, p ActivityParams, q url.Values, now time.Time) MemoryChart {
	w, at := parseMemoryQuery(q, now)
	live := w.Live(now)
	if live {
		w = slideToNow(w, now)
	}
	chart := MemoryChart{Presets: memoryPresetLinks(p, w, now), Ticks: memoryTicks(w), Live: live}
	failed := func(err error) MemoryChart {
		return MemoryChart{Presets: chart.Presets, readErr: fmt.Sprintf("Could not load memory: %v", err)}
	}
	peaks, err := r.store.MemoryPeaks(ctx, w, w.Step())
	if err != nil {
		return failed(err)
	}
	chart.Buckets = memory.Buckets(w, peaks)
	chart.Selected = memory.Select(chart.Buckets, at)
	b := chart.Buckets[chart.Selected]
	caption := MemoryCaption{Label: bucketLabel(b), Spike: b.Spike, Culprit: option.None[memory.Culprit]()}
	if peak, ok := b.Peak.Get(); ok {
		procs, err := r.store.MemoryProcesses(ctx, peak.Sample)
		if err != nil {
			return failed(err)
		}
		caption.GB = gigabytes(peak.Used)
		caption.Culprit = memory.CulpritOf(procs)
	}
	chart.Caption = option.Some(caption)
	return chart
}
