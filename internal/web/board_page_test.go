package web_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/html"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func boardBody(t *testing.T, server *web.Server) string {
	t.Helper()
	rec := do(t, server, http.MethodGet, "/board", req{Header: http.Header{"Hx-Request": {"true"}}})
	return rec.Body
}

func TestBoard(t *testing.T) {
	t.Parallel()

	t.Run("groups", testBoardGroups)
	t.Run("stacks", testBoardStacks)
	t.Run("rows", testBoardRows)
	t.Run("titles", testBoardTitles)
	t.Run("spend", testBoardSpend)
	t.Run("glyphs", testBoardGlyphs)
	t.Run("confirm", testBoardConfirm)
	t.Run("forms", testBoardForms)
	t.Run("scope", testBoardScope)
	t.Run("squares", testBoardSquares)

	t.Run("the swap is the table alone", func(t *testing.T) {
		t.Parallel()
		body := renderPath(t, newServer(seededStore(t, testNow), testNow), "/board")

		root := htmltest.Parse(t, body)
		root.One(t, "table#board")
		if got := root.Find("main#main"); len(got) != 0 {
			t.Errorf("GET /board carries the page shell, want the table alone")
		}
		if regions := root.Find("[hx-swap-oob]"); len(regions) != 0 {
			t.Errorf("GET /board carries an out-of-band region: %s", regions[0].HTML())
		}
		if forms := root.Find("form#launch"); len(forms) != 0 {
			t.Errorf("GET /board carries the launch form, want it left in the page")
		}
	})

	t.Run("polls itself instead of reloading", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow)))

		want := map[string]string{
			"hx-get": "/board?all=1", "hx-trigger": "every 5s", "hx-sync": "this:drop", "hx-swap": "outerHTML",
		}
		got := attrs(root.One(t, "table#board"), "hx-get", "hx-trigger", "hx-sync", "hx-swap")
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
				board := htmltest.Parse(t, boardBody(t, srv)).One(t, "table#board")
				if got, _ := board.Attr("hx-trigger"); got != tt.want {
					t.Errorf("GET /board hx-trigger = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("verbs need no JavaScript", func(t *testing.T) {
		t.Parallel()
		states := make([]plan.State, 0, int(plan.RefreshConflicted)+1)
		for s := plan.Blocked; s <= plan.RefreshConflicted; s++ {
			states = append(states, s)
		}
		page, err := web.RenderStatesPage(states)
		if err != nil {
			t.Fatalf("RenderStatesPage: %v", err)
		}

		root := htmltest.Parse(t, page)
		verbForms := root.Find(`form[action="/verb"][method="post"][hx-post]`)
		if len(verbForms) == 0 {
			t.Error("RenderStatesPage has no scriptless verb form")
		}
		root.One(t, `form#launch[method="post"][action="/launch/open"][hx-post="/launch/open"]`)
		if selections := root.Find(`table#board input[type="checkbox"][form="launch"][name="ticket"]`); len(selections) == 0 {
			t.Error("RenderStatesPage has no scriptless ticket selection")
		}
		if got, want := len(root.Find("[hx-post]")), len(verbForms)+1; got != want {
			t.Errorf("RenderStatesPage hx-post elements = %d, want %d verb forms and the launch form", got, want)
		}
		for _, tag := range startTags(page) {
			var hasHTMX, posts bool
			for _, attr := range tag.Attr {
				hasHTMX = hasHTMX || strings.HasPrefix(attr.Key, "hx-")
				posts = posts || attr.Key == "hx-post"
			}
			if !hasHTMX {
				continue
			}
			got := map[string]string{}
			for _, attr := range tag.Attr {
				got[attr.Key] = attr.Val
			}
			formPosts := tag.Data == "form" && got["method"] == "post" &&
				(got["action"] == "/verb" || got["action"] == "/launch/open")
			allowed := tag.Data == "table" || tag.Data == "div" || tag.Data == "tr" || tag.Data == "section" ||
				(tag.Data == "a" && !posts) || (tag.Data == "input" && got["id"] == "rail-search") ||
				((tag.Data == "input" || tag.Data == "button") && got["hx-target"] == "#board") || formPosts
			if !allowed {
				t.Errorf("RenderStatesPage control needs JavaScript: %s", tag.String())
			}
			if posts && !formPosts {
				t.Errorf("RenderStatesPage posts over htmx rather than a form: %s", tag.String())
			}
		}
	})
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
