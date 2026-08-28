package cc

import (
	"cmp"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

type bandView struct {
	Fleet  fleetCard
	Stack  stackCard
	Checks checksCard
}

type fleetCard struct {
	Total    int
	Yours    int
	Segments []stateSegment
}

type stateSegment struct {
	State string
	Tone  string
	Count int
}

type stackCard struct {
	HasWorktree bool
	Deepest     int
	Bars        []depthBar
}

type depthBar struct {
	Depth int
	Count int
	// Pct sizes the bar against the histogram's own largest bucket: a template cannot divide.
	Pct int
}

type checksCard struct {
	Total    int
	Reported int
	Green    int
	Red      int
}

func deriveBand(rows []row) bandView {
	return bandView{Fleet: deriveFleetCard(rows), Stack: deriveStackCard(rows), Checks: deriveChecksCard(rows)}
}

func deriveFleetCard(rows []row) fleetCard {
	counts := make(map[string]int, len(rows))
	tones := make(map[string]string, len(rows))
	yours := 0
	for _, r := range rows {
		counts[r.State]++
		tones[r.State] = r.Tone
		if !r.Unattended {
			yours++
		}
	}
	segments := make([]stateSegment, 0, len(counts))
	for state, count := range counts {
		segments = append(segments, stateSegment{State: state, Tone: tones[state], Count: count})
	}
	slices.SortFunc(segments, func(a, b stateSegment) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.State, b.State)
	})
	return fleetCard{Total: len(rows), Yours: yours, Segments: segments}
}

func deriveStackCard(rows []row) stackCard {
	counts := make(map[int]int, len(rows))
	deepest := 0
	hasWorktree := false
	for _, r := range rows {
		counts[r.StackDepth]++
		if r.StackDepth > deepest {
			deepest = r.StackDepth
		}
		if r.Worktree != "" {
			hasWorktree = true
		}
	}
	if !hasWorktree {
		return stackCard{}
	}

	largest := 0
	for _, count := range counts {
		if count > largest {
			largest = count
		}
	}
	depths := slices.Sorted(maps.Keys(counts))
	bars := make([]depthBar, 0, len(depths))
	for _, d := range depths {
		bars = append(bars, depthBar{Depth: d, Count: counts[d], Pct: counts[d] * 100 / largest})
	}
	return stackCard{HasWorktree: true, Deepest: deepest, Bars: bars}
}

func deriveChecksCard(rows []row) checksCard {
	card := checksCard{Total: len(rows)}
	for _, r := range rows {
		if len(r.Checks) == 0 {
			continue
		}
		card.Reported++
		for _, c := range r.Checks {
			switch toVerdictCheckState(gh.CheckState{Status: c.Status, Conclusion: c.Conclusion}) {
			case verdict.Success, verdict.Skipped:
				card.Green++
			case verdict.Failure:
				card.Red++
			default:
			}
		}
	}
	return card
}
