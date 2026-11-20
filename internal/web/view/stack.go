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
	Wave    int
	Current bool
}

type StackFold struct {
	Label string
	Links []StackLink
}

type Stack struct {
	Blockers      []StackLink
	Unlocks       []StackLink
	Feature       string
	Project       ProjectMark
	Merged        int
	Total         int
	MergedPercent int
	GraphPath     string
	BoardPath     string
	InFlight      []StackLink
	Folds         []StackFold // Ready to launch, Blocked, Merged, in that order
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
	if s.Total > 0 {
		s.MergedPercent = s.Merged * 100 / s.Total
	}
	s.Folds = []StackFold{{Label: "Ready to launch"}, {Label: "Blocked"}, {Label: "Merged"}}
	foldOf := map[plan.GlyphWord]*StackFold{
		plan.GlyphReady: &s.Folds[0], plan.GlyphBlocked: &s.Folds[1], plan.GlyphDone: &s.Folds[2],
	}
	wave := plan.Waves(blocking)
	for _, r := range members {
		member := link(r.URL)
		member.Wave = wave[plan.TicketURL(r.URL)]
		if group, folded := plan.RailGroup(r.Glyph); group == plan.GroupInFlight && !folded {
			s.InFlight = append(s.InFlight, member)
		} else if fold, ok := foldOf[r.Glyph]; ok {
			fold.Links = append(fold.Links, member)
		}
	}
	return s
}
