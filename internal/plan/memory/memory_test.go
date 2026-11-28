package memory_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestParseWindow(t *testing.T) {
	t.Parallel()
	def := memory.PresetWindow(memory.Preset24h, now)
	from, to := "2026-06-01T10:00:00Z", "2026-06-01T11:00:00Z"
	tests := []struct {
		name     string
		from, to string
		want     memory.Window
	}{
		{"valid", from, to, memory.Window{From: now.Add(-2 * time.Hour), To: now.Add(-time.Hour)}},
		{"missing", "", "", def},
		{"missing to", from, "", def},
		{"malformed", "yesterday", to, def},
		{"reversed", to, from, def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, memory.ParseWindow(tt.from, tt.to, now)); diff != "" {
				t.Errorf("ParseWindow (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWindow_Step(t *testing.T) {
	t.Parallel()
	tests := []struct {
		span time.Duration
		want time.Duration
	}{
		{time.Hour, time.Minute},
		{time.Hour + time.Second, 5 * time.Minute},
		{6 * time.Hour, 5 * time.Minute},
		{6*time.Hour + time.Second, 15 * time.Minute},
		{24 * time.Hour, 15 * time.Minute},
		{24*time.Hour + time.Second, time.Hour},
	}
	for _, tt := range tests {
		got := memory.Window{From: now.Add(-tt.span), To: now}.Step()
		if got != tt.want {
			t.Errorf("Step at span %v = %v, want %v", tt.span, got, tt.want)
		}
	}
}

func TestWindow_Live(t *testing.T) {
	t.Parallel()
	w := memory.PresetWindow(memory.Preset7d, now)
	if got := w.To.Sub(w.From); got != 7*24*time.Hour {
		t.Errorf("7d span = %v", got)
	}
	if !w.Live(now.Add(memory.SampleInterval)) || w.Live(now.Add(memory.SampleInterval+time.Second)) {
		t.Error("Live should hold for one sample interval past the end, no longer")
	}
}

func TestBuckets(t *testing.T) {
	t.Parallel()
	w := memory.PresetWindow(memory.Preset1h, now)
	at := func(m int) time.Time { return w.From.Add(time.Duration(m) * time.Minute) }
	peaks := []memory.BucketPeak{
		{Start: at(0), Sample: 1, Total: 100, Used: 50},
		{Start: at(0).Add(20 * time.Second), Sample: 2, Total: 100, Used: 89},
		{Start: at(1), Sample: 3, Total: 100, Used: 90},
		{Start: at(2), Sample: 4, Total: 100, Used: 95},
		{Start: at(-5), Sample: 5, Total: 100, Used: 99},
		{Start: at(60), Sample: 6, Total: 100, Used: 99},
	}
	got := memory.Buckets(w, peaks)
	if len(got) != 60 {
		t.Fatalf("len(Buckets) = %d, want 60", len(got))
	}
	tests := []struct {
		i      int
		sample option.Option[memory.SampleID]
		spike  bool
	}{
		{0, option.Some(memory.SampleID(2)), false},
		{1, option.Some(memory.SampleID(3)), true},
		{2, option.Some(memory.SampleID(4)), true},
		{3, option.None[memory.SampleID](), false},
		{59, option.None[memory.SampleID](), false},
	}
	for _, tt := range tests {
		b := got[tt.i]
		sample := option.None[memory.SampleID]()
		if p, ok := b.Peak.Get(); ok {
			sample = option.Some(p.Sample)
		}
		if sample != tt.sample {
			t.Errorf("bucket %d peak sample = %v, want %v", tt.i, sample, tt.sample)
		}
		if b.Spike != tt.spike || b.Index != tt.i {
			t.Errorf("bucket %d = {Index %d, Spike %v}, want spike %v", tt.i, b.Index, b.Spike, tt.spike)
		}
	}
	if !got[0].End.Equal(at(1)) {
		t.Errorf("bucket 0 end = %v, want %v", got[0].End, at(1))
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	w := memory.PresetWindow(memory.Preset1h, now)
	bs := memory.Buckets(w, nil)
	tests := []struct {
		name string
		at   option.Option[time.Time]
		want int
	}{
		{"none is latest", option.None[time.Time](), 59},
		{"inside", option.Some(w.From.Add(5*time.Minute + time.Second)), 5},
		{"bucket start", option.Some(w.From.Add(5 * time.Minute)), 5},
		{"outside is latest", option.Some(w.From.Add(-time.Hour)), 59},
	}
	for _, tt := range tests {
		if got := memory.Select(bs, tt.at); got != tt.want {
			t.Errorf("%s: Select = %d, want %d", tt.name, got, tt.want)
		}
	}
	if got := memory.Select(nil, option.None[time.Time]()); got != -1 {
		t.Errorf("Select(nil) = %d, want -1", got)
	}
}

func TestCulpritOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		procs []memory.ProcFact
		want  option.Option[memory.Culprit]
	}{
		{"none", nil, option.None[memory.Culprit]()},
		{"sums across runs", []memory.ProcFact{
			{RunID: 1, Name: "node", RSS: 40},
			{RunID: 1, Name: "node", RSS: 10},
			{RunID: 2, Name: "node", RSS: 30},
			{RunID: 3, Name: "go", RSS: 70},
		}, option.Some(memory.Culprit{Name: "node", RSS: 80, Runs: 2})},
		{"tie goes to the first name", []memory.ProcFact{
			{RunID: 1, Name: "b", RSS: 5},
			{RunID: 2, Name: "a", RSS: 5},
		}, option.Some(memory.Culprit{Name: "a", RSS: 5, Runs: 1})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := memory.CulpritOf(tt.procs); got != tt.want {
				t.Errorf("CulpritOf = %v, want %v", got, tt.want)
			}
		})
	}
}
