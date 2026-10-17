package web_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/html"

	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func boardBody(t *testing.T, server *web.Server) string {
	t.Helper()
	rec := do(t, server, http.MethodGet, "/board", req{Header: http.Header{"Hx-Request": {"true"}}})
	return rec.Body
}

func startTags(page string) []html.Token {
	var tags []html.Token
	markup := html.NewTokenizer(strings.NewReader(page))
	for tokenType := markup.Next(); tokenType != html.ErrorToken; tokenType = markup.Next() {
		if tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken {
			tags = append(tags, markup.Token())
		}
	}
	return tags
}

func TestBoard(t *testing.T) {
	t.Parallel()

	t.Run("groups", testBoardGroups)
	t.Run("rows", testBoardRows)
	t.Run("titles", testBoardTitles)
	t.Run("spend", testBoardSpend)
	t.Run("glyphs", testBoardGlyphs)
	t.Run("scope", testBoardScope)
	t.Run("squares", testBoardSquares)

	t.Run("the swap is the list alone", func(t *testing.T) {
		t.Parallel()
		body := renderPath(t, newServer(seededStore(t, testNow), testNow), "/board")

		root := htmltest.Parse(t, body)
		root.One(t, "div#board")
		if got := root.Find("main#main"); len(got) != 0 {
			t.Errorf("GET /board carries the page shell, want the list alone")
		}
		if regions := root.Find("[hx-swap-oob]"); len(regions) != 0 {
			t.Errorf("GET /board carries an out-of-band region: %s", regions[0].HTML())
		}
	})

	t.Run("a row opens its session and nothing on the list posts", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow)))

		board := root.One(t, "div#board")
		for _, forbidden := range []string{"form", "button", "input", "table"} {
			if got := board.Find(forbidden); len(got) != 0 {
				t.Errorf("GET /tickets board has <%s>: %s", forbidden, got[0].HTML())
			}
		}
		if got := root.Find(`form#launch`); len(got) != 0 {
			t.Errorf("GET /tickets has the bulk launch form: %s", got[0].HTML())
		}
		if got := len(board.Find("a[data-ref][href]")); got == 0 {
			t.Error("GET /tickets board has no row linking to a session")
		}
	})

	t.Run("polls itself instead of reloading", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow)))

		want := map[string]string{
			"hx-get": "/board?all=1", "hx-trigger": "every 5s", "hx-sync": "this:drop", "hx-swap": "outerHTML",
		}
		got := attrs(root.One(t, "div#board"), "hx-get", "hx-trigger", "hx-sync", "hx-swap")
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /tickets board poll mismatch (-want +got):\n%s", diff)
		}
		if refresh := root.Find(`meta[http-equiv="refresh"]`); len(refresh) != 0 {
			t.Errorf("GET /tickets has a meta refresh: %s", refresh[0].HTML())
		}
		if selects := root.Find(`[hx-select="#board"]`); len(selects) != 0 {
			t.Errorf("GET /tickets board poll selects from a full document: %s", selects[0].HTML())
		}
	})

	t.Run("the poll interval comes from the server", func(t *testing.T) {
		t.Parallel()
		store := seededStore(t, testNow)
		for _, tt := range []struct {
			name    string
			seconds int
			want    string
		}{
			{name: "default", want: "every 5s"},
			{name: "configured", seconds: 1, want: "every 1s"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				srv := web.NewServer(web.Deps{
					Logger: slog.New(slog.DiscardHandler), Store: store, Clock: fixedClock(testNow), BoardPollSeconds: tt.seconds,
				})
				board := htmltest.Parse(t, boardBody(t, srv)).One(t, "div#board")
				if got, _ := board.Attr("hx-trigger"); got != tt.want {
					t.Errorf("GET /board hx-trigger = %q, want %q", got, tt.want)
				}
			})
		}
	})
}
