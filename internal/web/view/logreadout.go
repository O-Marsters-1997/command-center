package view

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

type workedLine struct {
	Label string `json:"label"`
	Stats string `json:"stats"`
	Open  bool   `json:"open"`
}

type phaseSegment struct {
	Label    string `json:"label"`
	Title    string `json:"title"`
	Duration string `json:"duration"`
	Weight   int    `json:"weight"`
	Failed   bool   `json:"failed"`
	Active   bool   `json:"active"`
	Path     string `json:"path"`
}

type metric struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Note   string `json:"note"`
	Failed bool   `json:"failed"`
}

type readout struct {
	Metrics   []metric `json:"metrics"`
	Lead      string   `json:"lead"`
	Insight   string   `json:"insight"`
	ClearPath string   `json:"clear_path"`
}

type changedFiles struct {
	Count   int `json:"count"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type jumpLink struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type phaseStats struct {
	label    string
	duration time.Duration
	turns    int
	cost     float64
	mix      toolMix
	fails    int
}

type runStats struct {
	phases     []phaseStats
	wall       time.Duration
	span       time.Duration
	turns      int
	phaseTurns int
	cost       float64
	runCost    bool
	phaseCost  bool
	calls      int
	fails      int
	recovered  bool
}

func measure(run agentlog.Run) runStats {
	stats := runStats{wall: run.End, phases: make([]phaseStats, len(run.Phases))}
	spend := 0.0
	for i, phase := range run.Phases {
		end := run.End
		if i+1 < len(run.Phases) {
			end = run.Phases[i+1].At
		}
		ps := phaseStats{label: phaseLabel(phase), duration: max(0, end-phase.At), turns: phase.Turns, cost: phase.Spend}
		var pairs []callPair
		for _, e := range phase.Events {
			switch e.Kind {
			case agentlog.File, agentlog.Tool:
				pairs = append(pairs, callPair{call: e, called: true})
			case agentlog.Fail:
				ps.fails++
			default:
			}
		}
		ps.mix = mixOf(pairs)
		stats.phases[i] = ps
		stats.span += ps.duration
		stats.phaseTurns += ps.turns
		stats.calls += ps.mix.calls()
		stats.fails += ps.fails
		spend += phase.Spend
	}
	stats.turns = stats.phaseTurns

	if spend > 0 {
		stats.cost, stats.runCost, stats.phaseCost = spend, true, true
	}
	if r := run.Result; r != nil {
		stats.wall = r.Duration
		stats.turns = r.Turns
		stats.recovered = r.Outcome == "success"
		if r.CostUSD > 0 {
			stats.cost, stats.runCost = r.CostUSD, true
			if spend > 0 {
				for i := range stats.phases {
					stats.phases[i].cost *= r.CostUSD / spend
				}
			}
		}
	}
	return stats
}

func phaseLabel(phase agentlog.Phase) string {
	if phase.Skill == "" {
		return "start"
	}
	return phase.Skill
}

func (s runStats) worked(open bool) workedLine {
	var stats []string
	if s.turns > 0 {
		stats = append(stats, plural(s.turns, "turn", "turns"))
	}
	if s.runCost {
		stats = append(stats, money(s.cost))
	}
	line := workedLine{Label: "Worked for " + formatShort(s.wall), Open: open}
	if len(stats) > 0 {
		line.Stats = "· " + strings.Join(stats, " · ")
	}
	return line
}

func (s runStats) strip(phases []agentlog.Phase, params Params, selected int) []phaseSegment {
	segments := make([]phaseSegment, len(phases))
	for i, ps := range s.phases {
		target := strconv.Itoa(i)
		if i == selected {
			target = ""
		}
		segments[i] = phaseSegment{
			Label:    ps.label,
			Title:    phases[i].Note,
			Duration: formatShort(ps.duration),
			Weight:   max(1, int(ps.duration.Round(time.Second)/time.Second)),
			Failed:   ps.fails > 0,
			Active:   i == selected,
			Path:     params.withPhase(target).pagePath(),
		}
	}
	return segments
}

func (s runStats) readout(run agentlog.Run, params Params, selected int) readout {
	if len(s.phases) == 0 {
		return readout{}
	}
	if selected >= 0 {
		return s.phaseReadout(selected, params)
	}
	out := readout{Metrics: []metric{{Label: "Time", Value: formatShort(s.wall)}}}
	if s.turns > 0 {
		out.Metrics = append(out.Metrics, metric{Label: "Turns", Value: strconv.Itoa(s.turns)})
	}
	if s.runCost {
		out.Metrics = append(out.Metrics, metric{Label: "Cost", Value: money(s.cost)})
	}
	out.Metrics = append(out.Metrics, metric{Label: "Tool calls", Value: strconv.Itoa(s.calls)})
	if s.fails > 0 {
		failed := metric{Label: "Failed", Value: strconv.Itoa(s.fails), Failed: true}
		if s.recovered {
			failed.Note = "recovered"
		}
		out.Metrics = append(out.Metrics, failed)
	}

	if s.span <= 0 {
		return out
	}
	longest := slices.MaxFunc(s.phases, func(a, b phaseStats) int { return cmp.Compare(a.duration, b.duration) })
	out.Lead = longest.label
	out.Insight = fmt.Sprintf("took %d%% of the run", percent(float64(longest.duration), float64(s.span)))
	if s.phaseCost {
		out.Insight += fmt.Sprintf(" and %s of the cost", money(longest.cost))
	}
	if len(s.phases) > 1 {
		out.Insight += ". Select a phase to see where the time went."
	} else {
		out.Insight += "."
	}
	return out
}

func (s runStats) phaseReadout(selected int, params Params) readout {
	ps := s.phases[selected]
	out := readout{ClearPath: params.withPhase("").pagePath(), Lead: ps.label + "."}
	out.Metrics = append(out.Metrics, metric{
		Label: "Time", Value: formatShort(ps.duration),
		Note: fmt.Sprintf("%d%% of run", percent(float64(ps.duration), float64(s.span))),
	})
	if ps.turns > 0 {
		out.Metrics = append(out.Metrics, metric{Label: "Turns", Value: strconv.Itoa(ps.turns)})
	}
	if s.phaseCost {
		out.Metrics = append(out.Metrics, metric{
			Label: "Cost", Value: money(ps.cost),
			Note: fmt.Sprintf("%d%%", percent(ps.cost, s.cost)),
		})
	}
	out.Metrics = append(out.Metrics, metric{
		Label: "Tool calls", Value: strconv.Itoa(ps.mix.calls()), Note: ps.mix.kinds(),
	})
	if ps.fails > 0 {
		out.Metrics = append(out.Metrics, metric{Label: "Failed", Value: strconv.Itoa(ps.fails), Failed: true})
	}

	var clauses []string
	if ps.turns > 0 && s.phaseTurns > 0 {
		perTurn := ps.duration.Seconds() / float64(ps.turns)
		average := s.span.Seconds() / float64(s.phaseTurns)
		clauses = append(clauses, fmt.Sprintf("%.0fs per turn, %s", perTurn, pace(perTurn, average)))
	}
	switch ps.fails {
	case 0:
		if len(clauses) == 0 {
			clauses = append(clauses, "no checks failed")
		}
	case 1:
		clauses = append(clauses, "one check failed before it settled")
	default:
		clauses = append(clauses, fmt.Sprintf("%d checks failed before it settled", ps.fails))
	}
	insight := strings.Join(clauses, "; ") + "."
	out.Insight = strings.ToUpper(insight[:1]) + insight[1:]
	return out
}

func pace(perTurn, average float64) string {
	switch {
	case perTurn > average*1.15:
		return "slower turns than the run's average"
	case perTurn < average*0.85:
		return "faster turns than the run's average"
	default:
		return "turns at the run's usual pace"
	}
}

func percent(part, whole float64) int {
	if whole <= 0 {
		return 0
	}
	return int(part/whole*100 + 0.5)
}

func money(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

func formatShort(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", d/time.Minute, d%time.Minute/time.Second)
	}
	return fmt.Sprintf("%dh %02dm", d/time.Hour, d%time.Hour/time.Minute)
}

func changedFilesOf(phases []agentlog.Phase) changedFiles {
	var changed changedFiles
	seen := map[string]bool{}
	for _, phase := range phases {
		for _, e := range phase.Events {
			if e.Kind != agentlog.File || categoryOf(e.Tool) != editing {
				continue
			}
			if !seen[e.Detail] {
				seen[e.Detail] = true
				changed.Count++
			}
			changed.Added += e.Diff.Added
			changed.Removed += e.Diff.Removed
		}
	}
	return changed
}

func jumpOf(phases []agentlog.Phase, params Params) jumpLink {
	first, fails := -1, 0
	for i, phase := range phases {
		for _, e := range phase.Events {
			if e.Kind != agentlog.Fail {
				continue
			}
			fails++
			if first < 0 {
				first = i
			}
		}
	}
	if fails == 0 {
		return jumpLink{}
	}
	return jumpLink{
		Label: plural(fails, "check", "checks") + " failed along the way",
		Path:  params.withPhase(strconv.Itoa(first)).pagePath() + "#first-fail",
	}
}
