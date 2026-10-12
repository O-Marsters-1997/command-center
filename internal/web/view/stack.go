package view

import (
	"net/url"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// StackLink is one ticket in a session's feature stack: a blocker, or a ticket waiting on it.
type StackLink struct {
	Ref   string
	Title string
	Glyph string
	State string
	Path  string
}

// Stack is the session's place in its feature: what it waits on and what merging it unlocks.
type Stack struct {
	Blockers  []StackLink
	Unlocks   []StackLink
	Feature   string
	Merged    int
	Total     int
	GraphPath string
	BoardPath string
	Members   []StackLink
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
			Glyph: known.Glyph, State: known.State,
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
	for _, r := range rows {
		if r.Feature != row.Feature {
			continue
		}
		s.Total++
		if r.State == plan.Merged.String() {
			s.Merged++
		}
		s.Members = append(s.Members, link(r.URL))
	}
	return s
}
