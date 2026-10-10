package view

import "slices"

// StackLink is one ticket in a session's feature stack: a blocker, or a ticket waiting on it.
type StackLink struct {
	Ref   string
	Glyph string
	State string
	Path  string
}

// Stack is the session's place in its feature: what it waits on and what merging it unlocks.
type Stack struct {
	Blockers []StackLink
	Unlocks  []StackLink
}

func buildStack(board Board, row Row) Stack {
	rows := rowsIn(board.Groups)
	byURL := make(map[string]Row, len(rows))
	for _, r := range rows {
		byURL[r.URL] = r
	}
	link := func(url string) StackLink {
		known := byURL[url]
		return StackLink{Ref: ticketRef(url), Path: SessionPath(url), Glyph: known.Glyph, State: known.State}
	}
	var s Stack
	for _, url := range row.BlockedBy {
		s.Blockers = append(s.Blockers, link(url))
	}
	for _, r := range rows {
		if slices.Contains(r.BlockedBy, row.URL) {
			s.Unlocks = append(s.Unlocks, link(r.URL))
		}
	}
	return s
}
