package view

import "github.com/O-Marsters-1997/command-center/internal/plan"

// Section is one attention band of the board: the rows whose glyphs share a section key.
type Section struct {
	Key   string
	Title string
	Rows  []Row
}

var sectionOrder = []Section{
	{Key: "needs-you", Title: "Needs you"},
	{Key: "in-progress", Title: "In progress"},
	{Key: "ready", Title: "Ready"},
	{Key: "blocked", Title: "Blocked"},
	{Key: "done", Title: "Done"},
}

func sectionKey(glyph string) string {
	switch glyph {
	case plan.GlyphFailed, plan.GlyphAttention:
		return "needs-you"
	case plan.GlyphRunning, plan.GlyphPending, plan.GlyphChecking:
		return "in-progress"
	case plan.GlyphReady:
		return "ready"
	case plan.GlyphBlocked:
		return "blocked"
	default:
		return "done"
	}
}

// Sectioned buckets rows into the fixed attention order, keeping each row's relative order and
// dropping empty sections.
func Sectioned(rows []Row) []Section {
	var out []Section
	for _, s := range sectionOrder {
		for _, r := range rows {
			if sectionKey(r.Glyph) == s.Key {
				s.Rows = append(s.Rows, r)
			}
		}
		if len(s.Rows) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// nameDependencies fills each row's After (its unmet blockers, for a blocked row) and Unlocks
// (every ticket waiting on it) as ticket refs.
func nameDependencies(rows []Row, tickets []plan.Ticket) {
	unlocks := plan.Unlocks(tickets)
	glyphByURL := make(map[string]string, len(rows))
	for _, r := range rows {
		glyphByURL[r.URL] = r.Glyph
	}
	for i := range rows {
		r := &rows[i]
		for _, waiter := range unlocks[r.URL] {
			r.Unlocks = append(r.Unlocks, ticketRef(waiter))
		}
		if r.Glyph != plan.GlyphBlocked {
			continue
		}
		for _, blocker := range r.BlockedBy {
			if glyphByURL[blocker] != plan.GlyphDone {
				r.After = append(r.After, ticketRef(blocker))
			}
		}
	}
}
