package webtest

import (
	"path"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

type BoardRow struct {
	Ref                                              string
	State                                            string
	Ticket, Stack, Checks, PR, Elapsed, Spend, Verbs htmltest.Node
	Row                                              htmltest.Node
}

func BoardRows(tb testing.TB, page string) []BoardRow {
	tb.Helper()
	board := htmltest.Parse(tb, page).One(tb, "table#board")
	headerRows := board.Find("tr")
	if len(headerRows) == 0 {
		tb.Fatalf("board has no header row:\n%s", board.HTML())
	}
	headers := headerRows[0].Find("th")
	names := make([]string, len(headers))
	for i, header := range headers {
		names[i] = header.Text()
	}
	var rows []BoardRow
	for _, tr := range board.Find("tr.group") {
		cells := tr.Find("td")
		if len(cells) != len(names) {
			tb.Fatalf("board row has %d cells for %d headers:\n%s", len(cells), len(names), tr.HTML())
		}
		byName := make(map[string]htmltest.Node, len(names))
		for i, name := range names {
			byName[name] = cells[i]
		}
		for _, name := range []string{"state", "ref", "ticket", "stack", "checks", "pr", "elapsed", "spend", "verbs"} {
			if _, ok := byName[name]; !ok {
				tb.Fatalf("board row missing %q column:\n%s", name, tr.HTML())
			}
		}
		anchors := byName["ref"].Find("a")
		if len(anchors) != 1 {
			tb.Fatalf("board row has %d ref anchors, want one:\n%s", len(anchors), tr.HTML())
		}
		ref := anchors[0].Text()
		state := byName["state"]
		glyphs := state.Find(".glyph")
		if len(glyphs) != 1 {
			tb.Fatalf("board row has %d state glyphs, want one:\n%s", len(glyphs), tr.HTML())
		}
		glyph := glyphs[0].Text()
		var queued []string
		for _, piece := range strings.Split(state.Text(), "·")[1:] {
			piece = strings.TrimSpace(piece)
			if strings.HasSuffix(piece, " queued") {
				queued = append(queued, piece)
			}
		}
		if len(queued) > 0 {
			glyph += " · " + strings.Join(queued, " · ")
		}
		rows = append(rows, BoardRow{
			Ref: ref, State: glyph, Ticket: byName["ticket"], Stack: byName["stack"],
			Checks: byName["checks"], PR: byName["pr"], Elapsed: byName["elapsed"],
			Spend: byName["spend"], Verbs: byName["verbs"], Row: tr,
		})
	}
	return rows
}

func RowFor(tb testing.TB, page, ticketURL string) BoardRow {
	tb.Helper()
	ref := "#" + path.Base(ticketURL)
	rows := BoardRows(tb, page)
	refs := make([]string, 0, len(rows))
	var candidates []BoardRow
	for _, row := range rows {
		refs = append(refs, row.Ref)
		if row.Ref == ref {
			candidates = append(candidates, row)
		}
	}
	parts := strings.Split(strings.TrimSuffix(ticketURL, "/"), "/")
	if len(parts) >= 4 && parts[len(parts)-2] == "issues" {
		href := "/s/" + strings.Join(parts[len(parts)-4:len(parts)-2], "/") + "/" + parts[len(parts)-1]
		for _, row := range candidates {
			if len(row.Row.Find(`a[href="`+href+`"]`)) == 1 {
				return row
			}
		}
	} else if len(candidates) == 1 {
		return candidates[0]
	}
	tb.Fatalf("no board row for %s; found refs: %s", ticketURL, strings.Join(refs, ", "))
	return BoardRow{}
}
