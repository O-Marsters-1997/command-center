package insight

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type ConflictPath struct {
	Path      string
	Generated bool
}

type ConflictFact struct {
	Ticket, Repo, Feature string
	At                    time.Time
	Paths                 []ConflictPath
}

type OpenSpan struct {
	Ticket, Feature string
	From            time.Time
	To              option.Option[time.Time]
}

type HotspotRow struct {
	Path      string
	Generated bool
	Tickets   int
	Repeats   int
}

type StackPair struct {
	A, B, Path string
}

type ConflictFacts struct {
	Conflicts []ConflictFact
	Spans     []OpenSpan
}

func Hotspots(conflicts []ConflictFact) []HotspotRow {
	ordered := slices.SortedStableFunc(slices.Values(conflicts), func(a, b ConflictFact) int { return a.At.Compare(b.At) })
	type ticketPath struct{ ticket, path string }
	seen := make(map[ticketPath]bool)
	rows := make(map[string]*HotspotRow)
	for _, c := range ordered {
		for _, p := range c.Paths {
			row := rows[p.Path]
			if row == nil {
				row = &HotspotRow{Path: p.Path, Generated: p.Generated}
				rows[p.Path] = row
			}
			k := ticketPath{c.Ticket, p.Path}
			if seen[k] {
				row.Repeats++
				continue
			}
			seen[k] = true
			row.Tickets++
		}
	}
	out := make([]HotspotRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	slices.SortFunc(out, func(a, b HotspotRow) int {
		return cmp.Or(cmp.Compare(b.Tickets, a.Tickets), cmp.Compare(b.Repeats, a.Repeats), cmp.Compare(a.Path, b.Path))
	})
	return out
}

func StackPairs(f ConflictFacts) []StackPair {
	spans := make(map[string]OpenSpan, len(f.Spans))
	for _, s := range f.Spans {
		spans[s.Ticket] = s
	}
	type featurePath struct{ feature, path string }
	tickets := make(map[featurePath]map[string]bool)
	for _, c := range f.Conflicts {
		for _, p := range c.Paths {
			if p.Generated {
				continue
			}
			k := featurePath{c.Feature, p.Path}
			if tickets[k] == nil {
				tickets[k] = make(map[string]bool)
			}
			tickets[k][c.Ticket] = true
		}
	}
	var pairs []StackPair
	for k, set := range tickets {
		names := slices.Sorted(maps.Keys(set))
		for i, a := range names {
			for _, b := range names[i+1:] {
				sa, okA := spans[a]
				sb, okB := spans[b]
				if okA && okB && overlaps(sa, sb) {
					pairs = append(pairs, StackPair{A: a, B: b, Path: k.path})
				}
			}
		}
	}
	slices.SortFunc(pairs, func(x, y StackPair) int {
		return cmp.Or(cmp.Compare(x.A, y.A), cmp.Compare(x.B, y.B), cmp.Compare(x.Path, y.Path))
	})
	return pairs
}

func overlaps(a, b OpenSpan) bool {
	return endsAfter(a, b.From) && endsAfter(b, a.From)
}

func endsAfter(s OpenSpan, t time.Time) bool {
	to, closed := s.To.Get()
	return !closed || to.After(t)
}
