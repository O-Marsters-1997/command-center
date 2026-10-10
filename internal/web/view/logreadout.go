package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

type workedLine struct {
	Label string `json:"label"`
	Stats string `json:"stats"`
}

type runStats struct {
	wall    time.Duration
	turns   int
	cost    float64
	runCost bool
}

func measure(run agentlog.Run) runStats {
	stats := runStats{wall: run.End}
	spend := 0.0
	for _, phase := range run.Phases {
		stats.turns += phase.Turns
		spend += phase.Spend
	}
	if spend > 0 {
		stats.cost, stats.runCost = spend, true
	}
	if r := run.Result; r != nil {
		stats.wall = r.Duration
		stats.turns = r.Turns
		if r.CostUSD > 0 {
			stats.cost, stats.runCost = r.CostUSD, true
		}
	}
	return stats
}

func (s runStats) worked() workedLine {
	var stats []string
	if s.turns > 0 {
		stats = append(stats, plural(s.turns, "turn", "turns"))
	}
	if s.runCost {
		stats = append(stats, money(s.cost))
	}
	line := workedLine{Label: "Worked for " + formatShort(s.wall)}
	if len(stats) > 0 {
		line.Stats = "· " + strings.Join(stats, " · ")
	}
	return line
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
