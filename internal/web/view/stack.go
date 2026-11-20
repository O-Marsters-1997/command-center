package view

import (
	"fmt"
	"net/url"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type StackLink struct {
	Ref     string
	Title   string
	Glyph   plan.GlyphWord
	Label   string
	Path    string
	Metric  string
	Current bool
	Wave    int
}

func (s Stack) members() []StackLink {
	var all []StackLink
	for _, w := range s.Waves {
		all = append(all, w.Members...)
	}
	return all
}

func (s Stack) withGlyph(keep func(plan.GlyphWord) bool) []StackLink {
	return slices.DeleteFunc(s.members(), func(l StackLink) bool { return !keep(l.Glyph) })
}

func (s Stack) Live() []StackLink {
	return s.withGlyph(func(g plan.GlyphWord) bool {
		return g != plan.GlyphDone && g != plan.GlyphBlocked && g != plan.GlyphReady
	})
}

func (s Stack) ReadyLinks() []StackLink {
	return s.withGlyph(func(g plan.GlyphWord) bool { return g == plan.GlyphReady })
}

func (s Stack) BlockedLinks() []StackLink {
	return s.withGlyph(func(g plan.GlyphWord) bool { return g == plan.GlyphBlocked })
}

func (s Stack) MergedLinks() []StackLink {
	return s.withGlyph(func(g plan.GlyphWord) bool { return g == plan.GlyphDone })
}

type StackFold struct {
	Label string
	Links []StackLink
}

func (s Stack) Folds() []StackFold {
	return []StackFold{
		{Label: "Ready to launch", Links: s.ReadyLinks()},
		{Label: "Blocked", Links: s.BlockedLinks()},
		{Label: "Merged", Links: s.MergedLinks()},
	}
}

func (s Stack) MergedPct() int {
	if s.Total == 0 {
		return 0
	}
	return s.Merged * 100 / s.Total
}

type StackWave struct {
	N       int
	Members []StackLink
}

type Stack struct {
	Blockers  []StackLink
	Unlocks   []StackLink
	Feature   string
	Project   ProjectMark
	Merged    int
	Total     int
	GraphPath string
	BoardPath string
	Waves     []StackWave
}

func (r Row) stackMetric() string {
	switch {
	case r.Alive && r.Elapsed != "":
		return r.Elapsed
	case r.SpendPctWeek > 0:
		return fmt.Sprintf("%.2f%%", r.SpendPctWeek)
	}
	return ""
}

func buildStack(board Board, row Row) Stack {
	rows := rowsIn(board.Groups)
	byURL := make(map[string]Row, len(rows))
	for _, r := range rows {
		byURL[r.URL] = r
	}
	link := func(ticketURL string) StackLink {
		known := byURL[ticketURL]
		return StackLink{
			Ref: ticketRef(ticketURL), Title: known.Title, Path: SessionPath(ticketURL),
			Glyph: known.Glyph, Label: known.Label, Metric: known.stackMetric(),
			Current: ticketURL == row.URL,
		}
	}
	var s Stack
	for _, blocker := range row.BlockedBy {
		s.Blockers = append(s.Blockers, link(blocker))
	}
	for _, r := range rows {
		if slices.Contains(r.BlockedBy, row.URL) {
			s.Unlocks = append(s.Unlocks, link(r.URL))
		}
	}
	if row.Feature == "" {
		return s
	}
	s.Feature = row.Feature
	s.BoardPath = "/f/" + url.PathEscape(row.Feature)
	s.GraphPath = s.BoardPath + "/graph"
	s.Project = row.Mark()
	var members []Row
	blocking := make(map[plan.TicketURL][]plan.TicketURL)
	for _, r := range rows {
		if r.Feature != row.Feature {
			continue
		}
		members = append(members, r)
		blocking[plan.TicketURL(r.URL)] = plan.TicketURLs(r.BlockedBy)
		s.Total++
		if r.State == plan.Merged.String() {
			s.Merged++
		}
	}
	wave := plan.Waves(blocking)
	for _, r := range members {
		n := wave[plan.TicketURL(r.URL)]
		if n > len(s.Waves) {
			s.Waves = append(s.Waves, make([]StackWave, n-len(s.Waves))...)
		}
		s.Waves[n-1].N = n
		member := link(r.URL)
		member.Wave = n
		s.Waves[n-1].Members = append(s.Waves[n-1].Members, member)
	}
	return s
}
