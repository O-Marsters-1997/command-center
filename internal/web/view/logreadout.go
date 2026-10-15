package view

import (
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

type workedLine struct {
	Label string
	Stats string
}

type runStats struct {
	wall  time.Duration
	turns int
}

func measure(run agentlog.Run) runStats {
	stats := runStats{wall: run.End}
	for _, phase := range run.Phases {
		stats.turns += phase.Turns
	}
	if r := run.Result; r != nil {
		stats.wall = r.Duration
		stats.turns = r.Turns
	}
	return stats
}

func (s runStats) worked() workedLine {
	line := workedLine{Label: "Worked for " + formatShort(s.wall)}
	if s.turns > 0 {
		line.Stats = "· " + plural(s.turns, "turn", "turns")
	}
	return line
}

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
