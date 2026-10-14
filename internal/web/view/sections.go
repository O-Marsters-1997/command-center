package view

import "github.com/O-Marsters-1997/command-center/internal/plan"

// Section is one attention band of the board: the rows whose glyphs share a section key.
type Section struct {
	Key   SectionKey
	Glyph plan.GlyphWord
	Stat  string
	Title string
	Rows  []Row
}

var sectionOrder = []Section{
	{Key: SectionNeedsYou, Glyph: plan.GlyphAttention, Stat: "need you", Title: "Needs you"},
	{Key: SectionInProgress, Glyph: plan.GlyphRunning, Stat: "in progress", Title: "In progress"},
	{Key: SectionReady, Glyph: plan.GlyphReady, Stat: "ready", Title: "Ready"},
	{Key: SectionBlocked, Glyph: plan.GlyphBlocked, Stat: "blocked", Title: "Blocked"},
	{Key: SectionDone, Glyph: plan.GlyphDone, Stat: "merged", Title: "Done"},
}

func sectionKey(glyph plan.GlyphWord) SectionKey {
	switch glyph {
	case plan.GlyphFailed, plan.GlyphAttention:
		return SectionNeedsYou
	case plan.GlyphRunning, plan.GlyphPending, plan.GlyphChecking:
		return SectionInProgress
	case plan.GlyphReady:
		return SectionReady
	case plan.GlyphBlocked:
		return SectionBlocked
	case plan.GlyphDone, plan.GlyphUnknown:
		return SectionDone
	}
	return SectionDone
}

// Sectioned buckets rows into the fixed attention order, keeping each row's relative order and
// dropping empty sections.
func Sectioned(rows []Row) []Section {
	var out []Section
	for _, s := range sectionOrder {
		if s.Key == SectionReady {
			s.Title = "Ready to launch"
		}
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

// FilterTab is one pill of the all-tickets page: a section key (or "all"), its link and whether it
// is the filter in force.
type FilterTab struct {
	Key     SectionKey
	Title   string
	Path    string
	Count   int
	Current bool
}

func filterSections(sections []Section, filter SectionKey) []Section {
	if filter == SectionUnknown {
		return sections
	}
	var out []Section
	for _, s := range sections {
		if s.Key == filter {
			out = append(out, s)
		}
	}
	return out
}

func filterTabs(sections []Section, params Params) []FilterTab {
	filter := params.Filter
	total := 0
	counts := make(map[SectionKey]int, len(sections))
	for _, s := range sections {
		counts[s.Key] = len(s.Rows)
		total += len(s.Rows)
	}
	tabs := []FilterTab{{
		Key: SectionUnknown, Title: "All", Path: params.withScope(ticketsPath), Count: total,
		Current: filter == SectionUnknown,
	}}
	for _, s := range sectionOrder {
		tabs = append(tabs, FilterTab{
			Key: s.Key, Title: s.Title, Count: counts[s.Key], Current: filter == s.Key,
			Path: Params{Filter: s.Key, Project: params.Project, All: true}.pagePath(),
		})
	}
	return tabs
}

// nameDependencies fills each row's After (its unmet blockers, for a blocked row) and Unlocks
// (every ticket waiting on it) as ticket refs.
func nameDependencies(rows []Row, tickets []plan.Ticket) {
	unlocks := plan.Unlocks(tickets)
	glyphByURL := make(map[string]plan.GlyphWord, len(rows))
	for _, r := range rows {
		glyphByURL[r.URL] = r.Glyph
	}
	for i := range rows {
		r := &rows[i]
		for _, waiter := range unlocks[plan.TicketURL(r.URL)] {
			if _, onBoard := glyphByURL[string(waiter)]; !onBoard {
				continue
			}
			r.Unlocks = append(r.Unlocks, ticketRef(string(waiter)))
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
