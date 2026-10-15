package webtest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

type failingTB struct {
	testing.TB
	message string
}

func (f *failingTB) Helper() {}
func (f *failingTB) Fatalf(format string, args ...any) {
	f.message = fmt.Sprintf(format, args...)
	panic("fatal")
}

func TestBoardRowsFollowHeaderOrder(t *testing.T) {
	t.Parallel()

	cells := map[string]string{
		"state":  `<span class="glyph">ready</span> · launch queued`,
		"ref":    `<a href="/s/one/12">#12</a>`,
		"ticket": "Build", "stack": "L2", "checks": "1/1", "pr": "#8 open",
		"elapsed": "1m", "spend": "5 tok", "verbs": "launch",
	}
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{"normal", []string{"", "state", "ref", "ticket", "stack", "checks", "pr", "elapsed", "spend", "verbs"}},
		{"reordered", []string{"pr", "ticket", "ref", "state", "checks", "spend", "stack", "verbs", "elapsed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var page strings.Builder
			page.WriteString(`<table id="board"><tr>`)
			for _, name := range tc.order {
				fmt.Fprintf(&page, "<th>%s</th>", name)
			}
			page.WriteString(`</tr><tbody><tr class="group">`)
			for _, name := range tc.order {
				fmt.Fprintf(&page, "<td>%s</td>", cells[name])
			}
			page.WriteString(`</tr></tbody></table>`)
			row := webtest.RowFor(t, page.String(), "https://example.com/one/12")
			got := []string{row.Ref, row.State, row.Ticket.Text(), row.Stack.Text(),
				row.Checks.Text(), row.PR.Text(), row.Elapsed.Text(), row.Spend.Text(), row.Verbs.Text()}
			want := []string{"#12", "ready · launch queued", "Build", "L2", "1/1", "#8 open", "1m", "5 tok", "launch"}
			if !slices.Equal(got, want) {
				t.Errorf("row fields = %v, want %v:\n%s", got, want, row.Row.HTML())
			}
		})
	}
}

func TestRowForMissingListsRefs(t *testing.T) {
	t.Parallel()

	page := `<table id="board"><tr><th>ref</th><th>state</th><th>ticket</th>` +
		`<th>stack</th><th>checks</th><th>pr</th><th>elapsed</th>` +
		`<th>spend</th><th>verbs</th></tr><tr class="group">` +
		`<td><a href="/s/one/12">#12</a></td><td><span class="glyph">ready</span></td>` +
		`<td>Build</td><td>L2</td><td>1/1</td><td>#8 open</td>` +
		`<td>1m</td><td>5 tok</td><td>launch</td></tr></table>`
	fake := &failingTB{}
	func() {
		defer func() { _ = recover() }()
		webtest.RowFor(fake, page, "https://example.com/one/13")
	}()
	if !strings.Contains(fake.message, "#12") || !strings.Contains(fake.message, "13") {
		t.Errorf("RowFor missing failure = %q, want found refs and requested ticket", fake.message)
	}
}

func TestBoardRowsMalformedRowReportsOnlyThatRow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, ref, state string }{
		{"missing ref", `#12`, `<span class="glyph">ready</span>`},
		{"missing glyph", `<a href="/s/one/12">#12</a>`, `ready`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := `<tr class="group"><td>` + tc.ref + `</td><td>` + tc.state + `</td>` +
				`<td></td><td></td><td></td><td></td><td></td><td></td><td></td></tr>`
			page := `<table id="board"><tr><th>ref</th><th>state</th>` +
				`<th>ticket</th><th>stack</th><th>checks</th><th>pr</th>` +
				`<th>elapsed</th><th>spend</th><th>verbs</th></tr>` +
				row + `</table>`
			fake := &failingTB{}
			func() {
				defer func() { _ = recover() }()
				webtest.BoardRows(fake, page)
			}()
			if !strings.Contains(fake.message, `<tr class="group">`) || strings.Contains(fake.message, `<table`) {
				t.Errorf("BoardRows failure = %q, want row HTML only", fake.message)
			}
		})
	}
}

func TestRowForDistinguishesSameNumberAcrossRepos(t *testing.T) {
	t.Parallel()

	page := `<table id="board"><tr><th>ref</th><th>state</th><th>ticket</th>` +
		`<th>stack</th><th>checks</th><th>pr</th><th>elapsed</th>` +
		`<th>spend</th><th>verbs</th></tr>`
	for _, repo := range []string{"first", "second"} {
		page += `<tr class="group"><td><a href="/s/owner/` + repo + `/12">#12</a></td>` +
			`<td><span class="glyph">ready</span></td><td>` + repo + `</td>` +
			`<td></td><td></td><td></td><td></td><td></td><td></td></tr>`
	}
	page += `</table>`
	row := webtest.RowFor(t, page, "https://github.com/owner/second/issues/12")
	if got := row.Ticket.Text(); got != "second" {
		t.Errorf("RowFor(second) ticket = %q, want second:\n%s", got, row.Row.HTML())
	}
}

func TestRowForRejectsWrongRepoWithSameNumber(t *testing.T) {
	t.Parallel()

	page := `<table id="board"><tr><th>ref</th><th>state</th><th>ticket</th>` +
		`<th>stack</th><th>checks</th><th>pr</th><th>elapsed</th>` +
		`<th>spend</th><th>verbs</th></tr><tr class="group">` +
		`<td><a href="/s/owner/first/12">#12</a></td>` +
		`<td><span class="glyph">ready</span></td><td>first</td>` +
		`<td></td><td></td><td></td><td></td><td></td><td></td></tr></table>`
	fake := &failingTB{}
	func() {
		defer func() { _ = recover() }()
		webtest.RowFor(fake, page, "https://github.com/owner/second/issues/12")
	}()
	if !strings.Contains(fake.message, "#12") || !strings.Contains(fake.message, "second") {
		t.Errorf("RowFor wrong repo failure = %q, want requested repo and refs", fake.message)
	}
}
