package view

import (
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const shortHashLen = 7

type WorkflowSeries struct {
	Workflow   string          `json:"workflow"`
	ShortHash  string          `json:"short_hash"`
	Tickets    int             `json:"tickets"`
	MedianPct  float64         `json:"median_pct_week"`
	MedianWall string          `json:"median_wall"`
	Steps      []StepBreakdown `json:"steps"`

	MedianRework   float64 `json:"median_rework"`
	WithdrawnShare float64 `json:"withdrawn_share"`
}

type StepBreakdown struct {
	Step         string         `json:"step"`
	MedianPct    float64        `json:"median_pct_week"`
	MedianWall   string         `json:"median_wall"`
	MedianTurns  int            `json:"median_turns"`
	CacheReadPct float64        `json:"cache_read_pct"`
	Tools        map[string]int `json:"tools"`
}

type stepSamples struct {
	pcts, walls, turns, cacheReads []float64
	tools                          map[string][]float64
}

type workflowSamples struct {
	name, hash  string
	ticketPcts  []float64
	ticketWalls []float64
	reworks     []float64
	steps       map[plan.StepName]*stepSamples
	stepOrder   []plan.StepName
	tickets     int
}

func buildWorkflowSeries(tickets []store.WorkflowTicketSpend, withdrawn map[string]int, factor float64) []WorkflowSeries {
	var order []*workflowSamples
	byHash := make(map[string]*workflowSamples)
	for _, t := range tickets {
		w, ok := byHash[t.WorkflowHash]
		if !ok {
			w = &workflowSamples{name: t.Workflow, hash: t.WorkflowHash, steps: make(map[plan.StepName]*stepSamples)}
			byHash[t.WorkflowHash] = w
			order = append(order, w)
		}
		w.tickets++
		w.reworks = append(w.reworks, float64(t.Rework.Total()))
		var pct, wall float64
		var measured bool
		for _, s := range t.Steps {
			ss, ok := w.steps[s.Step]
			if !ok {
				ss = &stepSamples{tools: make(map[string][]float64)}
				w.steps[s.Step] = ss
				w.stepOrder = append(w.stepOrder, s.Step)
			}
			stepPct := spend.PctWeek(s.WeightUSD, factor)
			pct += stepPct
			ss.pcts = append(ss.pcts, stepPct)
			if !s.Metrics.Wall.IsSome() {
				continue
			}
			measured = true
			seconds := s.Metrics.Wall.Unwrap().Seconds()
			wall += seconds
			ss.walls = append(ss.walls, seconds)
			ss.turns = append(ss.turns, float64(s.Metrics.Turns))
			if total := s.Metrics.Input + s.Metrics.CacheWrite + s.Metrics.CacheRead; total > 0 {
				ss.cacheReads = append(ss.cacheReads, float64(s.Metrics.CacheRead)/float64(total)*100)
			}
			for tool, n := range s.Metrics.Tools {
				ss.tools[tool] = append(ss.tools[tool], float64(n))
			}
		}
		w.ticketPcts = append(w.ticketPcts, pct)
		if measured {
			w.ticketWalls = append(w.ticketWalls, wall)
		}
	}

	series := make([]WorkflowSeries, len(order))
	for i, w := range order {
		series[i] = WorkflowSeries{
			Workflow: w.name, ShortHash: w.hash[:min(shortHashLen, len(w.hash))], Tickets: w.tickets,
			MedianPct: median(w.ticketPcts), MedianWall: wallLabel(w.ticketWalls),
			Steps:        make([]StepBreakdown, len(w.stepOrder)),
			MedianRework: median(w.reworks),
		}
		if n := withdrawn[w.hash]; n > 0 {
			series[i].WithdrawnShare = float64(n) / float64(n+w.tickets)
		}
		for j, name := range w.stepOrder {
			ss := w.steps[name]
			tools := make(map[string]int, len(ss.tools))
			for tool, counts := range ss.tools {
				tools[tool] = int(median(counts) + 0.5)
			}
			series[i].Steps[j] = StepBreakdown{
				Step: string(name), MedianPct: median(ss.pcts), MedianWall: wallLabel(ss.walls),
				MedianTurns: int(median(ss.turns) + 0.5), CacheReadPct: median(ss.cacheReads), Tools: tools,
			}
		}
	}
	return series
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func wallLabel(seconds []float64) string {
	if len(seconds) == 0 {
		return ""
	}
	return (time.Duration(median(seconds) * float64(time.Second))).Round(time.Second).String()
}
