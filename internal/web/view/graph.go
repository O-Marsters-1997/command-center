package view

import (
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type GraphRow struct {
	URL         string         `json:"url"`
	Ref         string         `json:"ref"`
	Title       string         `json:"title"`
	State       string         `json:"state"`
	Glyph       plan.GlyphWord `json:"glyph"`
	Alive       bool           `json:"alive"`
	Blocking    []string       `json:"blocking"`
	After       []string       `json:"after"`
	Reason      string         `json:"reason"`
	Elapsed     string         `json:"elapsed"`
	PRNumber    int            `json:"pr_number"`
	SessionPath string         `json:"session_path"`
}

type GraphGroup struct {
	Root     option.Option[GraphRow] `json:"root"`
	Children []GraphRow              `json:"children"`
}

// Graph is the /graph.json body: groups in board order, each row slimmed to a GraphRow.
func Graph(groups []Group) []GraphGroup {
	result := make([]GraphGroup, 0, len(groups))
	for _, group := range groups {
		converted := GraphGroup{Children: make([]GraphRow, 0, len(group.Children))}
		if group.Root.IsSome() {
			converted.Root = option.Some(graphRow(group.Root.Unwrap()))
		}
		for _, child := range group.Children {
			converted.Children = append(converted.Children, graphRow(child))
		}
		result = append(result, converted)
	}
	return result
}

func graphRow(row Row) GraphRow {
	return GraphRow{
		URL: row.URL, Ref: row.Ticket(), Title: row.Title, State: row.State,
		Glyph: row.Glyph, Alive: row.Alive, Blocking: row.Blocking, After: row.After,
		Reason: row.Reason, Elapsed: row.Elapsed, PRNumber: row.PRNumber,
		SessionPath: SessionPath(row.URL),
	}
}
