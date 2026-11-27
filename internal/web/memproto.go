package web

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const memTotalGB = 16.0

var memVariant string

type memProtoPage struct {
	view.ActivityPage
	Mem *memProto
}

type memProto struct {
	Variant   string
	Name      string
	Prev      string
	Next      string
	Hours     []memHour
	DayLabels []string
	Spikes    []memSpike
	Lanes     []memLane
	Band      []memHour
	Ticks     []memTick
	Threshold int
}

type memHour struct {
	At    time.Time
	Pct   int
	Spike bool
	Left  float64
}

func (h memHour) Title() string {
	return fmt.Sprintf("%s · %d%% · %.1f of %.0f GB", h.At.Format("Mon 15:04"), h.Pct, float64(h.Pct)/100*memTotalGB, memTotalGB)
}

type memRun struct {
	Ref    string
	Title  string
	Repo   string
	Start  time.Time
	End    time.Time
	PeakMB int
	Alive  bool
}

func (r memRun) Peak() string { return gb(r.PeakMB) }
func (r memRun) Share() int  { return min(100, r.PeakMB*100/3072) }

type memSpike struct {
	At     time.Time
	Pct    int
	Runs   []memRun
	Docker int
	Other  int
}

func (s memSpike) GB() string    { return fmt.Sprintf("%.1f", float64(s.Pct)/100*memTotalGB) }
func (s memSpike) Agents() string {
	total := 0
	for _, r := range s.Runs {
		total += r.PeakMB
	}
	return gb(total)
}
func (s memSpike) DockerGB() string { return gb(s.Docker) }
func (s memSpike) OtherGB() string  { return gb(s.Other) }

type memLane struct {
	memRun
	Left  float64
	Width float64
	Heavy bool
}

func (l memLane) LabelInside() bool { return l.Left+l.Width > 88 }

type memTick struct {
	Left  float64
	Label string
}

var memVariants = []struct{ key, name string }{
	{"A", "Run-log histogram"},
	{"B", "Spikes as events"},
	{"C", "Overlap lanes"},
}

func gb(mb int) string { return fmt.Sprintf("%.1f GB", float64(mb)/1024) }

func newMemProto(now time.Time, variant string) *memProto {
	i := 0
	for j, v := range memVariants {
		if v.key == variant {
			i = j
		}
	}
	n := len(memVariants)
	p := &memProto{
		Variant:   memVariants[i].key,
		Name:      memVariants[i].name,
		Prev:      memVariants[(i+n-1)%n].key,
		Next:      memVariants[(i+1)%n].key,
		Threshold: 90,
	}
	end := now.Truncate(time.Hour)
	start := end.Add(-167 * time.Hour)
	spikeAt := map[time.Time]int{
		end.Add(-3*24*time.Hour - 6*time.Hour): 97,
		end.Add(-22 * time.Hour):               94,
		end.Add(-4 * time.Hour):                91,
		end.Add(-2*24*time.Hour - 2*time.Hour): 86,
	}
	for h := range 168 {
		at := start.Add(time.Duration(h) * time.Hour)
		pct := 38 + int(14*math.Sin(float64(at.Hour()-6)/24*2*math.Pi)) + int(noise(at.Unix())*10)
		if s, ok := spikeAt[at]; ok {
			pct = s
		}
		p.Hours = append(p.Hours, memHour{At: at, Pct: pct, Spike: pct >= p.Threshold})
	}
	for d := range 7 {
		p.DayLabels = append(p.DayLabels, start.Add(time.Duration(d)*24*time.Hour).Format("Mon 2"))
	}

	runs := fakeRuns(end)
	for _, h := range p.Hours {
		if !h.Spike {
			continue
		}
		s := memSpike{At: h.At, Pct: h.Pct, Docker: 2140, Other: 5300}
		for _, r := range runs {
			if !r.Start.After(h.At.Add(time.Hour)) && !r.End.Before(h.At) {
				s.Runs = append(s.Runs, r)
			}
		}
		if len(s.Runs) == 0 {
			s.Runs = runs[:3]
		}
		p.Spikes = append([]memSpike{s}, p.Spikes...)
	}

	winStart := end.Add(-23 * time.Hour)
	span := float64(24 * time.Hour)
	pos := func(t time.Time) float64 { return math.Max(0, float64(t.Sub(winStart))/span*100) }
	for _, h := range p.Hours[144:] {
		h.Left = pos(h.At)
		p.Band = append(p.Band, h)
	}
	for _, r := range runs {
		if r.End.Before(winStart) {
			continue
		}
		left := pos(r.Start)
		p.Lanes = append(p.Lanes, memLane{memRun: r, Left: left, Width: math.Max(1.5, pos(r.End)-left), Heavy: r.PeakMB >= 2048})
	}
	for k := 0; k <= 24; k += 6 {
		at := winStart.Add(time.Duration(k) * time.Hour)
		p.Ticks = append(p.Ticks, memTick{Left: pos(at), Label: at.Format("15:04")})
	}
	return p
}

func fakeRuns(end time.Time) []memRun {
	h := func(n int) time.Time { return end.Add(time.Duration(n) * time.Hour) }
	return []memRun{
		{"twenty#57", "Go onboarding step operations", "twenty", h(-5), h(-3), 2710, false},
		{"twenty#56", "Go workspace member role and lab flag operations", "twenty", h(-6), h(-3), 2480, false},
		{"twenty#54", "Go membership primitives", "twenty", h(-6), end.Add(40 * time.Minute), 2320, true},
		{"command-center#1211", "Let a repo allowlist MCP servers", "command-center", h(-9), h(-8), 610, false},
		{"twenty#42", "Go generic record resolver design", "twenty", h(-24), h(-20), 2950, false},
		{"twenty#33", "Go page layout writes and dashboards", "twenty", h(-23), h(-21), 2290, false},
		{"twenty#4", "Route roles, permissions and webhooks to Go", "twenty", h(-22), h(-19), 1880, false},
		{"command-center#1200", "Insights: the pulse page", "command-center", h(-16), h(-14), 520, false},
		{"twenty#47", "Remaining objects through the record resolver", "twenty", h(-13), h(-11), 2105, false},
	}
}

func noise(seed int64) float64 {
	f := fnv.New32a()
	fmt.Fprint(f, seed)
	return float64(f.Sum32()%1000) / 1000
}

func memPeak(id int64, action string) string {
	if memVariant != "A" {
		return ""
	}
	if strings.Contains(strings.ToLower(action), "withdraw") {
		return "—"
	}
	return gb(300 + int(noise(id)*2900))
}

func memHeavy(peak string) bool {
	var v float64
	_, err := fmt.Sscanf(peak, "%f GB", &v)
	return err == nil && v >= 2
}
