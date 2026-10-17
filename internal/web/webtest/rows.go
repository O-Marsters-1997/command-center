package webtest

import (
	"path"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

type BoardRow struct {
	Ref   string
	State string
	Title string
	Row   htmltest.Node
}

func BoardRows(tb testing.TB, page string) []BoardRow {
	tb.Helper()
	var rows []BoardRow
	for _, a := range htmltest.Parse(tb, page).One(tb, "div#board").Find("a[data-ref]") {
		ref, _ := a.Attr("data-ref")
		state, _ := a.One(tb, ".glyph").Attr("title")
		rows = append(rows, BoardRow{Ref: ref, State: state, Title: a.One(tb, "b").Text(), Row: a})
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
			if got, _ := row.Row.Attr("href"); got == href {
				return row
			}
		}
	} else if len(candidates) == 1 {
		return candidates[0]
	}
	tb.Fatalf("no board row for %s; found refs: %s", ticketURL, strings.Join(refs, ", "))
	return BoardRow{}
}
